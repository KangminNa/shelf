// Package watch는 서비스·웹서버·Docker를 지켜본다 (v2-objects §4 J).
// 정해진 간격마다 보고, 바뀔 때만 이벤트를 낸다 — 보내는 일은 알리기(notify)가 듣고 한다.
//
// 알림 폭탄을 막는 규칙:
//   - 두 번 연속(약 1분) 문제일 때만 멈춤으로 본다 — 배포·재시작은 잠깐 깜빡인다.
//   - 직접 멈춘 서비스와 배포 중인 서비스는 보지 않는다.
//   - 처음 보았을 때 이미 문제였던 것은 알리지 않고 기준으로만 삼는다 (Naru를 다시 켤 때마다 쏟아지지 않게).
//   - 인증서 곧 끝남은 주소마다 하루 한 번.
package watch

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// Parts는 지켜보기가 쓰는 것들이다.
type Parts struct {
	Services   contract.ServiceReader
	Kinds      contract.KindLookup
	Containers contract.ContainerWatcher
	Ports      contract.PortChecker
	Deployer   contract.Deployer
	WebServer  contract.WebServerSync
	Certs      contract.CertificateReader
	Admin      contract.AdminDomainSetting
	Clock      contract.Clock
	Events     contract.EventPublisher
	Every      time.Duration // 0이면 30초
	Log        *slog.Logger
}

type healthWatcher struct {
	p Parts

	mu   sync.RWMutex
	snap model.WatchSnapshot

	checked    bool
	services   map[model.ServiceID]*flag
	docker     flag
	webServer  flag
	certWarned map[string]time.Time
}

// flag는 하나의 "괜찮은가"를 지켜본다 — 두 번 연속 나빠야 멈춤, 한 번 좋아지면 복구.
type flag struct {
	bad        int
	down       bool
	badAtStart bool // 처음 보았을 때부터 나빴다 — 멈춤이 되어도 알리지 않는다
	since      time.Time
	why        string
}

const (
	same = iota
	wentDown
	cameUp
)

func (f *flag) observe(ok, first bool, now time.Time, why string) int {
	if ok {
		f.bad, f.badAtStart, f.why = 0, false, ""
		if f.down {
			f.down = false
			return cameUp
		}
		return same
	}
	f.bad++
	f.why = why
	if first {
		f.badAtStart = true
	}
	if f.bad >= 2 && !f.down {
		f.down, f.since = true, now
		if f.badAtStart {
			return same
		}
		return wentDown
	}
	return same
}

// NewHealthWatcher는 지켜보기를 만든다. run은 ctx가 끝날 때까지 정해진 간격으로 본다.
func NewHealthWatcher(p Parts) (contract.HealthWatcher, func(context.Context)) {
	if p.Every == 0 {
		p.Every = 30 * time.Second
	}
	w := &healthWatcher{p: p, services: map[model.ServiceID]*flag{}, certWarned: map[string]time.Time{}}
	w.snap = model.WatchSnapshot{DockerUp: true, WebServerUp: true}
	return w, w.run
}

func (w *healthWatcher) Snapshot() model.WatchSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snap
}

func (w *healthWatcher) run(ctx context.Context) {
	// 처음에는 조금 기다린다 — 함께 뜬 웹서버가 맞춰질 시간을 준다
	first := w.p.Every / 6
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.check(ctx)
			t.Reset(w.p.Every)
		}
	}
}

func (w *healthWatcher) check(ctx context.Context) {
	now := w.p.Clock.Now()
	first := !w.checked
	w.checked = true

	ws := w.p.WebServer.Status()
	switch w.webServer.observe(ws.Connected, first, now, ws.LastError) {
	case wentDown:
		w.p.Events.Publish(model.WebServerDown{Why: ws.LastError})
	case cameUp:
		w.p.Events.Publish(model.WebServerUp{})
	}

	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	states, derr := w.p.Containers.All(dctx)
	cancel()
	why := ""
	if derr != nil {
		why = derr.Error()
	}
	switch w.docker.observe(derr == nil, first, now, why) {
	case wentDown:
		w.p.Events.Publish(model.DockerDown{Why: why})
	case cameUp:
		w.p.Events.Publish(model.DockerUp{})
	}

	all, err := w.p.Services.List(ctx)
	if err != nil {
		w.p.Log.Warn("watch: list services", "err", err)
		return
	}
	if derr == nil { // Docker를 못 읽으면 서비스를 판정하지 않는다 — 모르는 것을 멈춤으로 보지 않는다
		w.judgeServices(ctx, all, states, now)
	}
	w.warnCertificates(ctx, all, now)
	w.publishSnapshot(all, now)
}

func (w *healthWatcher) judgeServices(ctx context.Context, all []model.Service, states model.ContainerStates, now time.Time) {
	seen := map[model.ServiceID]bool{}
	for _, s := range all {
		tools, ok := w.p.Kinds.Find(s.Kind)
		if !ok || !tools.Destination.Find(s).Container {
			continue // 지켜보는 것은 컨테이너 서비스뿐이다
		}
		if s.Live.Stopped || s.Live.CurrentContainer() == "" || w.p.Deployer.IsDeploying(s.ID) {
			continue // 직접 멈춤·아직 배포 전·배포 중 — 판정하지 않고, 기록도 지운다
		}
		seen[s.ID] = true
		f := w.services[s.ID]
		newcomer := f == nil
		if newcomer {
			f = &flag{}
			w.services[s.ID] = f
		}
		why := w.problem(ctx, s, states)
		switch f.observe(why == "", newcomer, now, why) {
		case wentDown:
			w.p.Events.Publish(model.ServiceDown{Service: s.ID, Name: s.Name.String(), Why: why})
		case cameUp:
			w.p.Events.Publish(model.ServiceUp{Service: s.ID, Name: s.Name.String()})
		}
	}
	for id := range w.services {
		if !seen[id] {
			delete(w.services, id)
		}
	}
}

// problem은 서비스가 지금 괜찮지 않은 이유다. 괜찮으면 빈 값.
func (w *healthWatcher) problem(ctx context.Context, s model.Service, states model.ContainerStates) string {
	st, ok := states[s.Live.CurrentContainer()]
	if !ok {
		return "missing"
	}
	switch st.State {
	case "restarting":
		return "restarting"
	case "exited", "dead":
		return "crashed"
	case "running":
	default:
		return "" // 만들어짐·일시정지 — 멈춤으로 보지 않는다
	}
	if s.Port == 0 {
		return ""
	}
	host := st.IP
	if host == "" {
		host = s.Live.InstanceIP
	}
	if host == "" {
		host = s.Live.CurrentContainer()
	}
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if w.p.Ports.Answers(pctx, host, s.Port) != nil {
		return "noanswer"
	}
	return ""
}

func (w *healthWatcher) warnCertificates(ctx context.Context, all []model.Service, now time.Time) {
	certs, err := w.p.Certs.Read(ctx)
	if err != nil || len(certs) == 0 {
		return
	}
	var domains []model.DomainName
	if d, _ := w.p.Admin.Get(ctx); !d.IsZero() {
		domains = append(domains, d)
	}
	for _, s := range all {
		for _, d := range s.Domains {
			if d.HTTPS {
				domains = append(domains, d.Domain)
			}
		}
	}
	for _, d := range domains {
		st := certs.For(d, now)
		if !st.EndsSoon(now) {
			continue
		}
		if last, ok := w.certWarned[d.String()]; ok && now.Sub(last) < 24*time.Hour {
			continue
		}
		w.certWarned[d.String()] = now
		w.p.Events.Publish(model.CertificateEndingSoon{Domain: d.String(), NotAfter: st.NotAfter})
	}
}

func (w *healthWatcher) publishSnapshot(all []model.Service, now time.Time) {
	snap := model.WatchSnapshot{CheckedAt: now, DockerUp: !w.docker.down, WebServerUp: !w.webServer.down}
	for _, s := range all {
		if f := w.services[s.ID]; f != nil && f.down {
			snap.Down = append(snap.Down, model.DownService{Service: s.ID, Name: s.Name.String(), Why: f.why, Since: f.since})
		}
	}
	sort.Slice(snap.Down, func(i, j int) bool { return snap.Down[i].Name < snap.Down[j].Name })
	w.mu.Lock()
	w.snap = snap
	w.mu.Unlock()
}
