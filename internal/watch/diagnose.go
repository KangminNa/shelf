package watch

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// DiagnoserParts는 진단이 쓰는 것들이다.
type DiagnoserParts struct {
	Kinds    contract.KindLookup
	Ports    contract.PortChecker
	DNS      contract.DNSChecker
	Admin    contract.AdminDomainSetting
	Certs    contract.CertificateReader
	Clock    contract.Clock
	PublicIP string // NARU_PUBLIC_IP — 비면 관리 주소를 조회한 IP를 이 서버의 IP로 본다
}

const (
	dnsKeep     = 10 * time.Minute // DNS 답을 이만큼 들고 있는다 — 30초마다 조회하지 않는다
	certsKeep   = time.Minute
	hookWait    = 24 * time.Hour // 만든 지 이만큼 지나도 웹훅이 오지 않으면 말한다
	portSearch  = 5 * time.Second
	portsToTry  = 8
	answerAfter = 3 * time.Second
)

// commonPorts는 이미지가 EXPOSE를 적지 않았을 때 찔러 볼 흔한 앱 포트다.
var commonPorts = []model.Port{80, 3000, 8080, 8000, 5000, 4000}

type seenDNS struct {
	a  model.DNSAnswer
	at time.Time
}

type diagnoser struct {
	p DiagnoserParts

	mu      sync.Mutex
	dns     map[string]seenDNS
	certs   model.Certificates
	certsAt time.Time
}

// NewDiagnoser는 진단을 만든다. 지켜보기 고리 하나가 차례로 부른다.
func NewDiagnoser(p DiagnoserParts) contract.Diagnoser {
	return &diagnoser{p: p, dns: map[string]seenDNS{}}
}

func (d *diagnoser) Diagnose(ctx context.Context, s model.Service, states model.ContainerStates) []model.Finding {
	tools, ok := d.p.Kinds.Find(s.Kind)
	if !ok {
		return nil
	}
	var out []model.Finding
	add := func(f model.Finding) {
		f.Service, f.Name = s.ID, s.Name.String()
		out = append(out, f)
	}
	if tools.Destination.Find(s).Container {
		if f, ok := d.wrongPort(ctx, s, states); ok {
			add(f)
		}
	}
	for _, f := range d.dnsFindings(ctx, s) {
		add(f)
	}
	created := s.Created
	if s.FromGit() && s.AutoDeploy && s.HookLog.At.IsZero() && !created.IsZero() && d.p.Clock.Now().Sub(created) > hookWait {
		add(model.Finding{Key: "find.webhook", Level: model.FindingHint})
	}
	return out
}

// wrongPort는 앱 포트는 응답하지 않는데 다른 포트가 응답하는지 본다 — 이미지가 연 포트 먼저, 그다음 흔한 포트.
func (d *diagnoser) wrongPort(ctx context.Context, s model.Service, states model.ContainerStates) (model.Finding, bool) {
	st, ok := states[s.Live.CurrentContainer()]
	if !ok || st.State != "running" || s.Port == 0 {
		return model.Finding{}, false
	}
	host := reach(s, st)
	actx, cancel := context.WithTimeout(ctx, answerAfter)
	err := d.p.Ports.Answers(actx, host, s.Port)
	cancel()
	if err == nil {
		return model.Finding{}, false
	}
	var candidates []model.Port
	for _, p := range append(slices.Clone(st.Ports), commonPorts...) {
		if p != s.Port && !slices.Contains(candidates, p) && len(candidates) < portsToTry {
			candidates = append(candidates, p)
		}
	}
	sctx, cancel := context.WithTimeout(ctx, portSearch)
	defer cancel()
	for _, p := range candidates {
		if d.p.Ports.Answers(sctx, host, p) == nil {
			return model.Finding{Key: "find.port", Level: model.FindingUrgent, Args: []string{portArg(s.Port), portArg(p)}, FixPort: p}, true
		}
	}
	return model.Finding{}, false
}

// dnsFindings는 서비스 주소가 이 서버를 가리키는지 본다. 이 서버의 IP를 모르면 보지 않는다.
// 쓸 수 있는 인증서가 있는 주소는 건너뛴다 — 이미 여기로 온다는 뜻이고, 프록시를 쓰는 주소를 잘못 짚지 않는다.
func (d *diagnoser) dnsFindings(ctx context.Context, s model.Service) []model.Finding {
	if len(s.Domains) == 0 {
		return nil
	}
	here := d.thisServer(ctx)
	if len(here) == 0 {
		return nil
	}
	now := d.p.Clock.Now()
	certs := d.certificates(ctx, now)
	var out []model.Finding
	for _, dom := range s.Domains {
		if dom.HTTPS && certs.For(dom.Domain, now).Usable(now) {
			continue
		}
		a := d.lookup(ctx, dom.Domain, now)
		switch {
		case !a.Resolves:
			out = append(out, model.Finding{Key: "find.dns.none", Level: model.FindingWarning, Args: []string{dom.Domain.String(), strings.Join(here, ", ")}})
		case !overlaps(a.IPs, here):
			out = append(out, model.Finding{Key: "find.dns.elsewhere", Level: model.FindingWarning, Args: []string{dom.Domain.String(), strings.Join(here, ", "), a.IPList()}})
		}
	}
	return out
}

// thisServer는 이 서버의 IP다 — NARU_PUBLIC_IP, 없으면 관리 주소를 조회한 IP.
func (d *diagnoser) thisServer(ctx context.Context) []string {
	if d.p.PublicIP != "" {
		return []string{d.p.PublicIP}
	}
	admin, _ := d.p.Admin.Get(ctx)
	if admin.IsZero() {
		return nil
	}
	return d.lookup(ctx, admin, d.p.Clock.Now()).IPs
}

func (d *diagnoser) lookup(ctx context.Context, name model.DomainName, now time.Time) model.DNSAnswer {
	d.mu.Lock()
	seen, ok := d.dns[name.String()]
	d.mu.Unlock()
	if ok && now.Sub(seen.at) < dnsKeep {
		return seen.a
	}
	a := d.p.DNS.PointsHere(ctx, name, "")
	d.mu.Lock()
	d.dns[name.String()] = seenDNS{a: a, at: now}
	d.mu.Unlock()
	return a
}

func (d *diagnoser) certificates(ctx context.Context, now time.Time) model.Certificates {
	d.mu.Lock()
	defer d.mu.Unlock()
	if now.Sub(d.certsAt) >= certsKeep {
		d.certs, _ = d.p.Certs.Read(ctx) // 못 읽으면 인증서가 없는 것으로 본다
		d.certsAt = now
	}
	return d.certs
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// reach는 Naru가 컨테이너의 앱에 닿는 주소다 — 지금 IP, 배포 때 기록한 IP, 그래도 없으면 컨테이너 이름.
func reach(s model.Service, st model.ContainerState) string {
	switch {
	case st.IP != "":
		return st.IP
	case s.Live.InstanceIP != "":
		return s.Live.InstanceIP
	}
	return s.Live.CurrentContainer()
}

// portArg는 문구에 끼울 포트다.
func portArg(p model.Port) string { return strconv.Itoa(int(p)) }
