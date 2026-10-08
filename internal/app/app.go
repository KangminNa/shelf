// Package app은 Naru를 조립하고 띄운다. 무엇이 무엇에 의존하는지는 여기서만 보인다.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/KangminNa/naru/internal/auth"
	"github.com/KangminNa/naru/internal/dnscheck"
	"github.com/KangminNa/naru/internal/hostinfo"
	"github.com/KangminNa/naru/internal/store"
	"github.com/KangminNa/naru/internal/v1import"
	"github.com/KangminNa/naru/internal/web"
)

// Config는 환경 변수에서 읽는다. 모두 비워 둬도 돈다 — 나머지는 첫 설정 화면에서 정한다.
type Config struct {
	Listen      string // NARU_LISTEN, 기본 :8080
	DataDir     string // NARU_DATA_DIR, 기본 ./data
	ProcDir     string // NARU_PROC_DIR, 기본 /proc (컨테이너에서 호스트의 /proc를 붙일 때)
	AdminDomain string // ADMIN_DOMAIN — 있으면 화면 설정보다 우선
	ACMEEmail   string // ACME_EMAIL — 있으면 화면 설정보다 우선
	Version     string
}

func ConfigFromEnv(version string) Config {
	return Config{
		Listen:      envOr("NARU_LISTEN", ":8080"),
		DataDir:     envOr("NARU_DATA_DIR", "./data"),
		ProcDir:     envOr("NARU_PROC_DIR", "/proc"),
		AdminDomain: dnscheck.Normalize(os.Getenv("ADMIN_DOMAIN")),
		ACMEEmail:   os.Getenv("ACME_EMAIL"),
		Version:     version,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type App struct {
	cfg   Config
	log   *slog.Logger
	Store *store.Store
	Auth  *auth.Service
	host  *hostinfo.Sampler
	web   *web.Server
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
	a := &App{cfg: cfg, log: log, Store: st, Auth: accounts}

	report, err := v1import.Run(cfg.DataDir, st, accounts)
	if err != nil {
		// v1 데이터를 못 읽어도 v2는 뜬다 — 다음 실행에서 다시 시도한다.
		log.Error("v1 import failed — will retry next start", "err", err)
	} else if report.Found {
		log.Info("imported from v1", "accounts", report.Users, "admin_domain", report.AdminDomain)
	}
	return a, nil
}

func (a *App) Close() error { return a.Store.Close() }

// Run은 ctx가 끝날 때까지 화면을 띄운다.
func (a *App) Run(ctx context.Context) error {
	a.host = hostinfo.NewSampler(a.cfg.ProcDir, a.cfg.DataDir)
	go a.host.Run(ctx, 5*time.Second)

	srv, err := web.New(web.Deps{
		Store: a.Store, Auth: a.Auth, Host: a.host, Lookup: dnscheck.System, Log: a.log,
		DataDir: a.cfg.DataDir, Version: a.cfg.Version,
		EnvAdminDomain: a.cfg.AdminDomain, EnvACMEEmail: a.cfg.ACMEEmail,
	})
	if err != nil {
		return err
	}
	a.web = srv

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

	a.log.Info("naru started", "version", a.cfg.Version, "listen", a.cfg.Listen, "data", a.cfg.DataDir)
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
