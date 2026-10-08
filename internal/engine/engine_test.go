package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

const sock = "/run/caddy/admin.sock"

// 픽스처는 testdata/에 골든 파일로 남는다. scripts/caddy-validate.sh가 실제 Caddy로 검증한다.
var fixtures = map[string]Plan{
	"setup-open": {AdminSocket: sock, AdminUpstream: "naru:8080", OpenFallback: true},
	"full": {
		AdminSocket: sock, AdminUpstream: "naru:8080", AdminHosts: []string{"naru.example.com"}, AdminHTTPS: true,
		ACMEEmail: "me@example.com",
		Sites: []Site{
			{Hosts: []string{"www.example.com"}, Upstream: "shelf-landing:4023", HTTPS: true, HSTS: true},
			{Hosts: []string{"blog.example.com"}, Upstream: "shelf-blog:3000", HTTPS: true},
			{Hosts: []string{"nas.example.com"}, Upstream: "host.docker.internal:5000"},
			{Hosts: []string{"router.example.com"}, Upstream: "https://192.168.0.1:443", HTTPS: true},
			{Hosts: []string{"broken.example.com"}, Upstream: ""},
			{Hosts: []string{"static.example.com"}, Root: "/srv/sites/landing/12", HTTPS: true},
		},
	},
	"internal-tls": {
		AdminSocket: sock, AdminUpstream: "naru:8080", AdminHosts: []string{"naru.localhost"}, AdminHTTPS: true, InternalTLS: true,
		Sites: []Site{{Hosts: []string{"app.localhost"}, Upstream: "naru-app:3000", HTTPS: true}},
	},
}

func TestGolden(t *testing.T) {
	for name, p := range fixtures {
		got, err := Render(p)
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
			t.Fatalf("%s: run `go test ./internal/engine -update` to create it: %v", name, err)
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
					Match  []struct{ Host []string }
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

func decode(t *testing.T, p Plan) decoded {
	t.Helper()
	b, err := Render(p)
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
					if h["handler"] == "static_response" {
						if hs, ok := h["headers"].(map[string]any); ok && hs["Location"] != nil {
							t.Errorf("%s/%s: no HTTP→HTTPS redirect until certificates are confirmed (invariant 4)", name, sname)
						}
					}
				}
			}
		}
		raw, _ := Render(p)
		for _, secret := range []string{"whsec", "ghp_", "password"} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("%s: secrets never go into the Caddy config (invariant 5)", name)
			}
		}
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

func TestRenderRefusesBadPlans(t *testing.T) {
	if _, err := Render(Plan{}); err == nil {
		t.Fatal("no admin socket, no config")
	}
	dup := Plan{AdminSocket: sock, AdminHosts: []string{"a.example.com"}, Sites: []Site{{Hosts: []string{"A.example.com"}, Upstream: "x:1"}}}
	if _, err := Render(dup); err == nil {
		t.Fatal("a host used twice is refused")
	}
}

func TestCheckAdmin(t *testing.T) {
	good, _ := Render(fixtures["setup-open"])
	if err := CheckAdmin(good, sock); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{}`, `{"admin":{"listen":"localhost:2019"}}`, `not json`} {
		if CheckAdmin([]byte(bad), sock) == nil {
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

func TestSyncLoadsOnceAndThenOnlyChecks(t *testing.T) {
	f, path := startFake(t)
	sites := []Site{{Hosts: []string{"blog.example.com"}, Upstream: "shelf-blog:3000"}}
	plan := func(context.Context) (Plan, error) {
		return Plan{AdminSocket: path, AdminUpstream: "naru:8080", Sites: sites}, nil
	}
	e := New(NewAdmin(path), plan, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	if err := e.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	e.Sync(ctx)
	if len(f.loads) != 1 || f.pings != 1 {
		t.Fatalf("an unchanged config is not sent again: loads=%d pings=%d", len(f.loads), f.pings)
	}
	if err := CheckAdmin(f.loads[0], path); err != nil {
		t.Fatal("what reached Caddy keeps the admin socket:", err)
	}
	for _, h := range f.hosts {
		if h != "127.0.0.1" {
			t.Fatalf("Caddy's socket API only accepts Host 127.0.0.1, sent %q", h)
		}
	}

	sites = append(sites, Site{Hosts: []string{"new.example.com"}, Upstream: "naru-new:80"})
	e.Sync(ctx)
	if len(f.loads) != 2 || !strings.Contains(string(f.loads[1]), "new.example.com") {
		t.Fatal("a change is sent")
	}
	if !e.Status().Connected {
		t.Fatal("status is connected")
	}
}

func TestSyncReportsRefusalAndRetries(t *testing.T) {
	f, path := startFake(t)
	plan := func(context.Context) (Plan, error) { return Plan{AdminSocket: path, AdminUpstream: "naru:8080"}, nil }
	e := New(NewAdmin(path), plan, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.fail = true
	if err := e.Sync(context.Background()); err == nil {
		t.Fatal("a refused config is an error")
	}
	if s := e.Status(); s.Connected || !strings.Contains(s.LastError, "loading config") {
		t.Fatalf("%+v", s)
	}
	f.fail = false
	if err := e.Sync(context.Background()); err != nil || len(f.loads) != 1 {
		t.Fatal("after a failure the next sync sends again")
	}
}

func TestSyncWithoutCaddy(t *testing.T) {
	plan := func(context.Context) (Plan, error) { return Plan{AdminSocket: "/nope/admin.sock"}, nil }
	e := New(NewAdmin("/nope/admin.sock"), plan, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := e.Sync(context.Background()); err == nil || e.Status().Connected {
		t.Fatal("no Caddy, not connected")
	}
	broken := func(context.Context) (Plan, error) { return Plan{}, errors.New("db gone") }
	e = New(NewAdmin("/nope"), broken, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e.Sync(context.Background()) == nil || !strings.Contains(e.Status().LastError, "db gone") {
		t.Fatal("plan errors surface in the status")
	}
}
