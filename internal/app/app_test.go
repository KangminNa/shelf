package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/KangminNa/naru/internal/engine"
	"github.com/KangminNa/naru/internal/service"
	"github.com/KangminNa/naru/internal/store"
)

func open(t *testing.T, cfg Config) *App {
	t.Helper()
	cfg.DataDir = t.TempDir()
	if cfg.CaddySocket == "" {
		cfg.CaddySocket = "/run/caddy/admin.sock"
	}
	if cfg.SelfUpstream == "" {
		cfg.SelfUpstream = "naru:8080"
	}
	a, err := Open(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func TestPlanBeforeSetupIsOpen(t *testing.T) {
	a := open(t, Config{})
	p, err := a.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !p.OpenFallback || len(p.AdminHosts) != 0 || p.AdminSocket != "/run/caddy/admin.sock" {
		t.Fatalf("%+v", p)
	}
	if _, err := engine.Render(p); err != nil {
		t.Fatal("a fresh install renders a valid config:", err)
	}
}

func TestPlanAfterSetup(t *testing.T) {
	a := open(t, Config{ACMEEmail: "env@example.com"})
	a.Store.SetSetting(store.KeySetupDone, "1")
	a.Store.SetSetting(store.KeyAdminDomain, "naru.example.com")
	a.Store.SetSetting(store.KeyACMEEmail, "db@example.com")
	blog, _ := a.Services.Create(service.Service{Name: "blog", Kind: service.KindRepo, Container: "shelf-blog", Port: 3000}, service.Secrets{})
	a.Services.AddDomain(blog, service.Domain{Domain: "blog.example.com", HTTPS: true, HSTS: true})
	a.Services.AddDomain(blog, service.Domain{Domain: "old.example.com"})
	nas, _ := a.Services.Create(service.Service{Name: "nas", Kind: service.KindExternal, Upstream: "host.docker.internal:5000"}, service.Secrets{})
	// 관리 주소와 같은 도메인이 서비스에 남아 있어도 관리 화면이 이긴다
	a.Services.AddDomain(nas, service.Domain{Domain: "naru.example.com"})

	p, err := a.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.OpenFallback {
		t.Fatal("with setup done and an admin address, unknown hosts get 404")
	}
	if len(p.AdminHosts) != 1 || p.AdminHosts[0] != "naru.example.com" || !p.AdminHTTPS {
		t.Fatalf("%+v", p.AdminHosts)
	}
	if p.ACMEEmail != "env@example.com" {
		t.Fatal("the environment wins over the screen")
	}
	if len(p.Sites) != 2 {
		t.Fatalf("one site per domain, admin address excluded: %+v", p.Sites)
	}
	if _, err := engine.Render(p); err != nil {
		t.Fatal(err)
	}
}

func TestPlanWithoutAdminAddressStaysReachableByIP(t *testing.T) {
	a := open(t, Config{})
	a.Store.SetSetting(store.KeySetupDone, "1")
	p, _ := a.Plan(context.Background())
	if !p.OpenFallback {
		t.Fatal("without an admin address the only way in is the IP — keep it open")
	}
}
