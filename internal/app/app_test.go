package app

// 인수 테스트 — 진짜 조립(app)을 그대로 띄우고 화면으로만 다룬다. 바깥(Docker·git·DNS·Caddy)만 가짜다.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/model"
	"github.com/KangminNa/naru/internal/netcheck"
	"github.com/KangminNa/naru/internal/store"
	"github.com/KangminNa/naru/internal/system"
)

var ctx = context.Background()

// ── 가짜 바깥 ─────────────────────────────

type fakeRegistry struct{}

func (fakeRegistry) Build(_ context.Context, folder io.Reader, tag string, _ io.Writer) (model.ImageID, error) {
	io.Copy(io.Discard, folder)
	return model.ImageID("sha256:" + tag), nil
}

func (fakeRegistry) Pull(_ context.Context, ref model.ImageRef, _ io.Writer) (model.ImageDetails, error) {
	return model.ImageDetails{ID: model.ImageID("sha256:" + ref.String()), Ports: []model.Port{80}}, nil
}

type fakeImages struct{}

func (fakeImages) Exists(context.Context, model.ImageID) bool { return true }
func (fakeImages) Remove(context.Context, string) error       { return nil }

// fakeDocker는 컨테이너를 기억한다. hold를 닫기 전까지 새 컨테이너는 응답하지 않는다(배포가 멈춰 있다).
type fakeDocker struct {
	mu      sync.Mutex
	states  model.ContainerStates
	down    bool
	hold    chan struct{}
	listens map[string]model.Port // 주소마다 앱이 실제로 듣는 포트 (없으면 어느 포트든 응답)
}

func (d *fakeDocker) Start(_ context.Context, s model.ContainerSpec) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ip := fmt.Sprintf("172.30.0.%d", len(d.states)+10)
	d.states[s.Name] = model.ContainerState{Name: s.Name, Running: true, State: "running", Status: "Up 1 second", IP: ip}
	return ip, nil
}

func (d *fakeDocker) Remove(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.states, name)
	return nil
}

func (d *fakeDocker) TurnOff(context.Context, string) error { return nil }
func (d *fakeDocker) TurnOn(context.Context, string) error  { return nil }

func (d *fakeDocker) All(context.Context) (model.ContainerStates, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.down {
		return nil, io.ErrUnexpectedEOF
	}
	out := model.ContainerStates{}
	for k, v := range d.states {
		out[k] = v
	}
	return out, nil
}

func (d *fakeDocker) One(_ context.Context, name string) (model.ContainerState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.states[name], nil
}

func (d *fakeDocker) Logs(context.Context, string, int) (string, error) { return "", nil }

// Recent는 앱 출력인 척한다.
func (d *fakeDocker) Recent(_ context.Context, name string, n int) ([]model.LogLine, error) {
	return []model.LogLine{
		{At: time.Date(2026, 10, 10, 12, 0, 1, 0, time.UTC), Source: model.LogApp, Stream: "stdout", Text: name + " listening on :80"},
		{At: time.Date(2026, 10, 10, 12, 0, 3, 0, time.UTC), Source: model.LogApp, Stream: "stderr", Text: "warn: <script>alert(1)</script>"},
	}, nil
}
func (d *fakeDocker) BelongingTo(context.Context, model.ServiceID) ([]string, error) { return nil, nil }

func (d *fakeDocker) Answers(_ context.Context, host string, port model.Port) error {
	d.mu.Lock()
	hold := d.hold
	p, picky := d.listens[host]
	d.mu.Unlock()
	if hold != nil {
		<-hold
	}
	if picky && p != port {
		return errors.New("connection refused")
	}
	return nil
}

func (d *fakeDocker) listen(host string, p model.Port) {
	d.mu.Lock()
	if d.listens == nil {
		d.listens = map[string]model.Port{}
	}
	d.listens[host] = p
	d.mu.Unlock()
}

func (d *fakeDocker) set(name string, st model.ContainerState) {
	d.mu.Lock()
	d.states[name] = st
	d.mu.Unlock()
}

type fakeGit struct{}

func (fakeGit) Download(_ context.Context, _ model.CodeSource, into string, _ io.Writer) (model.Commit, error) {
	os.MkdirAll(into, 0o755)
	os.WriteFile(filepath.Join(into, "Dockerfile"), []byte("FROM x\nEXPOSE 3000\n"), 0o644)
	os.WriteFile(filepath.Join(into, "index.html"), []byte("<h1>hi</h1>"), 0o644)
	return model.Commit{Hash: "a1b2c3d4e5f6", Message: "fix: things"}, nil
}

// fakeCerts는 웹서버 인증서 저장소인 척한다.
type fakeCerts struct {
	mu   sync.Mutex
	list model.Certificates
}

func (c *fakeCerts) Read(context.Context) (model.Certificates, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append(model.Certificates{}, c.list...), nil
}

func (c *fakeCerts) issue(names ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, model.Certificate{Names: names, Issuer: "Let's Encrypt",
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour)})
}

// fakeSnippet은 Caddy의 /adapt인 척한다 — "nonsense"는 모르는 지시어.
type fakeSnippet struct{}

func (fakeSnippet) Compile(_ context.Context, text string) ([]byte, []string, error) {
	if strings.Contains(text, "nonsense") {
		return nil, nil, errors.New("Caddyfile:2: unrecognized directive: nonsense")
	}
	var ignored []string
	if strings.Contains(text, "tls") {
		ignored = []string{"tls — 이 설정은 Naru가 정해요 / Naru manages tls"}
	}
	return []byte(`[{"match":[{"path":["/health"]}],"handle":[{"handler":"static_response","status_code":200}]}]`), ignored, nil
}

type fakeStats struct{}

func (fakeStats) Now() model.ServerSnapshot { return model.ServerSnapshot{Cores: 2} }

// fakeCaddy는 받은 설정을 기록한다.
type fakeCaddy struct {
	mu       sync.Mutex
	loads    []string
	fail     error
	refuseIf string // 설정에 이 글자가 있으면 거절한다 (웹서버가 틀린 설정을 거절하는 것처럼)
}

func (c *fakeCaddy) Send(_ context.Context, cfg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail != nil {
		return c.fail
	}
	if c.refuseIf != "" && strings.Contains(string(cfg), c.refuseIf) {
		return errors.New(`caddy: POST /load: 400 {"error":"loading new config: bad header"}`)
	}
	c.loads = append(c.loads, string(cfg))
	return nil
}

func (c *fakeCaddy) Ping(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fail
}

func (c *fakeCaddy) last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.loads) == 0 {
		return ""
	}
	return c.loads[len(c.loads)-1]
}

// ── 하네스 ─────────────────────────────────

type harness struct {
	t      *testing.T
	app    *App
	srv    *httptest.Server
	client *http.Client
	docker *fakeDocker
	caddy  *fakeCaddy
	certs  *fakeCerts
}

// newHarness는 Naru를 통째로 띄운다 (Docker 방식). DNS는 naru.example.com → 198.51.100.24 로 고정한다.
func newHarness(t *testing.T, envDomain string) *harness { return newHarnessIn(t, envDomain, "docker") }

// newHarnessIn은 설치 방식을 골라 띄운다 — "docker" 또는 "host".
func newHarnessIn(t *testing.T, envDomain, install string) *harness {
	return newHarnessWith(t, harnessOpts{envDomain: envDomain, install: install})
}

type harnessOpts struct {
	envDomain, install string
	watchEvery         time.Duration // 0이 아니면 지켜보기 고리를 이 간격으로 돌린다
}

func newHarnessWith(t *testing.T, o harnessOpts) *harness {
	envDomain, install := o.envDomain, o.install
	t.Helper()
	lookup := func(_ context.Context, host string) ([]string, error) {
		switch host {
		case "naru.example.com":
			return []string{"198.51.100.24"}, nil
		case "elsewhere.example.com":
			return []string{"203.0.113.9"}, nil
		}
		return nil, &url.Error{Op: "lookup", URL: host}
	}
	h := &harness{t: t, docker: &fakeDocker{states: model.ContainerStates{}}, caddy: &fakeCaddy{}, certs: &fakeCerts{}}
	cfg := Config{DataDir: t.TempDir(), CaddySocket: "/run/caddy/admin.sock", Network: "naru-net", AdminDomain: envDomain, Version: "test", Install: install}
	if install == "docker" {
		cfg.SelfUpstream, cfg.CaddySites = "naru:8080", "/srv/sites"
	}
	a, err := OpenWith(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), Outside{
		Builder: fakeRegistry{}, Puller: fakeRegistry{}, Images: fakeImages{},
		Starter: h.docker, Remover: h.docker, Switch: h.docker, Watcher: h.docker, Ports: h.docker, AppLogs: h.docker,
		Code: fakeGit{}, DNS: netcheck.NewDNS(lookup), Stats: fakeStats{}, Sender: h.caddy, Certs: h.certs, Snippet: fakeSnippet{},
		Clock: system.Clock{}, Random: system.Random{}, ReadyTimeout: 5 * time.Second, WatchEvery: o.watchEvery,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.app = a
	runCtx, stop := context.WithCancel(ctx)
	go a.syncWS(runCtx)
	go a.alerts(runCtx)
	if o.watchEvery > 0 {
		go a.watch(runCtx)
	}
	h.srv = httptest.NewServer(a.handler)
	t.Cleanup(func() {
		h.srv.Close()
		stop()
		h.release()
		a.Close()
	})
	jar, _ := cookiejar.New(nil)
	h.client = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return h
}

func (h *harness) get(path string) (int, string, string) {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.srv.URL+path, nil)
	req.Header.Set("Accept-Language", "ko-KR")
	return h.do(req)
}

func (h *harness) post(path string, form url.Values) (int, string, string) {
	h.t.Helper()
	req, _ := http.NewRequest("POST", h.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "ko-KR")
	return h.do(req)
}

func (h *harness) do(req *http.Request) (int, string, string) {
	h.t.Helper()
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header.Get("Location"), string(body)
}

func (h *harness) settings() store.Settings   { return store.NewSettings(h.app.db) }
func (h *harness) services() store.Services   { return store.NewServices(h.app.db) }
func (h *harness) history() store.Deployments { return store.NewDeployments(h.app.db) }
func (h *harness) secretsOf() store.Secrets   { return store.NewSecrets(h.app.db) }
func (h *harness) setting(key string) string  { v, _ := h.settings().Get(ctx, key); return v }
func (h *harness) service(id int64) model.Service {
	s, _ := h.services().Get(ctx, model.ServiceID(id))
	return s
}

// hold는 새 컨테이너가 응답하지 않게 붙잡는다 — 배포가 진행 중인 채로 남는다.
func (h *harness) hold() {
	h.docker.mu.Lock()
	h.docker.hold = make(chan struct{})
	h.docker.mu.Unlock()
}

func (h *harness) release() {
	h.docker.mu.Lock()
	if h.docker.hold != nil {
		close(h.docker.hold)
		h.docker.hold = nil
	}
	h.docker.mu.Unlock()
}

// waitFor는 웹서버에 cond를 만족하는 설정이 닿기를 기다린다.
func (h *harness) waitFor(what string, cond func(cfg string) bool) {
	h.t.Helper()
	for i := 0; i < 300; i++ {
		if cond(h.caddy.last()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("the web server never got a config where %s:\n%s", what, h.caddy.last())
}

func (h *harness) createService(s model.NewService, sec model.ServiceSecrets) model.ServiceID {
	h.t.Helper()
	id, err := h.services().Create(ctx, s)
	if err != nil {
		h.t.Fatal(err)
	}
	h.secretsOf().Set(ctx, id, sec)
	return id
}

func (h *harness) addDomain(id model.ServiceID, d string, https, hsts bool) {
	h.t.Helper()
	dn, err := model.ParseDomainName(d)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := store.NewDomains(h.app.db).Add(ctx, id, model.DomainInput{Domain: dn, HTTPS: https}); err != nil {
		h.t.Fatal(err)
	}
	if hsts { // 화면에는 HSTS를 켜는 칸이 아직 없다 (v1에서 옮겨 온 주소만 켜져 있다)
		raw, err := sql.Open("sqlite", filepath.Join(h.app.cfg.DataDir, "naru.db"))
		if err != nil {
			h.t.Fatal(err)
		}
		defer raw.Close()
		if _, err := raw.Exec(`UPDATE domains SET hsts = 1 WHERE domain = ?`, dn.String()); err != nil {
			h.t.Fatal(err)
		}
	}
}

func name(s string) model.ServiceName {
	n, err := model.ParseServiceName(s)
	if err != nil {
		panic(err)
	}
	return n
}

// createAccount는 마법사를 계정 단계까지 지난다.
func (h *harness) createAccount() {
	h.t.Helper()
	code, loc, _ := h.post("/setup", url.Values{"token": {h.app.key.Value()}, "username": {"admin"}, "password": {"longenough"}, "password2": {"longenough"}})
	if code != http.StatusSeeOther || loc != "/setup/domain" {
		h.t.Fatalf("account step: %d → %q", code, loc)
	}
}

// signedIn은 설정을 마치고 로그인한 상태로 만든다.
func (h *harness) signedIn() {
	h.t.Helper()
	h.createAccount()
	h.settings().Set(ctx, model.SettingSetupDone, "1")
	h.settings().Set(ctx, model.SettingAdminDomain, "naru.example.com")
}

// ── 첫 설정 · 로그인 · 설정 ─────────────────────

func TestFreshInstallSendsEveryoneToSetup(t *testing.T) {
	h := newHarness(t, "")
	for _, p := range []string{"/", "/settings", "/login"} {
		if code, loc, _ := h.get(p); code != http.StatusSeeOther || loc != "/setup" {
			t.Errorf("%s: %d → %q, want /setup", p, code, loc)
		}
	}
	code, _, body := h.get("/setup")
	if code != http.StatusOK || !strings.Contains(body, "docker compose logs naru") || strings.Contains(body, `name="password"`) {
		t.Fatalf("without a token the setup page explains where the link is and shows no form")
	}
	if code, _, _ := h.get("/setup?token=wrong"); code != http.StatusForbidden {
		t.Fatalf("a wrong token is forbidden, got %d", code)
	}
	if code, _, body := h.get("/setup?token=" + h.app.key.Value()); code != http.StatusOK || !strings.Contains(body, `name="password"`) {
		t.Fatal("the right token shows the account form")
	}
	h.waitFor("unknown hosts reach the admin screen before setup", func(cfg string) bool {
		return strings.Contains(cfg, "naru:8080") && !strings.Contains(cfg, `"status_code": 404`)
	})
}

func TestSetupRefusesWithoutTheToken(t *testing.T) {
	h := newHarness(t, "")
	code, _, _ := h.post("/setup", url.Values{"token": {"guess"}, "username": {"intruder"}, "password": {"longenough"}, "password2": {"longenough"}})
	if names, _ := h.app.Accounts.Names(ctx); code != http.StatusForbidden || len(names) != 0 {
		t.Fatalf("an account must not be created without the token (status %d)", code)
	}
	if code, _, _ := h.post("/setup", url.Values{"token": {"guess"}, "username": {"x"}, "password": {"longenough"}, "password2": {"longenough"}}); code != http.StatusForbidden {
		t.Fatal("the token is checked before anything else the form says")
	}
}

func TestWizardFromAccountToHome(t *testing.T) {
	h := newHarness(t, "")
	h.createAccount()

	if code, loc, _ := h.get("/"); code != http.StatusSeeOther || loc != "/setup/domain" {
		t.Fatalf("an unfinished wizard returns to the domain step: %d %q", code, loc)
	}

	// 다른 곳을 가리키는 도메인은 경고와 함께 머문다
	code, _, body := h.post("/setup/domain", url.Values{"domain": {"elsewhere.example.com"}, "action": {"check"}})
	if code != http.StatusOK || !strings.Contains(body, "203.0.113.9") {
		t.Fatalf("a domain pointing elsewhere is shown, not saved: %d", code)
	}
	if h.setting(model.SettingAdminDomain) != "" {
		t.Fatal("not saved before confirmation")
	}

	// 우리를 가리키는 도메인 — 사용자가 IP로 들어왔을 때 그 IP와 견준다
	req, _ := http.NewRequest("POST", h.srv.URL+"/setup/domain", strings.NewReader(url.Values{"domain": {"https://NARU.example.com/"}, "action": {"check"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "198.51.100.24"
	for _, c := range h.client.Jar.Cookies(req.URL) { // Host를 바꾸면 쿠키 저장소가 다른 호스트로 본다
		req.AddCookie(c)
	}
	if code, loc, _ := h.do(req); code != http.StatusSeeOther || loc != "/setup/https" {
		t.Fatalf("a matching domain moves on: %d %q", code, loc)
	}
	if v := h.setting(model.SettingAdminDomain); v != "naru.example.com" {
		t.Fatalf("the domain is saved normalized, got %q", v)
	}

	if code, _, _ := h.post("/setup/https", url.Values{"email": {"not-an-email"}, "action": {"save"}}); code != http.StatusBadRequest {
		t.Fatal("a malformed email is refused")
	}
	// 테스트 서버는 127.0.0.1로 들어와 있으니 관리 주소로 가라고 안내한다
	if code, _, body := h.post("/setup/https", url.Values{"email": {"me@example.com"}, "action": {"save"}}); code != http.StatusOK || !strings.Contains(body, "http://naru.example.com/login") {
		t.Fatalf("finishing points to the admin address: %d", code)
	}
	if h.setting(model.SettingACMEEmail) != "me@example.com" {
		t.Fatal("the email is saved")
	}
	code, _, body = h.get("/?ok=setup")
	if code != http.StatusOK || !strings.Contains(body, "설정을 마쳤어요") || strings.Contains(body, "관리 화면 주소가 아직 없어요") {
		t.Fatalf("home after setup: %d", code)
	}
	if code, loc, _ := h.get("/setup"); code != http.StatusSeeOther || loc != "/" {
		t.Fatal("setup is closed once done")
	}
	h.waitFor("the admin address has HTTPS and unknown hosts get 404", func(cfg string) bool {
		return strings.Contains(cfg, "naru.example.com") && strings.Contains(cfg, "me@example.com") && strings.Contains(cfg, `"status_code": 404`)
	})
}

func TestSkippingTheDomainShowsANoticeAtHome(t *testing.T) {
	h := newHarness(t, "")
	h.createAccount()
	h.post("/setup/domain", url.Values{"action": {"skip"}})
	h.post("/setup/https", url.Values{"action": {"skip"}})
	_, _, body := h.get("/")
	if !strings.Contains(body, "관리 화면 주소가 아직 없어요") {
		t.Fatal("without an admin address, home says so")
	}
}

// 입력 칸에서 Enter를 누르면 브라우저는 DOM에서 첫 번째 제출 버튼을 누른다.
// 그게 "건너뛰기"면 사용자가 적은 값이 버려진다.
func TestEnterSubmitsThePrimaryAction(t *testing.T) {
	h := newHarness(t, "")
	h.createAccount()
	firstSubmit := regexp.MustCompile(`<button[^>]*type="submit"[^>]*value="(\w+)"`)
	_, _, body := h.get("/setup/domain")
	if m := firstSubmit.FindStringSubmatch(body); m == nil || m[1] != "check" {
		t.Fatalf("domain step: first submit button is %v", m)
	}
	_, _, body = h.get("/setup/https")
	if m := firstSubmit.FindStringSubmatch(body); m == nil || m[1] != "save" {
		t.Fatalf("https step: first submit button is %v", m)
	}
}

func TestEnvironmentDomainWins(t *testing.T) {
	h := newHarness(t, "env.example.com")
	h.createAccount()
	_, _, body := h.get("/setup/domain")
	if !strings.Contains(body, "env.example.com") || strings.Contains(body, `name="domain"`) {
		t.Fatal("an environment domain is shown fixed, without an input")
	}
}

func TestLoginLogoutAndLockout(t *testing.T) {
	h := newHarness(t, "")
	h.createAccount()
	h.settings().Set(ctx, model.SettingSetupDone, "1")
	h.post("/logout", nil)
	if code, loc, _ := h.get("/"); code != http.StatusSeeOther || loc != "/login" {
		t.Fatalf("signed out → login, got %d %q", code, loc)
	}
	if code, _, _ := h.post("/login", url.Values{"username": {"admin"}, "password": {"wrong"}}); code != http.StatusUnauthorized {
		t.Fatal("a wrong password is 401")
	}
	for i := 0; i < 5; i++ {
		h.post("/login", url.Values{"username": {"admin"}, "password": {"wrong"}})
	}
	if code, _, body := h.post("/login", url.Values{"username": {"admin"}, "password": {"longenough"}}); code != http.StatusTooManyRequests || !strings.Contains(body, "15분") {
		t.Fatalf("locked out, got %d", code)
	}
}

func TestChangePasswordFromSettings(t *testing.T) {
	h := newHarness(t, "")
	h.createAccount()
	h.settings().Set(ctx, model.SettingSetupDone, "1")

	if code, _, _ := h.post("/settings/password", url.Values{"current": {"longenough"}, "next": {"newpassword1"}, "next2": {"different1"}}); code != http.StatusBadRequest {
		t.Fatal("mismatched confirmation is refused")
	}
	if code, _, body := h.post("/settings/password", url.Values{"current": {"nope"}, "next": {"newpassword1"}, "next2": {"newpassword1"}}); code != http.StatusBadRequest || !strings.Contains(body, "지금 비밀번호가 맞지 않아요") {
		t.Fatal("a wrong current password is refused with its own message")
	}
	if code, loc, _ := h.post("/settings/password", url.Values{"current": {"longenough"}, "next": {"newpassword1"}, "next2": {"newpassword1"}}); code != http.StatusSeeOther || !strings.HasPrefix(loc, "/settings?ok=password") {
		t.Fatalf("changed: %d %q", code, loc)
	}
	if code, _, _ := h.get("/settings"); code != http.StatusOK {
		t.Fatal("this device stays signed in")
	}
	h.post("/logout", nil)
	if code, loc, _ := h.post("/login", url.Values{"username": {"admin"}, "password": {"newpassword1"}}); code != http.StatusSeeOther || loc != "/" {
		t.Fatal("the new password works")
	}
}

func TestSettingsChangeTheAdminAddress(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	code, _, body := h.post("/settings/domain", url.Values{"domain": {"elsewhere.example.com"}, "email": {"ops@example.com"}, "action": {"check"}})
	if code != http.StatusOK || !strings.Contains(body, "203.0.113.9") {
		t.Fatalf("a domain pointing elsewhere is shown first: %d", code)
	}
	if code, loc, _ := h.post("/settings/domain", url.Values{"domain": {"elsewhere.example.com"}, "email": {"ops@example.com"}, "action": {"save"}}); code != http.StatusSeeOther || !strings.Contains(loc, "ok=domain") {
		t.Fatalf("saved anyway: %d %q", code, loc)
	}
	if h.setting(model.SettingAdminDomain) != "elsewhere.example.com" || h.setting(model.SettingACMEEmail) != "ops@example.com" {
		t.Fatal("both are saved")
	}
	if code, _, _ := h.post("/settings/domain", url.Values{"domain": {"bad domain"}}); code != http.StatusBadRequest {
		t.Fatal("a malformed domain is refused")
	}
	h.waitFor("the new admin address is served", func(cfg string) bool { return strings.Contains(cfg, "elsewhere.example.com") })
}

func TestCrossSiteFormsAreRefused(t *testing.T) {
	h := newHarness(t, "")
	req, _ := http.NewRequest("POST", h.srv.URL+"/login", strings.NewReader("username=admin&password=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	if code, _, _ := h.do(req); code != http.StatusForbidden {
		t.Fatalf("a foreign Origin is refused, got %d", code)
	}
}

func TestLanguageToggleAndSafeRedirect(t *testing.T) {
	h := newHarness(t, "")
	if _, loc, _ := h.get("/lang/en?back=/settings"); loc != "/settings" {
		t.Fatalf("back to the same page, got %q", loc)
	}
	_, _, body := h.get("/setup")
	if !strings.Contains(body, "You need the setup link") {
		t.Fatal("the language cookie switches the screen to English")
	}
	for _, evil := range []string{"https://evil.example", "//evil.example", "/\\evil.example"} {
		if _, loc, _ := h.get("/lang/ko?back=" + url.QueryEscape(evil)); loc != "/" {
			t.Errorf("back=%q must not leave the site, got %q", evil, loc)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t, "")
	res, err := h.client.Get(h.srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	for _, k := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options"} {
		if res.Header.Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
}

func TestUnknownPathsAre404(t *testing.T) {
	h := newHarness(t, "")
	if code, _, _ := h.get("/nope"); code != http.StatusNotFound {
		t.Fatalf("got %d", code)
	}
}

// ── 홈 · 서비스 ────────────────────────────

func TestHomeListsServicesWithTheirState(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	blog := h.createService(model.NewService{Name: name("blog"), Kind: model.KindRepo, Alias: "shelf-blog", Port: 3000}, model.ServiceSecrets{})
	h.addDomain(blog, "blog.example.com", true, false)
	h.createService(model.NewService{Name: name("api"), Kind: model.KindImage, Alias: "naru-api", Port: 8080}, model.ServiceSecrets{})
	h.createService(model.NewService{Name: name("nas"), Kind: model.KindExternal, External: "host.docker.internal:5000"}, model.ServiceSecrets{})
	h.docker.set("shelf-blog", model.ContainerState{Name: "shelf-blog", Running: true, State: "running", Status: "Up 3 hours"})

	_, _, body := h.get("/")
	for _, want := range []string{"blog.example.com", "실행 중", "Up 3 hours", "컨테이너 없음", "주소만 연결", "/services/1", "3개를 이 서버에서"} {
		if !strings.Contains(body, want) {
			t.Errorf("home is missing %q", want)
		}
	}
	if strings.Contains(body, "Docker에 연결하지 못했어요") || strings.Contains(body, "웹서버에 연결하지 못했어요") {
		t.Error("no trouble notices while everything is reachable")
	}
}

func TestHomeSaysWhenDockerOrTheWebServerIsDown(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	h.createService(model.NewService{Name: name("blog"), Kind: model.KindRepo, Alias: "shelf-blog", Port: 3000}, model.ServiceSecrets{})
	h.docker.down = true
	h.caddy.mu.Lock()
	h.caddy.fail = errors.New("caddy: dial unix /run/caddy/admin.sock: connect: no such file")
	h.caddy.mu.Unlock()
	h.settings().Set(ctx, model.SettingACMEEmail, "x@example.com") // 설정을 바꿔 다시 보내게 한다
	h.post("/settings/domain", url.Values{"domain": {"naru.example.com"}, "email": {"y@example.com"}, "action": {"save"}})
	for i := 0; i < 300; i++ {
		if _, _, body := h.get("/"); strings.Contains(body, "admin.sock") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, _, body := h.get("/")
	for _, want := range []string{"Docker에 연결하지 못했어요", "웹서버에 연결하지 못했어요", "admin.sock", "확인 불가"} {
		if !strings.Contains(body, want) {
			t.Errorf("home is missing %q", want)
		}
	}
}

func TestServicePage(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	id := h.createService(model.NewService{Name: name("landing"), Kind: model.KindRepo, Source: "https://github.com/me/landing", Branch: "main", Alias: "shelf-landing", Port: 4023, BuildPath: "site"},
		model.ServiceSecrets{GitToken: "ghp_secret", WebhookSecret: "whsec_secret"})
	h.addDomain(id, "www.example.com", true, true)
	h.docker.set("shelf-landing", model.ContainerState{State: "restarting", Status: "Restarting (1) 3 seconds ago"})

	code, _, body := h.get("/services/1")
	if code != http.StatusOK {
		t.Fatalf("got %d", code)
	}
	for _, want := range []string{"landing", "shelf-landing:4023", "www.example.com", "HSTS", "계속 다시 시작됨", "https://github.com/me/landing", "site", "https://naru.example.com/hooks/1", "whsec_secret"} {
		if !strings.Contains(body, want) {
			t.Errorf("service page is missing %q", want)
		}
	}
	if strings.Contains(body, "ghp_secret") {
		t.Fatal("the git token never reaches the screen")
	}
	for _, p := range []string{"/services/99", "/services/abc"} {
		if code, _, _ := h.get(p); code != http.StatusNotFound {
			t.Errorf("%s: got %d", p, code)
		}
	}
}

func TestCreatingAServiceStartsItsFirstDeploy(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	code, loc, _ := h.post("/services/new", url.Values{"kind": {"image"}, "source": {"traefik/whoami"}, "domain": {"Who.Example.com"}})
	if code != http.StatusSeeOther || !strings.HasPrefix(loc, "/services/1/deploys/") {
		t.Fatalf("%d %q", code, loc)
	}
	s := h.service(1)
	if s.Name.String() != "who" || s.Kind != model.KindImage || s.PrimaryDomain() != "who.example.com" || !s.AutoDeploy {
		t.Fatalf("the name comes from the domain: %+v", s)
	}
	if deps, _ := h.history().Recent(ctx, 1, 10); len(deps) != 1 || deps[0].Reason != model.ReasonCreate {
		t.Fatalf("the first deploy starts: %+v", deps)
	}
	if sec, _ := h.secretsOf().Get(ctx, 1); len(sec.WebhookSecret) < 32 {
		t.Fatal("every service gets its own webhook secret")
	}
	h.waitFor("the new service is routed to its container", func(cfg string) bool {
		return strings.Contains(cfg, "who.example.com") && strings.Contains(cfg, "naru-who:80")
	})

	// 외부 연결은 배포가 없다
	code, loc, _ = h.post("/services/new", url.Values{"kind": {"external"}, "upstream": {"localhost:5000"}, "domain": {"nas.example.com"}})
	if code != http.StatusSeeOther || loc != "/services/2" {
		t.Fatalf("%d %q", code, loc)
	}
	if nas := h.service(2); nas.External != "host.docker.internal:5000" || nas.Name.String() != "nas" {
		t.Fatalf("localhost means this server: %+v", nas)
	}
	if deps, _ := h.history().Recent(ctx, 2, 10); len(deps) != 0 {
		t.Fatal("nothing to deploy")
	}
}

func TestStaticSiteFromTheScreen(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	code, loc, _ := h.post("/services/new", url.Values{"kind": {"static"}, "source": {"https://github.com/me/site"}, "domain": {"www.example.com"}})
	if code != http.StatusSeeOther || !strings.HasPrefix(loc, "/services/1/deploys/") {
		t.Fatalf("%d %q", code, loc)
	}
	h.waitFor("the site is served from its folder", func(cfg string) bool { return strings.Contains(cfg, "/srv/sites/www/") })
	if _, err := os.Stat(filepath.Join(h.app.cfg.DataDir, "sites", "www")); err != nil {
		t.Fatal("files are published under data/sites")
	}
}

func TestCreateServiceValidation(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	taken := h.createService(model.NewService{Name: name("taken"), Kind: model.KindExternal, External: "x:1"}, model.ServiceSecrets{})
	h.addDomain(taken, "used.example.com", false, false)
	cases := []struct {
		form url.Values
		want string
	}{
		{url.Values{"kind": {"repo"}, "source": {"file:///etc"}}, "https://"},
		{url.Values{"kind": {"repo"}, "source": {"https://github.com/me/x"}, "branch": {"--upload-pack=x"}}, "브랜치"},
		{url.Values{"kind": {"repo"}, "source": {"https://github.com/me/x"}, "build_path": {"../../etc"}}, ".."},
		{url.Values{"kind": {"image"}, "source": {"bad image"}}, "이미지 이름"},
		{url.Values{"kind": {"external"}, "upstream": {"nope"}}, "주소:포트"},
		{url.Values{"kind": {"image"}, "source": {"a/b"}, "domain": {"used.example.com"}}, "이미 쓰는"},
		{url.Values{"kind": {"image"}, "source": {"a/b"}, "domain": {"naru.example.com"}}, "관리 화면"},
		{url.Values{"kind": {"image"}, "source": {"a/b"}, "name": {"taken"}}, "이미 있는 이름"},
		{url.Values{"kind": {"image"}, "source": {"a/b"}, "name": {"Bad_Name"}}, "영문 소문자"},
		{url.Values{"kind": {"image"}, "source": {"a/b"}, "env": {"not an env line"}}, "KEY=value"},
		{url.Values{"kind": {"image"}, "source": {"a/b"}, "volumes": {"relative:/data"}}, "서버경로"},
		{url.Values{"kind": {"image"}, "source": {"a/b"}, "port": {"99999"}}, "1~65535"},
		{url.Values{"kind": {"nonsense"}}, "종류"},
	}
	for _, c := range cases {
		code, _, body := h.post("/services/new", c.form)
		if code != http.StatusBadRequest || !strings.Contains(body, c.want) {
			t.Errorf("%v: %d, want message containing %q", c.form, code, c.want)
		}
	}
	if all, _ := h.services().List(ctx); len(all) != 1 {
		t.Fatal("nothing is created after a refused form")
	}
}

func TestSettingsDomainsAndDelete(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	env, _ := model.ParseEnvVars("A=1")
	id := h.createService(model.NewService{Name: name("api"), Kind: model.KindImage, Source: "me/api", Alias: "naru-api", Port: 3000}, model.ServiceSecrets{Env: env})

	code, loc, _ := h.post(fmt.Sprintf("/services/%d/settings", id), url.Values{"source": {"me/api"}, "port": {"8080"}, "env": {"A=1"}, "auto_deploy": {"1"}})
	if code != http.StatusSeeOther || !strings.Contains(loc, "ok=saved") {
		t.Fatalf("%d %q", code, loc)
	}
	if s := h.service(int64(id)); s.Port != 8080 {
		t.Fatal("port saved")
	}
	if _, loc, _ := h.post(fmt.Sprintf("/services/%d/settings", id), url.Values{"source": {"me/api"}, "port": {"8080"}, "env": {"A=2"}}); !strings.Contains(loc, "ok=redeploy") {
		t.Fatal("env changes say a redeploy is needed")
	}
	if code, _, body := h.post(fmt.Sprintf("/services/%d/settings", id), url.Values{"source": {"me/api"}, "env": {"oops"}}); code != http.StatusBadRequest || !strings.Contains(body, "KEY=value") || !strings.Contains(body, "oops") {
		t.Fatal("a bad settings form is shown again with what was typed")
	}

	h.post(fmt.Sprintf("/services/%d/domains", id), url.Values{"domain": {"api.example.com"}, "https": {"1"}})
	if s := h.service(int64(id)); s.PrimaryDomain() != "api.example.com" {
		t.Fatal("domain added")
	}
	h.waitFor("a port change reaches the web server", func(cfg string) bool {
		return strings.Contains(cfg, "api.example.com") && strings.Contains(cfg, "naru-api:8080")
	})
	if code, _, _ := h.post(fmt.Sprintf("/services/%d/domains", id), url.Values{"domain": {"naru.example.com"}}); code != http.StatusBadRequest {
		t.Fatal("the admin address cannot be taken")
	}
	s := h.service(int64(id))
	h.post(fmt.Sprintf("/services/%d/domains/%d/delete", id, s.Domains[0].ID), nil)
	if s := h.service(int64(id)); len(s.Domains) != 0 {
		t.Fatal("domain removed")
	}

	if code, _, _ := h.post(fmt.Sprintf("/services/%d/delete", id), url.Values{"confirm": {"nope"}}); code != http.StatusBadRequest {
		t.Fatal("deleting needs the name")
	}
	if _, loc, _ := h.post(fmt.Sprintf("/services/%d/delete", id), url.Values{"confirm": {"api"}}); loc != "/?ok=deleted" {
		t.Fatalf("deleted → home, got %q", loc)
	}
	if _, err := h.services().Get(ctx, id); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("gone")
	}
}

func TestDeployPageShowsStepsAndRefreshesWhileRunning(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	id := h.createService(model.NewService{Name: name("app"), Kind: model.KindImage, Source: "me/app"}, model.ServiceSecrets{})
	did, _ := h.history().Start(ctx, id, model.ReasonManual)
	h.history().SaveLog(ctx, did, "\n▶ 이미지 받기 / pulling the image\nok\n▶ 새 컨테이너 시작 / starting the new container\n")

	_, _, body := h.get(fmt.Sprintf("/services/%d/deploys/%d", id, did))
	for _, want := range []string{"이미지 받기", "새 컨테이너 시작", `http-equiv="refresh"`, "진행 중", `class="doing"`} {
		if !strings.Contains(body, want) {
			t.Errorf("deploy page is missing %q", want)
		}
	}
	h.history().Finish(ctx, model.Deployment{ID: did, Status: model.DeploySuccess, Log: "▶ 이미지 받기 / pulling\n"})
	_, _, body = h.get(fmt.Sprintf("/services/%d/deploys/%d", id, did))
	if strings.Contains(body, `http-equiv="refresh"`) {
		t.Fatal("a finished deploy stops refreshing")
	}
	if code, _, _ := h.get(fmt.Sprintf("/services/%d/deploys/999", id)); code != http.StatusNotFound {
		t.Fatal("unknown deploy → 404")
	}
}

func TestDeployStopStartAndRollBackFromTheScreen(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	id := h.createService(model.NewService{Name: name("app"), Kind: model.KindImage, Source: "me/app"}, model.ServiceSecrets{})
	_, loc, _ := h.post(fmt.Sprintf("/services/%d/deploy", id), nil)
	if !strings.HasPrefix(loc, fmt.Sprintf("/services/%d/deploys/", id)) {
		t.Fatalf("deploy → its page, got %q", loc)
	}
	h.app.wait()
	first, _ := h.history().Recent(ctx, id, 1)
	if first[0].Status != model.DeploySuccess {
		t.Fatalf("%+v", first[0])
	}
	h.post(fmt.Sprintf("/services/%d/deploy", id), nil)
	h.app.wait()

	if _, loc, _ := h.post(fmt.Sprintf("/services/%d/stop", id), nil); !strings.Contains(loc, "ok=stopped") || !h.service(int64(id)).Live.Stopped {
		t.Fatalf("stopped: %q", loc)
	}
	if _, loc, _ := h.post(fmt.Sprintf("/services/%d/start", id), nil); !strings.Contains(loc, "ok=started") || h.service(int64(id)).Live.Stopped {
		t.Fatalf("started: %q", loc)
	}
	_, loc, _ = h.post(fmt.Sprintf("/services/%d/rollback/%d", id, first[0].ID), nil)
	h.app.wait()
	if !strings.Contains(loc, "/deploys/") {
		t.Fatalf("rollback → its page, got %q", loc)
	}
	if latest, _ := h.history().Recent(ctx, id, 1); latest[0].Reason != model.ReasonRollBack || latest[0].Status != model.DeploySuccess {
		t.Fatalf("%+v", latest[0])
	}
	if _, loc, _ := h.post(fmt.Sprintf("/services/%d/rollback/999", id), nil); !strings.Contains(loc, "err=rollback") {
		t.Fatalf("an unknown deployment cannot be restored: %q", loc)
	}
}

// ── 인증서 · HTTPS 넘기기 ──────────────────────

func TestHTTPSRedirectStartsOnceTheCertificateExists(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	id := h.createService(model.NewService{Name: name("blog"), Kind: model.KindImage, Source: "me/blog", Alias: "naru-blog", Port: 80}, model.ServiceSecrets{})
	h.post(fmt.Sprintf("/services/%d/domains", id), url.Values{"domain": {"elsewhere.example.com"}, "https": {"1"}})

	_, _, body := h.get(fmt.Sprintf("/services/%d", id))
	if !strings.Contains(body, "인증서 받는 중") || !strings.Contains(body, "203.0.113.9") {
		t.Fatal("without a certificate the page says so and shows where DNS points")
	}
	h.waitFor("the site is served over HTTP without a redirect", func(cfg string) bool {
		return strings.Contains(cfg, "elsewhere.example.com") && !strings.Contains(cfg, "Location")
	})

	h.certs.issue("elsewhere.example.com")
	h.certs.issue("naru.example.com")
	h.post(fmt.Sprintf("/services/%d/domains", id), url.Values{"domain": {"other.example.com"}}) // 지도를 다시 그리게 한다
	h.waitFor("the site and the admin address redirect to HTTPS", func(cfg string) bool {
		return strings.Count(cfg, `"Location"`) == 2 && strings.Contains(cfg, `"status_code": 307`)
	})
	_, _, body = h.get(fmt.Sprintf("/services/%d", id))
	if !strings.Contains(body, "HTTPS · ") || strings.Contains(body, "인증서 받는 중") || !strings.Contains(body, "Let&#39;s Encrypt") {
		t.Fatal("the page shows until when the certificate lasts")
	}
	_, _, body = h.get("/settings")
	if !strings.Contains(body, "HTTPS · ") {
		t.Fatal("settings show the admin address certificate")
	}
}

// ── 웹서버 설정 (M5) ──────────────────────────

func TestWebServerSettingsFromTheScreen(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	shop := h.createService(model.NewService{Name: name("shop"), Kind: model.KindImage, Source: "me/shop", Alias: "naru-shop", Port: 3000}, model.ServiceSecrets{})
	api := h.createService(model.NewService{Name: name("api"), Kind: model.KindImage, Source: "me/api", Alias: "naru-api", Port: 8080}, model.ServiceSecrets{})
	h.addDomain(shop, "shop.example.com", true, false)
	page := fmt.Sprintf("/services/%d", shop)

	code, loc, _ := h.post(page+"/web", url.Values{
		"headers": {"X-Frame-Options: DENY"}, "allow": {"10.0.0.0/8\n192.168.1.5"}, "login_user": {"admin"}, "login_password": {"longenough"},
		"paths": {"/api api 떼기"}, "advanced": {"respond /health 200"},
	})
	if code != http.StatusSeeOther || !strings.Contains(loc, "ok=web") {
		t.Fatalf("%d %q", code, loc)
	}
	cfg := h.caddy.last()
	for _, want := range []string{"X-Frame-Options", `"client_ip"`, "192.168.1.5/32", `"authentication"`, "$2a$", `"strip_path_prefix": "/api"`, "naru-api:8080", `"/health"`, `"encode"`} {
		if !strings.Contains(cfg, want) {
			t.Errorf("the web server got %s", want)
		}
	}
	if strings.Contains(cfg, "longenough") {
		t.Fatal("the password itself never reaches the web server")
	}

	_, _, body := h.get(page)
	if !strings.Contains(body, "X-Frame-Options: DENY") || !strings.Contains(body, "/api api 떼기") || !strings.Contains(body, "비워 두면 지금 비밀번호 그대로") || strings.Contains(body, "$2a$") {
		t.Fatal("the screen shows the settings, and only that a password exists")
	}
	if _, _, body := h.get(fmt.Sprintf("/services/%d", api)); !strings.Contains(body, "이 서비스에 보내는 서비스") || !strings.Contains(body, "<b>shop</b>") {
		t.Fatal("the target service says who routes to it")
	}

	// 웹서버가 거절하면 되돌린다 — 이전 설정이 그대로 남고, 웹서버가 한 말이 보인다
	h.caddy.mu.Lock()
	h.caddy.refuseIf = "X-Refuse-Me"
	h.caddy.mu.Unlock()
	loads := len(h.caddy.loads)
	code, _, body = h.post(page+"/web", url.Values{"headers": {"X-Refuse-Me: 1"}, "login_user": {"admin"}})
	if code != http.StatusBadRequest || !strings.Contains(body, "되돌렸어요") || !strings.Contains(body, "bad header") || !strings.Contains(body, "X-Refuse-Me: 1") {
		t.Fatalf("refused: %d", code)
	}
	if got, _ := store.NewWebSettings(h.app.db).Get(ctx, shop); len(got.Headers) != 1 || got.Headers[0].Name != "X-Frame-Options" || got.Login.User != "admin" {
		t.Fatalf("the previous settings are restored: %+v", got)
	}
	if len(h.caddy.loads) != loads && !strings.Contains(h.caddy.last(), "X-Frame-Options") {
		t.Fatal("the web server keeps serving the previous settings")
	}

	// 고급 칸의 틀린 지시어는 웹서버에 닿기 전에 막힌다
	code, _, body = h.post(page+"/web", url.Values{"advanced": {"nonsense"}})
	if code != http.StatusBadRequest || !strings.Contains(body, "unrecognized directive: nonsense") {
		t.Fatalf("compile errors show Caddy's words: %d", code)
	}
	// 적용됐지만 반영되지 않은 지시어는 알려 준다
	if code, _, body := h.post(page+"/web", url.Values{"advanced": {"tls internal"}}); code != http.StatusOK || !strings.Contains(body, "반영되지 않았어요") {
		t.Fatalf("ignored directives are reported: %d", code)
	}
	for form, want := range map[string]string{"/api nowhere": "대상을 찾지 못했어요", "/api shop": "자기 자신으로는", "api api": "/경로 대상"} {
		if code, _, body := h.post(page+"/web", url.Values{"paths": {form}}); code != http.StatusBadRequest || !strings.Contains(body, want) {
			t.Errorf("%q: %d, want %q", form, code, want)
		}
	}
	if code, _, body := h.post(page+"/web", url.Values{"allow": {"not-an-ip"}}); code != http.StatusBadRequest || !strings.Contains(body, "대역") {
		t.Errorf("bad IP: %d", code)
	}
}

func TestImportFromNginxFillsTheFormWithoutSaving(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	shop := h.createService(model.NewService{Name: name("shop"), Kind: model.KindImage, Source: "me/shop", Alias: "naru-shop", Port: 3000}, model.ServiceSecrets{})
	nginx := "server {\n  server_name shop.example.com;\n  add_header X-Robots-Tag noindex;\n  location /api/ { proxy_pass http://api:8080/; }\n}"
	code, _, body := h.post(fmt.Sprintf("/services/%d/web/nginx", shop), url.Values{"nginx": {nginx}})
	if code != http.StatusOK || !strings.Contains(body, "X-Robots-Tag: noindex") || !strings.Contains(body, "/api api:8080 떼기") || !strings.Contains(body, "server_name") {
		t.Fatalf("the form is filled and skipped lines are listed: %d", code)
	}
	if got, _ := store.NewWebSettings(h.app.db).Get(ctx, shop); len(got.Headers) != 0 {
		t.Fatal("nothing is saved until the user applies")
	}
}

// ── 설치 방식 ─────────────────────────────

// 설치형은 같은 화면·같은 흐름이고, 웹서버가 닿는 방법만 다르다 — 컨테이너는 IP로, "이 서버"는 127.0.0.1로.
func TestHostInstallReachesContainersByIP(t *testing.T) {
	h := newHarnessIn(t, "", "host")
	h.signedIn()
	_, loc, _ := h.post("/services/new", url.Values{"kind": {"image"}, "source": {"traefik/whoami"}, "domain": {"who.example.com"}})
	if !strings.HasPrefix(loc, "/services/1/deploys/") {
		t.Fatalf("deploy starts: %q", loc)
	}
	h.app.wait()
	ip := h.service(1).Live.InstanceIP
	if ip == "" {
		t.Fatal("the container's IP is recorded")
	}
	h.post("/services/new", url.Values{"kind": {"external"}, "upstream": {"localhost:5000"}, "domain": {"nas.example.com"}})
	h.waitFor("containers by IP, this server as 127.0.0.1, the admin screen on loopback", func(cfg string) bool {
		return strings.Contains(cfg, ip+":80") && strings.Contains(cfg, "127.0.0.1:5000") && strings.Contains(cfg, "127.0.0.1:8080") &&
			!strings.Contains(cfg, "naru-who:80") && !strings.Contains(cfg, "host.docker.internal")
	})
	if !strings.Contains(h.caddy.last(), filepath.Join(h.app.cfg.DataDir, "sites")) && strings.Contains(h.caddy.last(), "/srv/sites") {
		t.Fatal("static sites are served straight from the data folder")
	}
}

func TestDockerInstallKeepsTheNetworkAlias(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	h.post("/services/new", url.Values{"kind": {"image"}, "source": {"traefik/whoami"}, "domain": {"who.example.com"}})
	h.app.wait()
	h.waitFor("containers by alias", func(cfg string) bool { return strings.Contains(cfg, "naru-who:80") })
}

func TestUnknownInstallModeRefusesToStart(t *testing.T) {
	_, err := OpenWith(Config{DataDir: t.TempDir(), Install: "kubernetes"}, slog.New(slog.NewTextHandler(io.Discard, nil)), Outside{Clock: system.Clock{}, Random: system.Random{}, Sender: &fakeCaddy{}})
	if err == nil || !strings.Contains(err.Error(), "NARU_INSTALL") {
		t.Fatalf("got %v", err)
	}
}

// ── 지켜보기 · 알리기 (M6-1) ──────────────────────

// receiver는 알림을 받는 쪽인 척한다.
type receiver struct {
	mu   sync.Mutex
	got  []map[string]any
	sigs []string
	srv  *httptest.Server
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var m map[string]any
		json.NewDecoder(req.Body).Decode(&m)
		r.mu.Lock()
		r.got, r.sigs = append(r.got, m), append(r.sigs, req.Header.Get("X-Naru-Signature-256"))
		r.mu.Unlock()
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, m := range r.got {
		out = append(out, fmt.Sprint(m["event"]))
	}
	return out
}

func (r *receiver) waitFor(t *testing.T, event string) map[string]any {
	t.Helper()
	for i := 0; i < 300; i++ {
		r.mu.Lock()
		for _, m := range r.got {
			if m["event"] == event {
				r.mu.Unlock()
				return m
			}
		}
		r.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s alert arrived (got %v)", event, r.events())
	return nil
}

func TestAlertAddressesFromTheScreen(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	rcv := newReceiver(t)
	code, loc, _ := h.post("/settings/alerts", url.Values{"name": {"ops"}, "url": {rcv.srv.URL + "/hook/very-secret-path"}, "secret": {"s3cret"}})
	if code != http.StatusSeeOther || !strings.Contains(loc, "ok=alert-added") {
		t.Fatalf("%d %q", code, loc)
	}
	_, _, body := h.get("/settings")
	if !strings.Contains(body, "<b>ops</b>") || strings.Contains(body, "very-secret-path") || !strings.Contains(body, "서명") {
		t.Fatal("the address is shown masked, with its format")
	}
	if _, loc, _ := h.post("/settings/alerts/1/test", nil); !strings.Contains(loc, "ok=alert-tested") {
		t.Fatalf("test: %q", loc)
	}
	if m := rcv.waitFor(t, "test"); m["level"] != "info" || rcv.sigs[0] == "" {
		t.Fatalf("a signed test arrives: %v %v", m, rcv.sigs)
	}
	if _, _, body := h.get("/settings"); !strings.Contains(body, "최근에 보낸 것") || !strings.Contains(body, "Naru 알림 시험") {
		t.Fatal("the result is listed")
	}
	if code, _, body := h.post("/settings/alerts", url.Values{"url": {"ftp://example.com/x"}}); code != http.StatusBadRequest || !strings.Contains(body, "https://") {
		t.Fatalf("a bad address is refused: %d", code)
	}

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "gone", http.StatusGone) }))
	defer dead.Close()
	h.post("/settings/alerts", url.Values{"url": {dead.URL + "/x"}})
	if code, _, body := h.post("/settings/alerts/2/test", nil); code != http.StatusBadGateway || !strings.Contains(body, "410") || strings.Contains(body, dead.URL+"/x") {
		t.Fatalf("a failing address says why, without the full address: %d", code)
	}
	h.post("/settings/alerts/2/delete", nil)
	if _, _, body := h.get("/settings"); strings.Contains(body, "/settings/alerts/2/test") {
		t.Fatal("removed")
	}
}

func TestAServiceThatStopsAndComesBackIsReported(t *testing.T) {
	h := newHarnessWith(t, harnessOpts{install: "docker", watchEvery: 20 * time.Millisecond})
	h.signedIn()
	rcv := newReceiver(t)
	h.post("/settings/alerts", url.Values{"url": {rcv.srv.URL + "/hook"}})
	id := h.createService(model.NewService{Name: name("blog"), Kind: model.KindImage, Source: "me/blog", Alias: "naru-blog", Port: 80}, model.ServiceSecrets{})
	store.NewLiveStates(h.app.db).Save(ctx, id, model.LiveState{Alias: "naru-blog", Instance: "naru-blog-1", Port: 80})
	h.docker.set("naru-blog-1", model.ContainerState{Name: "naru-blog-1", Running: true, State: "running"})
	time.Sleep(100 * time.Millisecond) // 건강한 모습을 먼저 본다

	h.docker.set("naru-blog-1", model.ContainerState{Name: "naru-blog-1", State: "exited", ExitCode: 1})
	down := rcv.waitFor(t, "service.down")
	if down["service"] != "blog" || down["level"] != "problem" || !strings.Contains(fmt.Sprint(down["detail"]), "멈췄어요") {
		t.Fatalf("%v", down)
	}
	h.docker.set("naru-blog-1", model.ContainerState{Name: "naru-blog-1", Running: true, State: "running"})
	if up := rcv.waitFor(t, "service.up"); up["level"] != "recovery" {
		t.Fatalf("%v", up)
	}
	time.Sleep(100 * time.Millisecond)
	count := 0
	for _, e := range rcv.events() {
		if e == "service.down" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("one outage, one alert: %v", rcv.events())
	}
}

// ── 진단 (M6-3) ─────────────────────────────

func TestWrongPortIsDiagnosedAndFixedFromHome(t *testing.T) {
	h := newHarnessWith(t, harnessOpts{install: "docker", watchEvery: 20 * time.Millisecond})
	h.signedIn()
	id := h.createService(model.NewService{Name: name("blog"), Kind: model.KindImage, Source: "me/blog", Alias: "naru-blog", Port: 3000}, model.ServiceSecrets{})
	store.NewLiveStates(h.app.db).Save(ctx, id, model.LiveState{Alias: "naru-blog", Instance: "naru-blog-1", Port: 3000})
	h.docker.set("naru-blog-1", model.ContainerState{Name: "naru-blog-1", Running: true, State: "running", IP: "172.30.0.5", Ports: []model.Port{8080}})
	h.docker.listen("172.30.0.5", 8080)                                                              // 이미지가 8080을 열었고, 앱도 거기서 듣는다
	h.post(fmt.Sprintf("/services/%d/domains", id), url.Values{"domain": {"elsewhere.example.com"}}) // DNS는 다른 서버를 가리킨다

	home := func(want string) string {
		t.Helper()
		for i := 0; i < 300; i++ {
			if _, _, body := h.get("/"); strings.Contains(body, want) {
				return body
			}
			time.Sleep(10 * time.Millisecond)
		}
		_, _, body := h.get("/")
		t.Fatalf("home never showed %q:\n%s", want, body)
		return ""
	}
	body := home(fmt.Sprintf(`action="/services/%d/port"`, id))
	for _, want := range []string{"주의가 필요한 것", "앱이 8080 포트를 듣고 있어요", "포트를 8080로 바꾸기",
		"elsewhere.example.com 주소가 다른 서버를 가리켜요", "203.0.113.9", "198.51.100.24"} {
		if !strings.Contains(body, want) {
			t.Errorf("home is missing %q", want)
		}
	}
	if strings.Contains(body, "앱이 응답하지 않아요") {
		t.Error("a known fix replaces the generic \"not answering\"")
	}
	if i, j := strings.Index(body, "8080 포트를 듣고"), strings.Index(body, "다른 서버를 가리켜요"); j < i {
		t.Error("heaviest first")
	}
	if _, _, page := h.get(fmt.Sprintf("/services/%d", id)); !strings.Contains(page, "포트를 8080로 바꾸기") {
		t.Error("the service page shows its own findings")
	}

	code, loc, _ := h.post(fmt.Sprintf("/services/%d/port", id), url.Values{"port": {"8080"}})
	if code != http.StatusSeeOther || !strings.Contains(loc, "ok=port") {
		t.Fatalf("%d %q", code, loc)
	}
	if h.service(int64(id)).Port != 8080 {
		t.Fatal("the port is saved")
	}
	h.waitFor("the web server follows the new port", func(cfg string) bool { return strings.Contains(cfg, "naru-blog:8080") })
	if deploys, _ := h.history().Recent(ctx, id, 5); len(deploys) != 0 {
		t.Fatal("no redeploy — the app already listens there")
	}
	body = home("다른 서버를 가리켜요")
	for i := 0; i < 300 && strings.Contains(body, "8080 포트를 듣고"); i++ {
		time.Sleep(10 * time.Millisecond)
		_, _, body = h.get("/")
	}
	if strings.Contains(body, "8080 포트를 듣고") {
		t.Fatal("fixed, so gone")
	}

	s := h.service(int64(id))
	h.post(fmt.Sprintf("/services/%d/domains/%d/delete", id, s.Domains[0].ID), nil)
	home("모두 정상이에요")

	if code, _, _ := h.post(fmt.Sprintf("/services/%d/port", id), url.Values{"port": {"0"}}); code != http.StatusBadRequest {
		t.Fatalf("a port is required: %d", code)
	}
}

func TestAFailedDeployIsReported(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	rcv := newReceiver(t)
	h.post("/settings/alerts", url.Values{"url": {rcv.srv.URL + "/hook"}})
	// 저장소에 없는 빌드 경로 — 배포가 확실히 실패한다
	id := h.createService(model.NewService{Name: name("app"), Kind: model.KindRepo, Source: "https://github.com/me/app", Branch: "main", BuildPath: "no/such/folder"}, model.ServiceSecrets{})
	h.post(fmt.Sprintf("/services/%d/deploy", id), nil)
	h.app.wait()
	if m := rcv.waitFor(t, "deploy.failed"); m["service"] != "app" || m["level"] != "problem" {
		t.Fatalf("%v", m)
	}
}

// ── 로그 (M6-2) ─────────────────────────────

func TestLogsPageMixesAppOutputAndRequests(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	id := h.createService(model.NewService{Name: name("blog"), Kind: model.KindImage, Source: "me/blog", Alias: "naru-blog", Port: 80}, model.ServiceSecrets{})
	store.NewLiveStates(h.app.db).Save(ctx, id, model.LiveState{Alias: "naru-blog", Instance: "naru-blog-1", Port: 80})
	h.post(fmt.Sprintf("/services/%d/domains", id), url.Values{"domain": {"blog.example.com"}, "https": {"1"}})
	// 웹서버가 남긴 접근 로그 (Docker 방식: <데이터>/caddy/data/access.log)
	logDir := filepath.Join(h.app.cfg.DataDir, "caddy", "data")
	os.MkdirAll(logDir, 0o755)
	os.WriteFile(filepath.Join(logDir, "access.log"), []byte(
		`{"logger":"http.log.access.access","ts":1791633602.0,"request":{"host":"blog.example.com","method":"GET","uri":"/posts?page=2"},"status":200,"duration":0.012}`+"\n"+
			`{"logger":"http.log.access.access","ts":1791633602.5,"request":{"host":"other.example.com","method":"GET","uri":"/x"},"status":404,"duration":0.001}`+"\n"), 0o644)

	code, _, body := h.get(fmt.Sprintf("/services/%d/logs", id))
	if code != http.StatusOK {
		t.Fatalf("%d", code)
	}
	for _, want := range []string{"naru-blog-1 listening on :80", "/posts?page=2", "12ms", "&lt;script&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("logs page is missing %q", want)
		}
	}
	if strings.Contains(body, "other.example.com") || strings.Contains(body, "<script>alert") {
		t.Fatal("only this service's requests, and app output is escaped")
	}
	if i, j := strings.Index(body, "listening on"), strings.Index(body, "/posts"); i < 0 || j < i {
		t.Fatal("in time order")
	}
	if _, _, body := h.get(fmt.Sprintf("/services/%d/logs?show=request", id)); strings.Contains(body, "listening on") || !strings.Contains(body, "/posts") {
		t.Fatal("request filter")
	}
	if _, _, body := h.get(fmt.Sprintf("/services/%d", id)); !strings.Contains(body, fmt.Sprintf("/services/%d/logs", id)) {
		t.Fatal("the service page links to its logs")
	}
	if code, _, _ := h.get("/services/999/logs"); code != http.StatusNotFound {
		t.Fatal("unknown service")
	}
	h.waitFor("the web server logs this service's requests only", func(cfg string) bool {
		return strings.Contains(cfg, `"filename": "/data/access.log"`) && strings.Contains(cfg, `"blog.example.com": [`) && strings.Contains(cfg, `"skip_unmapped_hosts": true`)
	})
}

// ── 웹훅 ─────────────────────────────────

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (h *harness) hook(id model.ServiceID, body string, headers map[string]string) (int, string) {
	h.t.Helper()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/hooks/%d", h.srv.URL, id), strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestWebhooks(t *testing.T) {
	h := newHarness(t, "")
	id := h.createService(model.NewService{Name: name("blog"), Kind: model.KindRepo, Source: "https://github.com/me/blog", Branch: "main", AutoDeploy: true},
		model.ServiceSecrets{WebhookSecret: "s3cret"})
	push := `{"ref":"refs/heads/main"}`
	deploys := func() int {
		all, _ := h.history().Recent(ctx, id, 100)
		return len(all)
	}

	if code, _ := h.hook(id, push, nil); code != http.StatusUnauthorized {
		t.Fatalf("unsigned → 401, got %d", code)
	}
	if code, _ := h.hook(id, push, map[string]string{"X-Hub-Signature-256": sign("wrong", []byte(push))}); code != http.StatusUnauthorized {
		t.Fatal("wrong secret → 401")
	}
	if code, _ := h.hook(id, `{"ref":"refs/heads/evil"}`, map[string]string{"X-Hub-Signature-256": sign("s3cret", []byte(push))}); code != http.StatusUnauthorized {
		t.Fatal("a signature for another body does not count")
	}
	if code, _ := h.hook(id, "{}", map[string]string{"X-GitHub-Event": "ping", "X-Hub-Signature-256": sign("s3cret", []byte("{}"))}); code != http.StatusOK {
		t.Fatal("ping → pong")
	}
	other := `{"ref":"refs/heads/feature"}`
	if code, body := h.hook(id, other, map[string]string{"X-Hub-Signature-256": sign("s3cret", []byte(other))}); code != http.StatusOK || !strings.Contains(body, "ignored") {
		t.Fatalf("other branches are acknowledged and ignored: %d %s", code, body)
	}
	if deploys() != 0 {
		t.Fatal("nothing deployed yet")
	}
	h.hold()
	if code, body := h.hook(id, push, map[string]string{"X-Hub-Signature-256": sign("s3cret", []byte(push))}); code != http.StatusAccepted || !strings.Contains(body, "deploying") {
		t.Fatalf("a signed push to main deploys: %d %s", code, body)
	}
	if code, body := h.hook(id, push, map[string]string{"X-Gitlab-Token": "s3cret"}); code != http.StatusAccepted || !strings.Contains(body, "another will follow") {
		t.Fatalf("GitLab token works too; pushes during a deploy are queued: %d %s", code, body)
	}
	if code, _ := h.hook(id, push, map[string]string{}); code != http.StatusUnauthorized {
		t.Fatal("still 401 without a signature")
	}
	if code, _ := h.hook(id, push+"", map[string]string{"X-Unused": "x"}); code != http.StatusUnauthorized {
		t.Fatal("unknown headers do not count")
	}
	if s := h.service(int64(id)); s.HookLog.At.IsZero() || s.HookLog.Result != "queued" {
		t.Fatalf("the last receipt is recorded: %+v", s.HookLog)
	}
	h.release()
	h.app.wait()
	if deploys() != 2 {
		t.Fatalf("pushes during a deploy collapse into one more deploy, got %d", deploys())
	}
	if code, _ := h.hook(999, push, nil); code != http.StatusNotFound {
		t.Fatal("unknown service → 404")
	}
	big := strings.Repeat("x", 1<<20+10)
	if code, _ := h.hook(id, big, map[string]string{"X-Gitlab-Token": "s3cret"}); code != http.StatusRequestEntityTooLarge {
		t.Fatal("bodies are limited")
	}
	q := url.Values{"secret": {"s3cret"}}
	if code, _ := h.hook(id, "{}", nil); code != http.StatusUnauthorized {
		t.Fatal("no secret")
	}
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/hooks/%d?%s", h.srv.URL, id, q.Encode()), strings.NewReader("{}"))
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	h.app.wait()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("registries that cannot sign use ?secret=: %d", res.StatusCode)
	}
}

// ── 복구 명령이 쓰는 조립 ────────────────────────

func TestOpeningForAShellCommandLeavesTheRunningServerAlone(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work", "blog-7")
	os.MkdirAll(work, 0o755)
	cfg := Config{DataDir: dir, CaddySocket: "/run/caddy/admin.sock", SelfUpstream: "naru:8080"}
	first, err := OpenWith(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), Outside{Clock: system.Clock{}, Random: system.Random{}, Sender: &fakeCaddy{}})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := store.NewServices(first.db).Create(ctx, model.NewService{Name: name("blog"), Kind: model.KindImage})
	did, _ := store.NewDeployments(first.db).Start(ctx, id, model.ReasonManual)
	first.Close()

	shell, err := OpenWith(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), Outside{Clock: system.Clock{}, Random: system.Random{}, Sender: &fakeCaddy{}})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.Close()
	if _, err := os.Stat(work); err != nil {
		t.Fatal("a shell command must not clear the server's work folders")
	}
	if d, _ := store.NewDeployments(shell.db).Get(ctx, did); d.Status != model.DeployRunning {
		t.Fatal("a shell command must not close the server's running deploys")
	}
}
