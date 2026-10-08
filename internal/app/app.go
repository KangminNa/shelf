// Package app은 Naru를 조립하고 띄운다. 무엇이 무엇에 의존하는지는 여기서만 보인다.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/KangminNa/naru/internal/auth"
	"github.com/KangminNa/naru/internal/deploy"
	"github.com/KangminNa/naru/internal/dnscheck"
	"github.com/KangminNa/naru/internal/docker"
	"github.com/KangminNa/naru/internal/engine"
	"github.com/KangminNa/naru/internal/hostinfo"
	"github.com/KangminNa/naru/internal/service"
	"github.com/KangminNa/naru/internal/store"
	"github.com/KangminNa/naru/internal/v1import"
	"github.com/KangminNa/naru/internal/web"
)

// Config는 환경 변수에서 읽는다. 모두 비워 둬도 돈다 — 나머지는 첫 설정 화면에서 정한다.
type Config struct {
	Listen       string // NARU_LISTEN, 기본 :8080
	DataDir      string // NARU_DATA_DIR, 기본 ./data
	ProcDir      string // NARU_PROC_DIR, 기본 /proc
	DockerSocket string // NARU_DOCKER_SOCKET, 기본 /var/run/docker.sock
	CaddySocket  string // NARU_CADDY_ADMIN — 웹서버(Caddy) 관리 소켓, 기본 /run/caddy/admin.sock
	SelfUpstream string // NARU_SELF_UPSTREAM — 웹서버가 관리 화면에 닿는 주소, 기본 naru:8080
	InternalTLS  bool   // NARU_TLS=internal — 개발용. 공개 CA 대신 내부 CA로 인증서
	Network      string // NARU_NETWORK — 앱 컨테이너와 웹서버가 함께 있는 네트워크, 기본 naru-net
	CaddySites   string // NARU_CADDY_SITES — 웹서버 컨테이너에서 본 정적 사이트 폴더, 기본 /srv/sites
	AdminDomain  string // ADMIN_DOMAIN — 있으면 화면 설정보다 우선
	ACMEEmail    string // ACME_EMAIL — 있으면 화면 설정보다 우선
	Version      string
}

func ConfigFromEnv(version string) Config {
	return Config{
		Listen:       envOr("NARU_LISTEN", ":8080"),
		DataDir:      envOr("NARU_DATA_DIR", "./data"),
		ProcDir:      envOr("NARU_PROC_DIR", "/proc"),
		DockerSocket: envOr("NARU_DOCKER_SOCKET", "/var/run/docker.sock"),
		CaddySocket:  envOr("NARU_CADDY_ADMIN", "/run/caddy/admin.sock"),
		SelfUpstream: envOr("NARU_SELF_UPSTREAM", "naru:8080"),
		InternalTLS:  os.Getenv("NARU_TLS") == "internal",
		Network:      envOr("NARU_NETWORK", "naru-net"),
		CaddySites:   envOr("NARU_CADDY_SITES", "/srv/sites"),
		AdminDomain:  dnscheck.Normalize(os.Getenv("ADMIN_DOMAIN")),
		ACMEEmail:    os.Getenv("ACME_EMAIL"),
		Version:      version,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type App struct {
	cfg      Config
	log      *slog.Logger
	Store    *store.Store
	Auth     *auth.Service
	Services *service.Repo
	Engine   *engine.Engine
}

// Open은 데이터를 열고 v1 데이터를 옮긴다. 화면은 아직 띄우지 않는다 (복구 명령도 이걸 쓴다).
func Open(cfg Config, log *slog.Logger) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "naru.db"))
	if err != nil {
		return nil, err
	}
	accounts, err := auth.New(st.DB)
	if err != nil {
		st.Close()
		return nil, err
	}
	a := &App{cfg: cfg, log: log, Store: st, Auth: accounts, Services: service.NewRepo(st.DB)}

	// v1 데이터를 못 읽어도 v2는 뜬다 — 다음 실행에서 다시 시도한다.
	if r, err := v1import.Run(cfg.DataDir, st, accounts); err != nil {
		log.Error("v1 import (accounts) failed — will retry next start", "err", err)
	} else if r.Found {
		log.Info("imported from v1", "accounts", r.Users, "admin_domain", r.AdminDomain)
	}
	if r, err := v1import.RunServices(cfg.DataDir, st, a.Services); err != nil {
		log.Error("v1 import (services) failed — will retry next start", "err", err)
	} else if !r.Skipped && (r.Apps+r.External) > 0 {
		log.Info("imported v1 apps and proxy hosts as services", "apps", r.Apps, "external", r.External, "domains", r.Domains)
	}
	return a, nil
}

func (a *App) Close() error { return a.Store.Close() }

// Plan은 DB를 웹서버 설정 계획으로 바꾼다.
func (a *App) Plan(context.Context) (engine.Plan, error) {
	services, err := a.Services.List()
	if err != nil {
		return engine.Plan{}, err
	}
	domain := a.cfg.AdminDomain
	if domain == "" {
		domain, _ = a.Store.Setting(store.KeyAdminDomain)
	}
	email := a.cfg.ACMEEmail
	if email == "" {
		email, _ = a.Store.Setting(store.KeyACMEEmail)
	}
	_, setupDone := a.Store.Setting(store.KeySetupDone)

	p := engine.Plan{
		AdminSocket:   a.cfg.CaddySocket,
		AdminUpstream: a.cfg.SelfUpstream,
		// 첫 설정이 끝나기 전이나 관리 주소가 없을 때는 IP로 들어와도 관리 화면에 닿아야 한다.
		OpenFallback: !setupDone || domain == "",
		ACMEEmail:    email,
		InternalTLS:  a.cfg.InternalTLS,
	}
	if domain != "" {
		p.AdminHosts, p.AdminHTTPS = []string{domain}, true
	}
	for _, s := range services {
		for _, d := range s.Domains {
			if d.Domain == domain {
				continue // 관리 주소가 우선
			}
			site := engine.Site{Hosts: []string{d.Domain}, Upstream: s.Target(), HTTPS: d.HTTPS, HSTS: d.HSTS}
			if s.Kind == service.KindStatic && s.Release != "" {
				site.Root = path.Join(a.cfg.CaddySites, s.Name, s.Release)
			}
			p.Sites = append(p.Sites, site)
		}
	}
	return p, nil
}

// Run은 ctx가 끝날 때까지 화면과 엔진 맞추기를 띄운다.
func (a *App) Run(ctx context.Context) error {
	host := hostinfo.NewSampler(a.cfg.ProcDir, a.cfg.DataDir)
	go host.Run(ctx, 5*time.Second)

	a.Engine = engine.New(engine.NewAdmin(a.cfg.CaddySocket), a.Plan, a.log)
	go a.Engine.Run(ctx)

	dockerClient := docker.New(a.cfg.DockerSocket)
	history := deploy.NewStore(a.Store.DB)
	if n, err := history.Interrupted("— Naru가 다시 시작되면서 중단됐어요 / interrupted by a Naru restart —"); err == nil && n > 0 {
		a.log.Warn("closed deploys interrupted by a restart", "count", n)
	}
	deployer := deploy.New(deploy.Config{
		WorkDir:  filepath.Join(a.cfg.DataDir, "work"),
		SitesDir: filepath.Join(a.cfg.DataDir, "sites"),
		Network:  a.cfg.Network,
	}, a.Services, history, dockerClient, a.Engine.Kick, a.log)
	deployer.Bind(ctx)
	os.RemoveAll(filepath.Join(a.cfg.DataDir, "work")) // 지난 실행이 남긴 clone

	srv, err := web.New(web.Deps{
		Store: a.Store, Auth: a.Auth, Services: a.Services,
		Containers: dockerClient, Engine: a.Engine, Deployer: deployer, Deployments: history,
		Host: host, Lookup: dnscheck.System, Log: a.log,
		DataDir: a.cfg.DataDir, Version: a.cfg.Version,
		EnvAdminDomain: a.cfg.AdminDomain, EnvACMEEmail: a.cfg.ACMEEmail,
	})
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              a.cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	errc := make(chan error, 1)
	go func() { errc <- httpServer.ListenAndServe() }()

	a.log.Info("naru started", "version", a.cfg.Version, "listen", a.cfg.Listen, "data", a.cfg.DataDir, "web_server", a.cfg.CaddySocket)
	if a.Auth.NeedsSetup() {
		a.printSetupLink()
	}

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdown)
	}
}

// printSetupLink는 첫 설정 주소를 로그에 남긴다. 이 주소를 아는 사람만 관리자 계정을 만들 수 있다.
func (a *App) printSetupLink() {
	path := "/setup?token=" + a.Auth.SetupToken()
	fmt.Fprintf(os.Stderr, "\n  처음 설정 / First-time setup\n  → http://<이 서버 주소 / this server>%s\n\n", path)
	a.log.Info("waiting for first-time setup", "setup", path)
}
