// Package app은 Naru를 조립하고 띄운다 (Composition Root).
// 무엇이 무엇을 쓰는지는 여기서만 보인다 — docs/design/v2-objects.md §7 표가 그대로 이 코드다.
// 다른 패키지는 서로의 구체 타입을 모르고, 여기서 인터페이스로 이어진다.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/KangminNa/naru/internal/access"
	"github.com/KangminNa/naru/internal/caddy"
	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/deploy"
	"github.com/KangminNa/naru/internal/docker"
	"github.com/KangminNa/naru/internal/events"
	"github.com/KangminNa/naru/internal/files"
	"github.com/KangminNa/naru/internal/git"
	"github.com/KangminNa/naru/internal/kinds"
	"github.com/KangminNa/naru/internal/model"
	"github.com/KangminNa/naru/internal/netcheck"
	"github.com/KangminNa/naru/internal/services"
	"github.com/KangminNa/naru/internal/settings"
	"github.com/KangminNa/naru/internal/stats"
	"github.com/KangminNa/naru/internal/store"
	"github.com/KangminNa/naru/internal/system"
	"github.com/KangminNa/naru/internal/views"
	"github.com/KangminNa/naru/internal/web"
	"github.com/KangminNa/naru/internal/webhook"
	"github.com/KangminNa/naru/internal/webserver"
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
	CaddyCerts   string // NARU_CADDY_CERTS — 웹서버가 받은 인증서 (Naru가 읽기만), 기본 <데이터>/caddy/data/caddy/certificates
	HTTPSPort    int    // NARU_HTTPS_PORT — 바깥에서 본 HTTPS 포트, 기본 443 (로컬처럼 다른 포트로 열었을 때만)
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
		CaddyCerts:   os.Getenv("NARU_CADDY_CERTS"),
		HTTPSPort:    envPort("NARU_HTTPS_PORT", 443),
		AdminDomain:  os.Getenv("ADMIN_DOMAIN"),
		ACMEEmail:    os.Getenv("ACME_EMAIL"),
		Version:      version,
	}
}

func envPort(key string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil && n > 0 && n < 65536 {
		return n
	}
	return fallback
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Outside는 바깥 세계(Docker·git·네트워크·웹서버·/proc)에 닿는 도구다. 테스트는 가짜로 바꿔 끼운다.
type Outside struct {
	Builder contract.ImageBuilder
	Puller  contract.ImagePuller
	Images  contract.ImageCleaner
	Starter contract.ContainerStarter
	Remover contract.ContainerRemover
	Switch  contract.ContainerSwitch
	Watcher contract.ContainerWatcher
	Code    contract.CodeDownloader
	Ports   contract.PortChecker
	DNS     contract.DNSChecker
	Stats   contract.ServerStats
	Sender  contract.ConfigSender // 관리 소켓 확인(AdminSocketGuard)은 app이 늘 감싼다
	Certs   contract.CertificateReader
	Clock   contract.Clock
	Random  contract.RandomTokens

	ReadyTimeout time.Duration // 새 컨테이너 응답을 기다리는 시간 (0이면 60초)
}

func certsDir(cfg Config) string {
	if cfg.CaddyCerts != "" {
		return cfg.CaddyCerts
	}
	return filepath.Join(cfg.DataDir, "caddy", "data", "caddy", "certificates")
}

// RealOutside는 진짜 Docker·git·DNS·Caddy 소켓이다.
func RealOutside(cfg Config) Outside {
	d := docker.New(cfg.DockerSocket)
	containers := docker.NewContainers(d)
	return Outside{
		Builder: docker.NewBuilder(d), Puller: docker.NewPuller(d), Images: docker.NewImages(d),
		Starter: containers, Remover: containers, Switch: containers, Watcher: containers,
		Code: git.Downloader{}, Ports: netcheck.TCP{}, DNS: netcheck.NewDNS(nil),
		Stats:  stats.NewProcSampler(cfg.ProcDir, cfg.DataDir),
		Sender: caddy.NewSocketSender(cfg.CaddySocket),
		Certs:  caddy.NewCertificateFiles(certsDir(cfg)),
		Clock:  system.Clock{}, Random: system.Random{},
	}
}

// App은 조립된 Naru다.
type App struct {
	cfg Config
	log *slog.Logger
	db  *store.DB
	out Outside

	ctx    context.Context // 배포가 따르는 수명 — Close하면 끝난다
	stop   context.CancelFunc
	wait   func() // 진행 중인 배포를 기다린다
	syncWS func(context.Context)
	work   files.TempFolders
	past   contract.DeployHistoryStore

	Accounts contract.AccountManager // 셸 명령(cli)이 쓴다
	key      contract.SetupKey
	handler  http.Handler
}

// Open은 데이터를 열고, v1 데이터를 옮기고, 객체들을 잇는다. 화면은 아직 띄우지 않는다 (복구 명령도 이걸 쓴다).
func Open(cfg Config, log *slog.Logger) (*App, error) { return OpenWith(cfg, log, RealOutside(cfg)) }

// OpenWith는 바깥 도구를 골라 조립한다 (테스트용).
func OpenWith(cfg Config, log *slog.Logger, out Outside) (*App, error) {
	if out.Certs == nil { // 테스트처럼 가짜를 주지 않으면 데이터 폴더의 Caddy 저장소를 읽는다
		out.Certs = caddy.NewCertificateFiles(certsDir(cfg))
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	db, err := store.Open(filepath.Join(cfg.DataDir, "naru.db"))
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, log: log, db: db, out: out}
	a.ctx, a.stop = context.WithCancel(context.Background())

	// v1 데이터를 못 읽어도 v2는 뜬다 — 다음 실행에서 다시 시도한다.
	if r, err := store.ImportV1(a.ctx, db, cfg.DataDir); err != nil {
		log.Error("v1 import failed — will retry next start", "err", err)
	} else if r.Found {
		log.Info("imported from v1", "accounts", r.Users, "admin_domain", r.AdminDomain,
			"apps", r.Apps, "external", r.External, "domains", r.Domains, "history", r.History)
	}
	if err := a.assemble(); err != nil {
		db.Close()
		return nil, err
	}
	return a, nil
}

// assemble은 §7 표대로 객체를 잇는다.
func (a *App) assemble() error {
	cfg, out, db := a.cfg, a.out, a.db
	bus := events.NewBus()

	// 저장
	settingStore := store.NewSettings(db)
	accounts, sessions := store.NewAccounts(db), store.NewSessions(db)
	serviceStore, domains, secrets := store.NewServices(db), store.NewDomains(db), store.NewSecrets(db)
	liveStates, hookLogs, history := store.NewLiveStates(db), store.NewHookLogs(db), store.NewDeployments(db)

	// B. 서버 설정 — 환경 변수가 이긴다
	envDomain, err := model.ParseOptionalDomainName(cfg.AdminDomain)
	if err != nil {
		return fmt.Errorf("ADMIN_DOMAIN: %w", err)
	}
	envEmail, err := model.ParseEmail(cfg.ACMEEmail)
	if err != nil {
		return fmt.Errorf("ACME_EMAIL: %w", err)
	}
	adminDomain := settings.NewAdminDomainSetting(envDomain, settingStore, bus)
	certEmail := settings.NewCertEmailSetting(envEmail, settingStore, bus)
	setup := settings.NewSetupProgress(settingStore, bus)

	// A. 관리자로 들어오기
	hasher := access.ScryptHasher{}
	a.key = access.NewRandomSetupKey(accounts, out.Random)
	login := access.NewLoginManager(accounts, sessions, hasher, access.NewMemoryLoginLimiter(out.Clock), out.Random, out.Clock)
	a.Accounts = access.NewAccountManager(accounts, accounts, sessions, hasher, a.key, out.Random, out.Clock)

	// D. 종류
	siteFiles := files.NewSiteFolders(filepath.Join(cfg.DataDir, "sites"))
	a.work = files.NewTempFolders(filepath.Join(cfg.DataDir, "work"))
	lookup := kinds.NewLookup(kinds.Tools{
		Work: a.work, Code: out.Code, Secrets: secrets,
		Dockerfile: files.DockerfileReader{}, Packer: files.TarPacker{}, Builder: out.Builder, Puller: out.Puller,
		Images: out.Images, Starter: out.Starter, Remover: out.Remover, Switch: out.Switch, Watcher: out.Watcher,
		Ports: out.Ports, Files: siteFiles, Network: cfg.Network, SitesShown: cfg.CaddySites, ReadyTimeout: out.ReadyTimeout,
	})

	// E. 배포
	a.past = history
	lock := deploy.NewMemoryDeployLock()
	deployer, wait := deploy.NewDeployer(a.ctx, a.log, deploy.Parts{
		Lock: lock, History: history, Past: history, Logs: deploy.NewDeployLog(history), Services: serviceStore,
		Live: liveStates, Kinds: lookup, Cleaner: deploy.NewOldVersionCleaner(history, out.Images, siteFiles), Events: bus,
	})
	a.wait = wait
	control := deploy.NewServiceControl(deploy.ControlParts{
		Services: serviceStore, Store: serviceStore, Live: liveStates, Lock: lock, Switch: out.Switch, Remover: out.Remover,
		Watcher: out.Watcher, Images: out.Images, Files: siteFiles, Past: history, Events: bus,
	})

	// C. 서비스
	names := services.NewNameChooser(serviceStore)
	checker := services.NewDomainChecker(domains, adminDomain)
	editor := services.NewServiceEditor(serviceStore, serviceStore, domains, secrets, names, checker, lookup, out.Random, bus)
	launcher := services.NewServiceLauncher(editor, lookup, deployer)

	// F. 웹훅
	hooks := webhook.NewHookReceiver(serviceStore, secrets,
		[]contract.SignatureChecker{webhook.GitHubSignature{}, webhook.GitLabToken{}, webhook.QuerySecret{}},
		webhook.NewBranchFilter(), deployer, hookLogs, out.Clock)

	// G. 웹서버
	siteMap := webserver.NewSiteMapBuilder(webserver.Fixed{
		AdminSocket: cfg.CaddySocket, AdminUpstream: cfg.SelfUpstream, InternalTLS: cfg.InternalTLS, HTTPSPort: cfg.HTTPSPort,
	}, serviceStore, lookup, webserver.Settings{Admin: adminDomain, Email: certEmail, Setup: setup}, out.Certs, out.Clock)
	sync, run := webserver.NewWebServerSync(siteMap, caddy.JSONWriter{}, caddy.NewAdminSocketGuard(out.Sender, cfg.CaddySocket), bus, a.log)
	a.syncWS = run

	// H. 보여주기
	viewer := views.NewServiceViewer(views.Parts{
		Services: serviceStore, Secrets: secrets, History: history, Containers: out.Watcher, Kinds: lookup,
		Admin: adminDomain, Deployer: deployer, Certs: out.Certs, Clock: out.Clock,
	}, a.log)

	srv, err := web.New(web.Deps{
		Login: login, Accounts: a.Accounts, SetupKey: a.key, AdminDomain: adminDomain, CertEmail: certEmail, Setup: setup,
		Launcher: launcher, Editor: editor, Viewer: viewer, Deployer: deployer, Control: control, Hooks: hooks,
		Stats: out.Stats, WebServer: sync, DNS: out.DNS, Certs: out.Certs, Clock: out.Clock, Log: a.log, DataDir: cfg.DataDir, Version: cfg.Version,
	})
	if err != nil {
		return err
	}
	a.handler = srv.Handler()
	return nil
}

// Handler는 관리 화면이다 (테스트는 이것을 그대로 띄운다).
func (a *App) Handler() http.Handler { return a.handler }

// SetupKey는 이번 실행의 첫 설정 열쇠다.
func (a *App) SetupKey() string { return a.key.Value() }

// Close는 진행 중인 배포를 멈추고 기다린 뒤 DB를 닫는다.
func (a *App) Close() error {
	a.stop()
	a.wait()
	return a.db.Close()
}

// Run은 ctx가 끝날 때까지 화면과 웹서버 맞추기를 띄운다.
func (a *App) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		a.stop()
	}()
	// 지난 실행이 남긴 것을 정리한다 — 셸 명령(Open만 하는)은 하지 않는다. 도는 서버의 배포를 건드리면 안 된다.
	a.work.Clear()
	if n, err := a.past.CloseInterrupted(ctx, "— Naru가 다시 시작되면서 중단됐어요 / interrupted by a Naru restart —"); err == nil && n > 0 {
		a.log.Warn("closed deploys interrupted by a restart", "count", n)
	}
	if sampler, ok := a.out.Stats.(interface {
		Run(context.Context, time.Duration)
	}); ok {
		go sampler.Run(ctx, 5*time.Second)
	}
	go a.syncWS(ctx)

	httpServer := &http.Server{
		Addr:              a.cfg.Listen,
		Handler:           a.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	errc := make(chan error, 1)
	go func() { errc <- httpServer.ListenAndServe() }()

	a.log.Info("naru started", "version", a.cfg.Version, "listen", a.cfg.Listen, "data", a.cfg.DataDir, "web_server", a.cfg.CaddySocket)
	if names, err := a.Accounts.Names(ctx); err == nil && len(names) == 0 {
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
	path := "/setup?token=" + a.key.Value()
	fmt.Fprintf(os.Stderr, "\n  처음 설정 / First-time setup\n  → http://<이 서버 주소 / this server>%s\n\n", path)
	a.log.Info("waiting for first-time setup", "setup", path)
}
