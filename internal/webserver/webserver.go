// Package webserver는 웹서버를 Naru의 상태에 맞춘다 (Reconciler).
// 원본은 Naru의 저장소 하나다. 바뀔 때마다 사이트 지도를 통째로 다시 그려 보낸다 — 부분 수정은 하지 않는다.
// 바뀌었다는 알림(SiteMapChanged)을 들을 때 바로, 그리고 30초마다 다시 확인한다 —
// 웹서버가 재시작돼도, Naru가 재시작돼도 맞춰진다.
package webserver

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"sync"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// ── 사이트 지도 ────────────────────────────

// Fixed는 실행 내내 바뀌지 않는 웹서버 값이다 (환경 변수에서 온다).
type Fixed struct {
	AdminSocket   string // 웹서버 관리 소켓 — 그린 설정에 항상 들어간다
	AdminUpstream string // 웹서버가 관리 화면에 닿는 주소
	InternalTLS   bool   // 개발용 내부 인증서
	AccessLog     string // 접근 로그 파일 (웹서버가 본 경로) — 비면 남기지 않는다
	Storage       string // 웹서버가 인증서를 둘 곳 — 비면 웹서버 기본값 (설치형은 데이터 폴더 안으로 정해 Naru가 읽는다)
	HTTPSPort     int    // 바깥에서 본 HTTPS 포트 (0이면 443) — 넘기는 주소에 쓴다
}

// Settings는 사이트 지도가 읽는 서버 설정이다.
type Settings struct {
	Admin contract.AdminDomainSetting
	Email contract.CertEmailSetting
	Setup contract.SetupProgress
}

type siteMapBuilder struct {
	fixed    Fixed
	services contract.ServiceReader
	kinds    contract.KindLookup
	settings Settings
	certs    contract.CertificateReader
	clock    contract.Clock
	web      contract.WebSettingsStore
	watcher  contract.ContainerWatcher
}

func NewSiteMapBuilder(fixed Fixed, services contract.ServiceReader, kinds contract.KindLookup, settings Settings,
	certs contract.CertificateReader, clock contract.Clock, web contract.WebSettingsStore, watcher contract.ContainerWatcher) contract.SiteMapBuilder {
	return siteMapBuilder{fixed, services, kinds, settings, certs, clock, web, watcher}
}

func (b siteMapBuilder) Build(ctx context.Context) (model.SiteMap, error) {
	all, err := b.services.List(ctx)
	if err != nil {
		return model.SiteMap{}, err
	}
	domain, _ := b.settings.Admin.Get(ctx)
	email, _ := b.settings.Email.Get(ctx)
	// 인증서를 읽지 못하면 "하나도 없음"으로 본다 — 넘기기가 꺼질 뿐 HTTP로는 열린다 (안전한 쪽으로 무너진다).
	certs, _ := b.certs.Read(ctx)
	now := b.clock.Now()
	ready := func(d model.DomainName) bool { return certs.For(d, now).Usable(now) }

	m := model.SiteMap{
		AdminSocket:   b.fixed.AdminSocket,
		AdminUpstream: b.fixed.AdminUpstream,
		// 첫 설정이 끝나기 전이나 관리 주소가 없을 때는 IP로 들어와도 관리 화면에 닿아야 한다.
		OpenFallback: !b.settings.Setup.Done(ctx) || domain.IsZero(),
		ACMEEmail:    email.String(),
		InternalTLS:  b.fixed.InternalTLS,
		HTTPSPort:    b.fixed.HTTPSPort,
		Storage:      b.fixed.Storage,
		AccessLog:    b.fixed.AccessLog,
	}
	if !domain.IsZero() {
		m.AdminHosts, m.AdminHTTPS = []string{domain.String()}, true
		m.AdminRedirectHTTP = ready(domain)
	}
	// 컨테이너 IP는 재시작하면 바뀔 수 있다 — 지금 IP로 덮어쓴다 (Docker를 못 읽으면 배포 때 기록한 IP).
	wctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	states, werr := b.watcher.All(wctx)
	cancel()
	if werr == nil {
		for i := range all {
			if st, ok := states[all[i].Live.CurrentContainer()]; ok && st.IP != "" {
				all[i].Live.InstanceIP = st.IP
			}
		}
	}
	// 경로별 연결의 목적지를 실제 주소로 바꾸려면 모든 서비스의 목적지를 먼저 알아야 한다
	destinations := map[model.ServiceID]model.Destination{}
	for _, s := range all {
		if tools, ok := b.kinds.Find(s.Kind); ok {
			destinations[s.ID] = tools.Destination.Find(s)
		}
	}
	for _, s := range all {
		to, ok := destinations[s.ID]
		if !ok {
			continue
		}
		web, err := b.web.Get(ctx, s.ID)
		if err != nil {
			return model.SiteMap{}, err
		}
		settings := siteSettings(web, destinations)
		for _, d := range s.Domains {
			if d.Domain == domain {
				continue // 관리 주소가 우선
			}
			m.Sites = append(m.Sites, model.Site{Hosts: []string{d.Domain.String()}, Destination: to, HTTPS: d.HTTPS, HSTS: d.HSTS,
				RedirectHTTP: d.HTTPS && ready(d.Domain), Settings: settings})
		}
	}
	return m, nil
}

// siteSettings는 서비스의 웹서버 설정을 사이트 지도에 싣는 모양으로 바꾼다.
// 경로의 대상 서비스가 지워졌으면 목적지가 비어 웹서버가 "연결할 곳이 없어요"(502)로 답한다.
func siteSettings(w model.WebSettings, destinations map[model.ServiceID]model.Destination) model.SiteSettings {
	out := model.SiteSettings{Headers: w.Headers, AllowFrom: w.AllowFrom, Login: w.Login, Maintenance: w.Maintenance, Compiled: w.Compiled}
	for _, p := range w.Paths {
		to := model.Destination{Address: p.External.String()}
		if p.Service != 0 {
			to = destinations[p.Service]
		}
		out.Paths = append(out.Paths, model.SitePath{Prefix: p.Prefix, Destination: to, StripPrefix: p.StripPrefix})
	}
	return out
}

// ── 맞추기 ────────────────────────────────

const (
	every  = 30 * time.Second // 바뀐 게 없어도 이 간격마다 확인한다
	retry  = 3 * time.Second  // 닿지 않을 때 — 함께 뜬 웹서버가 조금 늦게 준비되는 일이 흔하다
	reload = 5 * time.Minute  // 같은 설정이어도 이 간격마다 다시 보낸다
)

type webServerSync struct {
	builder contract.SiteMapBuilder
	writer  contract.ConfigWriter
	sender  contract.ConfigSender
	log     *slog.Logger
	kick    chan struct{}

	mu       sync.RWMutex
	status   model.WebServerStatus
	lastHash [32]byte
	lastLoad time.Time
}

// NewWebServerSync는 웹서버 맞추기를 만든다. run은 ctx가 끝날 때까지 맞추기를 돌린다.
// sender는 관리 소켓 설정이 빠진 설정을 보내지 않는 것(AdminSocketGuard)이어야 한다.
func NewWebServerSync(builder contract.SiteMapBuilder, writer contract.ConfigWriter, sender contract.ConfigSender,
	events contract.EventSubscriber, log *slog.Logger) (s contract.WebServerSync, run func(context.Context)) {
	w := &webServerSync{builder: builder, writer: writer, sender: sender, log: log, kick: make(chan struct{}, 1)}
	events.Subscribe(func(e model.Event) {
		if _, ok := e.(model.SiteMapChanged); ok {
			w.poke()
		}
	})
	return w, w.run
}

// poke는 바뀌었다고 알린다. 막히지 않는다 — 여러 번 와도 한 번 맞춘다.
func (w *webServerSync) poke() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

func (w *webServerSync) Status() model.WebServerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

func (w *webServerSync) run(ctx context.Context) {
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.kick:
		case <-t.C:
		}
		next := every
		if w.SyncNow(ctx) != nil {
			next = retry
		}
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(next)
	}
}

// SyncNow는 지금의 상태를 웹서버에 반영한다. 바뀐 게 없으면 닿는지만 본다.
func (w *webServerSync) SyncNow(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	m, err := w.builder.Build(ctx)
	if err != nil {
		w.fail("build", err)
		return err
	}
	cfg, err := w.writer.Write(m)
	if err != nil {
		w.fail("write", err)
		return err
	}
	hash := sha256.Sum256(cfg)

	w.mu.RLock()
	same := hash == w.lastHash && w.status.Connected && time.Since(w.lastLoad) < reload
	w.mu.RUnlock()
	if same {
		if err := w.sender.Ping(ctx); err != nil {
			w.fail("ping", err)
			return err
		}
		return nil
	}

	if err := w.sender.Send(ctx, cfg); err != nil {
		w.fail("send", err)
		return err
	}
	w.mu.Lock()
	changed := hash != w.lastHash
	w.lastHash, w.lastLoad = hash, time.Now()
	w.status = model.WebServerStatus{Connected: true, AppliedAt: time.Now()}
	w.mu.Unlock()
	if changed {
		w.log.Info("web server config applied", "sites", len(m.Sites), "admin", m.AdminHosts, "open_fallback", m.OpenFallback)
	}
	return nil
}

func (w *webServerSync) fail(stage string, err error) {
	w.mu.Lock()
	was := w.status.Connected || w.status.LastError != err.Error()
	w.status.Connected = false
	w.status.LastError = err.Error()
	w.lastHash = [32]byte{} // 다음에 다시 보낸다
	w.mu.Unlock()
	if was {
		w.log.Warn("web server unreachable or refused config", "stage", stage, "err", err)
	}
}
