package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/KangminNa/naru/internal/model"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

const sock = "/run/caddy/admin.sock"

// 픽스처는 testdata/에 골든 파일로 남는다. scripts/caddy-validate.sh가 실제 Caddy로 검증한다.
var fixtures = map[string]model.SiteMap{
	"setup-open": {AdminSocket: sock, AdminUpstream: "naru:8080", OpenFallback: true},
	"full": {
		AdminSocket: sock, AdminUpstream: "naru:8080", AdminHosts: []string{"naru.example.com"}, AdminHTTPS: true,
		ACMEEmail: "me@example.com",
		Sites: []model.Site{
			{Hosts: []string{"www.example.com"}, Destination: model.Destination{Address: "shelf-landing:4023"}, HTTPS: true, HSTS: true},
			{Hosts: []string{"blog.example.com"}, Destination: model.Destination{Address: "shelf-blog:3000"}, HTTPS: true},
			{Hosts: []string{"nas.example.com"}, Destination: model.Destination{Address: "host.docker.internal:5000"}},
			{Hosts: []string{"router.example.com"}, Destination: model.Destination{Address: "https://192.168.0.1:443"}, HTTPS: true},
			{Hosts: []string{"broken.example.com"}, Destination: model.Destination{Address: ""}},
			{Hosts: []string{"static.example.com"}, Destination: model.Destination{Folder: "/srv/sites/landing/12"}, HTTPS: true},
		},
	},
	// 인증서가 있는 주소만 넘긴다 — 그 판단은 webserver가 하고, 여기서는 지도대로 그리는지만 본다
	"redirects": {
		AdminSocket: sock, AdminUpstream: "naru:8080", AdminHosts: []string{"naru.example.com"}, AdminHTTPS: true, AdminRedirectHTTP: true,
		ACMEEmail: "me@example.com",
		Sites: []model.Site{
			{Hosts: []string{"ready.example.com"}, Destination: model.Destination{Address: "naru-ready:80"}, HTTPS: true, RedirectHTTP: true, HSTS: true},
			{Hosts: []string{"waiting.example.com"}, Destination: model.Destination{Address: "naru-waiting:80"}, HTTPS: true},
			{Hosts: []string{"plain.example.com"}, Destination: model.Destination{Address: "naru-plain:80"}, RedirectHTTP: true},
		},
	},
	"redirects-dev-port": {
		AdminSocket: sock, AdminUpstream: "naru:8080", AdminHosts: []string{"naru.localhost"}, AdminHTTPS: true, InternalTLS: true, HTTPSPort: 8443,
		Sites: []model.Site{{Hosts: []string{"site.localhost"}, Destination: model.Destination{Folder: "/srv/sites/site/3"}, HTTPS: true, RedirectHTTP: true}},
	},
	// 웹서버 설정이 모두 켜진 사이트 — 순서: IP 제한 → 점검 중 → 비밀번호 → 헤더·압축 → 고급 → 경로 → 기본 목적지
	"web-settings": {
		AdminSocket: sock, AdminUpstream: "naru:8080", ACMEEmail: "me@example.com",
		Sites: []model.Site{{
			Hosts: []string{"shop.example.com"}, Destination: model.Destination{Address: "naru-shop:3000"}, HTTPS: true, HSTS: true,
			Settings: model.SiteSettings{
				Headers:     []model.HeaderRule{{Name: "X-Frame-Options", Value: "DENY"}, {Name: "Server", Value: ""}},
				AllowFrom:   []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.1.5/32")},
				Login:       model.BasicLogin{User: "admin", Hash: "$2a$10$Ar0a1Qe5b8fTkNw0XQ3WKu3m1bcEw7oZp6qeD4JW9mH8l2sXh9mLu"},
				Maintenance: false,
				Paths: []model.SitePath{
					{Prefix: mustPrefix("/api"), Destination: model.Destination{Address: "naru-api:8080"}, StripPrefix: true},
					{Prefix: mustPrefix("/docs"), Destination: model.Destination{Folder: "/srv/sites/docs/4"}},
				},
				Compiled: []byte(`[{"match":[{"path":["/health"]}],"handle":[{"handler":"static_response","status_code":200}]}]`),
			},
		}, {
			Hosts: []string{"down.example.com"}, Destination: model.Destination{Address: "naru-down:80"},
			Settings: model.SiteSettings{Maintenance: true},
		}},
	},
	"internal-tls": {
		AdminSocket: sock, AdminUpstream: "naru:8080", AdminHosts: []string{"naru.localhost"}, AdminHTTPS: true, InternalTLS: true,
		Sites: []model.Site{{Hosts: []string{"app.localhost"}, Destination: model.Destination{Address: "naru-app:3000"}, HTTPS: true}},
	},
}

func TestGolden(t *testing.T) {
	for name, p := range fixtures {
		got, err := (JSONWriter{}).Write(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		path := filepath.Join("testdata", name+".json")
		if *update {
			os.MkdirAll("testdata", 0o755)
			os.WriteFile(path, append(got, '\n'), 0o644)
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: run `go test ./internal/caddy -update` to create it: %v", name, err)
		}
		if !bytes.Equal(bytes.TrimSpace(want), bytes.TrimSpace(got)) {
			t.Errorf("%s: rendered config changed — check it, then run with -update\n%s", name, got)
		}
	}
}

// decoded는 테스트에서 설정을 들여다보기 쉽게 푼다.
type decoded struct {
	Admin struct{ Listen string }
	Apps  struct {
		HTTP struct {
			Servers map[string]struct {
				Listen         []string
				AutomaticHTTPS struct {
					DisableRedirects bool `json:"disable_redirects"`
				} `json:"automatic_https"`
				Routes []struct {
					Match []struct {
						Host []string
						Not  []struct{ Path []string }
					}
					Handle []map[string]any
				}
			}
		}
		TLS *struct {
			Automation struct {
				Policies []struct{ Issuers []map[string]any }
			}
		}
	}
}

func decode(t *testing.T, p model.SiteMap) decoded {
	t.Helper()
	b, err := (JSONWriter{}).Write(p)
	if err != nil {
		t.Fatal(err)
	}
	var d decoded
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestInvariantsHoldForEveryFixture(t *testing.T) {
	for name, p := range fixtures {
		d := decode(t, p)
		if d.Admin.Listen != "unix//run/caddy/admin.sock" {
			t.Errorf("%s: the admin socket must always be in the config (invariant 2), got %q", name, d.Admin.Listen)
		}
		for sname, s := range d.Apps.HTTP.Servers {
			if len(s.Listen) != 1 {
				t.Errorf("%s/%s: :80 and :443 are separate servers (invariant 3)", name, sname)
			}
			if !s.AutomaticHTTPS.DisableRedirects {
				t.Errorf("%s/%s: Caddy must not draw redirects on its own (invariant 3/4)", name, sname)
			}
			for _, r := range s.Routes {
				for _, h := range r.Handle {
					hs, ok := h["headers"].(map[string]any)
					if h["handler"] != "static_response" || !ok || hs["Location"] == nil {
						continue
					}
					// 넘기기는 :80에서, 지도가 넘기라고 한 HTTPS 주소에만, 인증서 확인 경로는 빼고, 307로 (불변식 4)
					if sname != "http" || len(r.Match) != 1 || h["status_code"] != float64(307) {
						t.Errorf("%s/%s: a redirect must be a 307 on :80 for one host match", name, sname)
						continue
					}
					if len(r.Match[0].Not) != 1 || r.Match[0].Not[0].Path[0] != acmeChallenge {
						t.Errorf("%s: certificate challenges must never be redirected", name)
					}
					for _, host := range r.Match[0].Host {
						if !redirectAllowed(p, host) {
							t.Errorf("%s: %s is redirected but the site map did not ask for it (invariant 4)", name, host)
						}
					}
				}
			}
		}
		raw, _ := (JSONWriter{}).Write(p)
		for _, secret := range []string{"whsec", "ghp_"} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("%s: secrets never go into the Caddy config (invariant 5)", name)
			}
		}
		// 예외 하나: 기본 인증은 Caddy가 검사하므로 bcrypt 해시만 들어간다 — 원문은 안 된다
		for _, m := range regexp.MustCompile(`"password": "([^"]*)"`).FindAllStringSubmatch(string(raw), -1) {
			if !strings.HasPrefix(m[1], "$2") {
				t.Errorf("%s: only bcrypt hashes may appear as passwords (invariant 5), got %q", name, m[1])
			}
		}
	}
}

func mustPrefix(s string) model.PathPrefix {
	p, err := model.ParsePathPrefix(s)
	if err != nil {
		panic(err)
	}
	return p
}

// siteHandlers는 그 주소의 :443(없으면 :80) 경로가 거치는 처리기 이름을 순서대로 — 하위 경로까지 펼친다.
func siteHandlers(t *testing.T, cfg []byte, host string) []string {
	t.Helper()
	var c struct {
		Apps struct {
			HTTP struct {
				Servers map[string]struct{ Routes []json.RawMessage }
			}
		}
	}
	json.Unmarshal(cfg, &c)
	var walk func(routes []json.RawMessage) []string
	walk = func(routes []json.RawMessage) []string {
		var out []string
		for _, raw := range routes {
			var r struct {
				Match  []map[string]any
				Handle []map[string]any
			}
			json.Unmarshal(raw, &r)
			for _, h := range r.Handle {
				name := h["handler"].(string)
				if name == "subroute" {
					b, _ := json.Marshal(h["routes"])
					var sub []json.RawMessage
					json.Unmarshal(b, &sub)
					out = append(out, walk(sub)...)
					continue
				}
				if name == "static_response" {
					name += fmt.Sprint(h["status_code"])
				}
				out = append(out, name)
			}
		}
		return out
	}
	for _, server := range []string{"https", "http"} {
		for _, raw := range c.Apps.HTTP.Servers[server].Routes {
			var flat bytes.Buffer
			json.Compact(&flat, raw)
			if strings.Contains(flat.String(), `"host":["`+host+`"]`) && !strings.Contains(flat.String(), `"Location"`) {
				return walk([]json.RawMessage{raw})
			}
		}
	}
	return nil
}

func TestWebSettingsAreDrawnInOrder(t *testing.T) {
	cfg, err := (JSONWriter{}).Write(fixtures["web-settings"])
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(siteHandlers(t, cfg, "shop.example.com"), " ")
	want := "static_response403 authentication headers encode static_response200 rewrite reverse_proxy file_server reverse_proxy"
	if got != want {
		t.Fatalf("handlers:\n got %s\nwant %s", got, want)
	}
	if got := strings.Join(siteHandlers(t, cfg, "down.example.com"), " "); got != "static_response503 encode reverse_proxy" {
		t.Fatalf("maintenance answers before anything else: %s", got)
	}
}

func TestEverySiteIsCompressed(t *testing.T) {
	for name, m := range fixtures {
		cfg, _ := (JSONWriter{}).Write(m)
		for _, s := range m.Sites {
			if !strings.Contains(strings.Join(siteHandlers(t, cfg, s.Hosts[0]), " "), "encode") {
				t.Errorf("%s/%s: compression is always on", name, s.Hosts[0])
			}
		}
	}
}

// 인증서 확인 요청을 막으면 발급·갱신이 실패한다 — IP 제한·점검 중·비밀번호는 그 경로를 빼고 건다 (불변식 4의 연장).
func TestGuardsNeverBlockCertificateChallenges(t *testing.T) {
	cfg, _ := (JSONWriter{}).Write(fixtures["web-settings"])
	var walk func(v any)
	guards := 0
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if hs, ok := x["handle"].([]any); ok && len(hs) > 0 {
				h := hs[0].(map[string]any)
				guard := h["handler"] == "authentication" || (h["handler"] == "static_response" && (h["status_code"] == float64(403) || h["status_code"] == float64(503)))
				if guard {
					guards++
					if !strings.Contains(fmt.Sprint(x["match"]), acmeChallenge) {
						t.Errorf("a guard must skip %s: %v", acmeChallenge, x["match"])
					}
				}
			}
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	var root any
	json.Unmarshal(cfg, &root)
	walk(root)
	if guards < 4 { // 403·인증이 :80과 :443에, 503이 :80에
		t.Fatalf("expected guards on both ports, found %d", guards)
	}
	if strings.Contains(string(cfg), "admin\",\"password") {
		t.Fatal("only the bcrypt hash goes in")
	}
}

// redirectAllowed는 지도가 그 주소를 넘기라고 했는가 — HTTPS를 켜고, 인증서가 있다고 한 주소뿐이다.
func redirectAllowed(m model.SiteMap, host string) bool {
	for _, h := range m.AdminHosts {
		if h == host {
			return m.AdminHTTPS && m.AdminRedirectHTTP
		}
	}
	for _, s := range m.Sites {
		for _, h := range s.Hosts {
			if h == host {
				return s.HTTPS && s.RedirectHTTP
			}
		}
	}
	return false
}

func TestRedirectsFollowTheSiteMap(t *testing.T) {
	d := decode(t, fixtures["redirects"])
	redirected := map[string]string{}
	for _, r := range d.Apps.HTTP.Servers["http"].Routes {
		for _, h := range r.Handle {
			if hs, ok := h["headers"].(map[string]any); ok && hs["Location"] != nil {
				redirected[r.Match[0].Host[0]] = hs["Location"].([]any)[0].(string)
			}
		}
	}
	if len(redirected) != 2 || redirected["ready.example.com"] != "https://{http.request.host}{http.request.uri}" || redirected["naru.example.com"] == "" {
		t.Fatalf("only the admin address and the site with a certificate are redirected: %v", redirected)
	}
	// 넘기는 주소도 HTTP 경로가 뒤에 남는다 — 인증서 확인 요청이 거기로 간다
	served := 0
	for _, r := range d.Apps.HTTP.Servers["http"].Routes {
		if len(r.Match) == 1 && r.Match[0].Host[0] == "ready.example.com" && len(r.Match[0].Not) == 0 {
			served++
		}
	}
	if served != 1 {
		t.Fatal("the plain route stays behind the redirect for challenge requests")
	}
	dev := decode(t, fixtures["redirects-dev-port"])
	var loc any
	for _, r := range dev.Apps.HTTP.Servers["http"].Routes {
		if hs, ok := r.Handle[0]["headers"].(map[string]any); ok && hs["Location"] != nil {
			loc = hs["Location"].([]any)[0]
		}
	}
	if loc != "https://{http.request.host}:8443{http.request.uri}" {
		t.Fatalf("a non-standard HTTPS port is kept: %v", loc)
	}
}

func TestHTTPSOnlyForHostsThatAskForIt(t *testing.T) {
	d := decode(t, fixtures["full"])
	https := d.Apps.HTTP.Servers["https"]
	var hosts []string
	for _, r := range https.Routes {
		for _, m := range r.Match {
			hosts = append(hosts, m.Host...)
		}
	}
	joined := strings.Join(hosts, " ")
	if strings.Contains(joined, "nas.example.com") || !strings.Contains(joined, "blog.example.com") || !strings.Contains(joined, "naru.example.com") {
		t.Fatalf("443 hosts: %v", hosts)
	}
	if d.Apps.TLS == nil || d.Apps.TLS.Automation.Policies[0].Issuers[0]["email"] != "me@example.com" {
		t.Fatal("the ACME email reaches the issuer")
	}
}

func TestNoHTTPSServerWhenNothingAsksForIt(t *testing.T) {
	d := decode(t, fixtures["setup-open"])
	if _, ok := d.Apps.HTTP.Servers["https"]; ok {
		t.Fatal("without HTTPS hosts there is no :443 server — Caddy must not go hunting for certificates")
	}
	last := d.Apps.HTTP.Servers["http"].Routes[0]
	if len(last.Match) != 0 || last.Handle[0]["handler"] != "reverse_proxy" {
		t.Fatal("before setup, unknown hosts on :80 reach the admin screen")
	}
}

func TestClosedFallbackIs404(t *testing.T) {
	p := fixtures["full"]
	d := decode(t, p)
	routes := d.Apps.HTTP.Servers["http"].Routes
	last := routes[len(routes)-1]
	if len(last.Match) != 0 || last.Handle[0]["status_code"] != float64(404) {
		t.Fatalf("after setup, unknown hosts get 404: %+v", last)
	}
}

func TestWriteRefusesBadSiteMaps(t *testing.T) {
	if _, err := (JSONWriter{}).Write(model.SiteMap{}); err == nil {
		t.Fatal("no admin socket, no config")
	}
	dup := model.SiteMap{AdminSocket: sock, AdminHosts: []string{"a.example.com"}, Sites: []model.Site{{Hosts: []string{"A.example.com"}, Destination: model.Destination{Address: "x:1"}}}}
	if _, err := (JSONWriter{}).Write(dup); err == nil {
		t.Fatal("a host used twice is refused")
	}
}

func TestCheckAdmin(t *testing.T) {
	good, _ := (JSONWriter{}).Write(fixtures["setup-open"])
	if err := checkAdmin(good, sock); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{}`, `{"admin":{"listen":"localhost:2019"}}`, `not json`} {
		if checkAdmin([]byte(bad), sock) == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
}

// fakeCaddy는 유닉스 소켓에서 Caddy 관리 API인 척하고 받은 것을 기록한다.
type fakeCaddy struct {
	mu    sync.Mutex
	loads [][]byte
	pings int
	fail  bool
	hosts []string
}

func (f *fakeCaddy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hosts = append(f.hosts, r.Host)
	if f.fail {
		http.Error(w, `{"error":"loading config: bad"}`, http.StatusBadRequest)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/load":
		b, _ := io.ReadAll(r.Body)
		f.loads = append(f.loads, b)
	case r.Method == http.MethodGet && r.URL.Path == "/config/admin":
		f.pings++
		w.Write([]byte(`{"listen":"unix//run/caddy/admin.sock"}`))
	default:
		http.NotFound(w, r)
	}
}

func startFake(t *testing.T) (*fakeCaddy, string) {
	t.Helper()
	dir, _ := os.MkdirTemp("", "cd")
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeCaddy{}
	srv := &http.Server{Handler: f}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return f, path
}

func TestSenderTalksToTheSocketAsLocalhost(t *testing.T) {
	f, path := startFake(t)
	m := model.SiteMap{AdminSocket: path, AdminUpstream: "naru:8080"}
	cfg, _ := (JSONWriter{}).Write(m)
	s := NewAdminSocketGuard(NewSocketSender(path), path)
	if err := s.Send(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.loads) != 1 || f.pings != 1 {
		t.Fatalf("loads=%d pings=%d", len(f.loads), f.pings)
	}
	for _, h := range f.hosts {
		if h != "127.0.0.1" {
			t.Fatalf("Caddy's socket API only accepts Host 127.0.0.1, sent %q", h)
		}
	}
	if err := s.Send(context.Background(), []byte(`{"admin":{"listen":"localhost:2019"}}`)); err == nil || len(f.loads) != 1 {
		t.Fatal("the guard never lets a config without the admin socket through")
	}
	f.fail = true
	if err := s.Send(context.Background(), cfg); err == nil || err.Error() != "caddy: bad" {
		t.Fatalf("a refused config is an error: %v", err)
	}
}
