package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/KangminNa/naru/internal/auth"
	"github.com/KangminNa/naru/internal/docker"
	"github.com/KangminNa/naru/internal/engine"
	"github.com/KangminNa/naru/internal/hostinfo"
	"github.com/KangminNa/naru/internal/service"
	"github.com/KangminNa/naru/internal/store"
)

type fakeContainers struct {
	all  map[string]docker.Container
	down bool
}

func (f *fakeContainers) Containers(context.Context) (map[string]docker.Container, error) {
	if f.down {
		return nil, io.ErrUnexpectedEOF
	}
	return f.all, nil
}

type fakeEngine struct {
	status engine.Status
	kicks  int
}

func (f *fakeEngine) Status() engine.Status { return f.status }
func (f *fakeEngine) Kick()                 { f.kicks++ }

type harness struct {
	t          *testing.T
	srv        *httptest.Server
	client     *http.Client
	auth       *auth.Service
	store      *store.Store
	services   *service.Repo
	containers *fakeContainers
	engine     *fakeEngine
}

// newHarness는 실제 화면 서버를 띄운다. DNS는 naru.example.com → 198.51.100.24 로 고정한다.
func newHarness(t *testing.T, envDomain string) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a, _ := auth.New(st.DB)
	lookup := func(_ context.Context, host string) ([]string, error) {
		if host == "naru.example.com" {
			return []string{"198.51.100.24"}, nil
		}
		if host == "elsewhere.example.com" {
			return []string{"203.0.113.9"}, nil
		}
		return nil, &url.Error{Op: "lookup", URL: host}
	}
	repo := service.NewRepo(st.DB)
	fc := &fakeContainers{all: map[string]docker.Container{}}
	fe := &fakeEngine{status: engine.Status{Connected: true}}
	s, err := New(Deps{
		Store: st, Auth: a, Services: repo, Containers: fc, Engine: fe,
		Host: hostinfo.NewSampler(dir, dir), Lookup: lookup,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), DataDir: dir, Version: "test",
		EnvAdminDomain: envDomain,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &harness{t: t, srv: ts, client: client, auth: a, store: st, services: repo, containers: fc, engine: fe}
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

// setupThrough는 마법사를 계정 단계까지 지난다.
func (h *harness) createAccount() {
	h.t.Helper()
	code, loc, _ := h.post("/setup", url.Values{"token": {h.auth.SetupToken()}, "username": {"admin"}, "password": {"longenough"}, "password2": {"longenough"}})
	if code != http.StatusSeeOther || loc != "/setup/domain" {
		h.t.Fatalf("account step: %d → %q", code, loc)
	}
}

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
	if code, _, body := h.get("/setup?token=" + h.auth.SetupToken()); code != http.StatusOK || !strings.Contains(body, `name="password"`) {
		t.Fatal("the right token shows the account form")
	}
}

func TestSetupRefusesWithoutTheToken(t *testing.T) {
	h := newHarness(t, "")
	code, _, _ := h.post("/setup", url.Values{"token": {"guess"}, "username": {"intruder"}, "password": {"longenough"}, "password2": {"longenough"}})
	if code != http.StatusForbidden || !h.auth.NeedsSetup() {
		t.Fatalf("an account must not be created without the token (status %d)", code)
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
	if _, set := h.store.Setting(store.KeyAdminDomain); set {
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
	if v, _ := h.store.Setting(store.KeyAdminDomain); v != "naru.example.com" {
		t.Fatalf("the domain is saved normalized, got %q", v)
	}

	if code, _, _ := h.post("/setup/https", url.Values{"email": {"not-an-email"}, "action": {"save"}}); code != http.StatusBadRequest {
		t.Fatal("a malformed email is refused")
	}
	// 테스트 서버는 127.0.0.1로 들어와 있으니 관리 주소로 가라고 안내한다
	if code, _, body := h.post("/setup/https", url.Values{"email": {"me@example.com"}, "action": {"save"}}); code != http.StatusOK || !strings.Contains(body, "http://naru.example.com/login") {
		t.Fatalf("finishing points to the admin address: %d", code)
	}
	if v, _ := h.store.Setting(store.KeyACMEEmail); v != "me@example.com" {
		t.Fatal("the email is saved")
	}
	code, _, body = h.get("/?ok=setup")
	if code != http.StatusOK || !strings.Contains(body, "설정을 마쳤어요") || strings.Contains(body, "관리 화면 주소가 아직 없어요") {
		t.Fatalf("home after setup: %d", code)
	}
	if code, loc, _ := h.get("/setup"); code != http.StatusSeeOther || loc != "/" {
		t.Fatal("setup is closed once done")
	}
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
	h.store.SetSetting(store.KeySetupDone, "1")
	h.post("/logout", nil)
	if code, loc, _ := h.get("/"); code != http.StatusSeeOther || loc != "/login" {
		t.Fatalf("signed out → login, got %d %q", code, loc)
	}
	if code, _, _ := h.post("/login", url.Values{"username": {"admin"}, "password": {"wrong"}}); code != http.StatusUnauthorized {
		t.Fatal("a wrong password is 401")
	}
	for i := 0; i < auth.MaxLoginFailures; i++ {
		h.post("/login", url.Values{"username": {"admin"}, "password": {"wrong"}})
	}
	if code, _, body := h.post("/login", url.Values{"username": {"admin"}, "password": {"longenough"}}); code != http.StatusTooManyRequests || !strings.Contains(body, "15분") {
		t.Fatalf("locked out, got %d", code)
	}
}

func TestChangePasswordFromSettings(t *testing.T) {
	h := newHarness(t, "")
	h.createAccount()
	h.store.SetSetting(store.KeySetupDone, "1")

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
	if _, err := h.auth.Login("ip", "admin", "newpassword1"); err != nil {
		t.Fatal("the new password works")
	}
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

// 문구 키는 두 언어에 모두 있어야 하고, 템플릿이 쓰는 키는 모두 있어야 한다.
func TestMessagesAreComplete(t *testing.T) {
	for key := range messages["ko"] {
		if _, ok := messages["en"][key]; !ok {
			t.Errorf("en is missing %q", key)
		}
	}
	for key := range messages["en"] {
		if _, ok := messages["ko"][key]; !ok {
			t.Errorf("ko is missing %q", key)
		}
	}
	used := regexp.MustCompile(`\{\{-?\s*t "([a-z0-9.]+)"`)
	entries, _ := templateFS.ReadDir("templates")
	for _, e := range entries {
		body, _ := templateFS.ReadFile("templates/" + e.Name())
		for _, m := range used.FindAllStringSubmatch(string(body), -1) {
			if _, ok := messages["ko"][m[1]]; !ok {
				t.Errorf("%s uses unknown key %q", e.Name(), m[1])
			}
		}
	}
	for _, key := range okKeys {
		if _, ok := messages["ko"][key]; !ok {
			t.Errorf("flash key %q has no message", key)
		}
	}
}

// signedIn은 설정을 마치고 로그인한 상태로 만든다.
func (h *harness) signedIn() {
	h.t.Helper()
	h.createAccount()
	h.store.SetSetting(store.KeySetupDone, "1")
	h.store.SetSetting(store.KeyAdminDomain, "naru.example.com")
}

func TestHomeListsServicesWithTheirState(t *testing.T) {
	h := newHarness(t, "")
	h.signedIn()
	blog, _ := h.services.Create(service.Service{Name: "blog", Kind: service.KindRepo, Container: "shelf-blog", Port: 3000}, service.Secrets{})
	h.services.AddDomain(blog, service.Domain{Domain: "blog.example.com", HTTPS: true})
	h.services.Create(service.Service{Name: "api", Kind: service.KindImage, Container: "naru-api", Port: 8080}, service.Secrets{})
	h.services.Create(service.Service{Name: "nas", Kind: service.KindExternal, Upstream: "host.docker.internal:5000"}, service.Secrets{})
	h.containers.all["shelf-blog"] = docker.Container{Name: "shelf-blog", State: docker.Running, Status: "Up 3 hours"}

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
	h.services.Create(service.Service{Name: "blog", Kind: service.KindRepo, Container: "shelf-blog", Port: 3000}, service.Secrets{})
	h.containers.down = true
	h.engine.status = engine.Status{LastError: "caddy: dial unix /run/caddy/admin.sock: connect: no such file"}
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
	id, _ := h.services.Create(service.Service{Name: "landing", Kind: service.KindRepo, Source: "https://github.com/me/landing", Container: "shelf-landing", Port: 4023, BuildPath: "site"}, service.Secrets{GitToken: "ghp_secret", WebhookSecret: "whsec_secret"})
	h.services.AddDomain(id, service.Domain{Domain: "www.example.com", HTTPS: true, HSTS: true})
	h.containers.all["shelf-landing"] = docker.Container{State: docker.Restarting, Status: "Restarting (1) 3 seconds ago"}

	code, _, body := h.get("/services/1")
	if code != http.StatusOK {
		t.Fatalf("got %d", code)
	}
	for _, want := range []string{"landing", "shelf-landing:4023", "www.example.com", "HSTS", "계속 다시 시작됨", "https://github.com/me/landing", "site"} {
		if !strings.Contains(body, want) {
			t.Errorf("service page is missing %q", want)
		}
	}
	for _, secret := range []string{"ghp_secret", "whsec_secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("secrets never reach the screen: %q", secret)
		}
	}
	for _, p := range []string{"/services/99", "/services/abc"} {
		if code, _, _ := h.get(p); code != http.StatusNotFound {
			t.Errorf("%s: got %d", p, code)
		}
	}
}

func TestAdminAddressChangesReachTheEngine(t *testing.T) {
	h := newHarness(t, "")
	h.createAccount()
	h.post("/setup/domain", url.Values{"domain": {"naru.example.com"}, "action": {"save"}})
	if h.engine.kicks != 1 {
		t.Fatalf("saving the admin address tells the engine, kicks=%d", h.engine.kicks)
	}
	// IP나 다른 주소로 들어와 있었다면, 설정을 마치면 관리 주소로 안내한다
	code, _, body := h.post("/setup/https", url.Values{"action": {"skip"}})
	if code != http.StatusOK || !strings.Contains(body, "http://naru.example.com/login") {
		t.Fatalf("finishing from another host points to the admin address: %d", code)
	}
	if h.engine.kicks != 2 {
		t.Fatal("finishing setup closes the IP fallback — the engine must know")
	}
}

// CSP가 인라인 style을 막으므로 템플릿에 style 속성이 있으면 화면이 조용히 깨진다.
func TestTemplatesHaveNoInlineStyles(t *testing.T) {
	entries, _ := templateFS.ReadDir("templates")
	for _, e := range entries {
		body, _ := templateFS.ReadFile("templates/" + e.Name())
		if strings.Contains(string(body), "style=") {
			t.Errorf("%s has an inline style attribute — CSP blocks it; use a class", e.Name())
		}
	}
}
