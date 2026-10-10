package watch

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/kinds"
	"github.com/KangminNa/naru/internal/model"
)

type fakeDNS struct {
	ips     map[string][]string // 없으면 조회되지 않는 이름
	lookups int
}

func (f *fakeDNS) PointsHere(_ context.Context, d model.DomainName, thisServer string) model.DNSAnswer {
	f.lookups++
	a := model.DNSAnswer{Domain: d.String(), Expected: thisServer}
	if ips, ok := f.ips[d.String()]; ok {
		a.IPs, a.Resolves = ips, true
		a.Matches = thisServer != "" && slices.Contains(ips, thisServer)
	}
	return a
}

func domain(s string) model.DomainName { d, _ := model.ParseDomainName(s); return d }

type diagRig struct {
	d     *diagnoser
	ports *fakePorts
	dns   *fakeDNS
	certs *fakeCerts
	admin *fakeAdmin
	clock *fakeClock
}

func newDiagRig(publicIP string) *diagRig {
	r := &diagRig{
		ports: &fakePorts{deaf: map[string]bool{}, only: map[string]model.Port{}},
		dns:   &fakeDNS{ips: map[string][]string{}},
		certs: &fakeCerts{},
		admin: &fakeAdmin{},
		clock: &fakeClock{now: time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)},
	}
	r.d = NewDiagnoser(DiagnoserParts{Kinds: kinds.NewLookup(kinds.Tools{}), Ports: r.ports, DNS: r.dns, Admin: r.admin,
		Certs: r.certs, Clock: r.clock, PublicIP: publicIP}).(*diagnoser)
	return r
}

func app(port model.Port) model.Service {
	return model.Service{ID: 1, Name: name("blog"), Kind: model.KindImage, Port: port,
		Live: model.LiveState{Alias: "naru-blog", Instance: "naru-blog-3", InstanceIP: "10.0.0.3"}}
}

func running(ports ...model.Port) model.ContainerStates {
	return model.ContainerStates{"naru-blog-3": {Name: "naru-blog-3", Running: true, State: "running", IP: "10.0.0.3", Ports: ports}}
}

func keys(fs []model.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Key)
	}
	return out
}

func TestAppListeningOnAnotherPortIsFoundWithAFix(t *testing.T) {
	r := newDiagRig("")
	r.ports.only["10.0.0.3"] = 8080 // 이미지가 8080을 열었고, 앱도 거기서 듣는다
	fs := r.d.Diagnose(ctx, app(3000), running(8080))
	if len(fs) != 1 {
		t.Fatalf("%+v", fs)
	}
	f := fs[0]
	if f.Key != "find.port" || f.Level != model.FindingUrgent || f.FixPort != 8080 || !slices.Equal(f.Args, []string{"3000", "8080"}) || f.Service != 1 || f.Name != "blog" {
		t.Fatalf("%+v", f)
	}

	// EXPOSE가 없어도 흔한 포트는 찔러 본다
	r.ports.only["10.0.0.3"] = 3000
	if fs := r.d.Diagnose(ctx, app(80), running()); len(fs) != 1 || fs[0].FixPort != 3000 {
		t.Fatalf("%+v", fs)
	}
	// 앱 포트가 응답하면 다른 포트는 찔러 보지 않는다
	r.ports.tried = nil
	if fs := r.d.Diagnose(ctx, app(3000), running(8080)); len(fs) != 0 || len(r.ports.tried) != 1 {
		t.Fatalf("%+v tried %v", fs, r.ports.tried)
	}
	// 아무 포트도 응답하지 않으면 고칠 방법을 모른다 — 멈춤은 지켜보기가 말한다
	r.ports.deaf["10.0.0.3"] = true
	if fs := r.d.Diagnose(ctx, app(3000), running(8080)); len(fs) != 0 {
		t.Fatalf("%+v", fs)
	}
	// 실행 중이 아니거나 Docker를 못 읽으면 포트를 보지 않는다
	r.ports.deaf["10.0.0.3"], r.ports.only["10.0.0.3"] = false, 8080
	if fs := r.d.Diagnose(ctx, app(3000), nil); len(fs) != 0 {
		t.Fatalf("%+v", fs)
	}
}

func TestDNSThatDoesNotPointHere(t *testing.T) {
	r := newDiagRig("198.51.100.24")
	s := app(80)
	s.Domains = []model.Domain{
		{Domain: domain("a.example.com"), HTTPS: true},
		{Domain: domain("b.example.com"), HTTPS: true},
		{Domain: domain("c.example.com")},
		{Domain: domain("d.example.com"), HTTPS: true},
	}
	r.dns.ips["a.example.com"] = []string{"198.51.100.24"}
	r.dns.ips["b.example.com"] = []string{"203.0.113.9"}
	r.dns.ips["d.example.com"] = []string{"104.21.3.4"} // 프록시 — 인증서가 있으니 이미 여기로 온다
	r.certs.list = model.Certificates{{Names: []string{"d.example.com"}, NotBefore: r.clock.now.Add(-time.Hour), NotAfter: r.clock.now.Add(60 * 24 * time.Hour)}}

	fs := r.d.Diagnose(ctx, s, running())
	if !slices.Equal(keys(fs), []string{"find.dns.elsewhere", "find.dns.none"}) {
		t.Fatalf("%+v", fs)
	}
	if !slices.Equal(fs[0].Args, []string{"b.example.com", "198.51.100.24", "203.0.113.9"}) || fs[0].Level != model.FindingWarning {
		t.Fatalf("%+v", fs[0])
	}
	if !slices.Equal(fs[1].Args, []string{"c.example.com", "198.51.100.24"}) {
		t.Fatalf("%+v", fs[1])
	}
	n := r.dns.lookups
	r.clock.now = r.clock.now.Add(time.Minute)
	r.d.Diagnose(ctx, s, running())
	if r.dns.lookups != n {
		t.Fatal("DNS answers are kept for a while — not looked up every 30 seconds")
	}
	r.clock.now = r.clock.now.Add(11 * time.Minute)
	r.d.Diagnose(ctx, s, running())
	if r.dns.lookups == n {
		t.Fatal("and looked up again later")
	}
}

func TestThisServerIPComesFromTheAdminAddress(t *testing.T) {
	r := newDiagRig("")
	s := app(80)
	s.Domains = []model.Domain{{Domain: domain("b.example.com")}}
	r.dns.ips["b.example.com"] = []string{"203.0.113.9"}
	if fs := r.d.Diagnose(ctx, s, running()); len(fs) != 0 {
		t.Fatalf("without knowing our own IP there is no DNS finding: %+v", fs)
	}
	r.admin.d = domain("naru.example.com")
	r.dns.ips["naru.example.com"] = []string{"198.51.100.24"}
	r.clock.now = r.clock.now.Add(11 * time.Minute)
	if fs := r.d.Diagnose(ctx, s, running()); len(fs) != 1 || fs[0].Args[1] != "198.51.100.24" {
		t.Fatalf("%+v", fs)
	}
	r.dns.ips["b.example.com"] = []string{"198.51.100.24"}
	r.clock.now = r.clock.now.Add(11 * time.Minute)
	if fs := r.d.Diagnose(ctx, s, running()); len(fs) != 0 {
		t.Fatalf("same IP as the admin address: %+v", fs)
	}
}

func TestWebhookThatNeverArrived(t *testing.T) {
	r := newDiagRig("")
	repo := model.Service{ID: 4, Name: name("landing"), Kind: model.KindRepo, Source: "https://github.com/me/landing", AutoDeploy: true, Created: r.clock.now.Add(-48 * time.Hour)}
	if fs := r.d.Diagnose(ctx, repo, running()); len(fs) != 1 || fs[0].Key != "find.webhook" || fs[0].Level != model.FindingHint {
		t.Fatalf("%+v", fs)
	}
	for why, s := range map[string]model.Service{
		"made today":       {ID: 4, Kind: model.KindRepo, Source: repo.Source, AutoDeploy: true, Created: r.clock.now.Add(-2 * time.Hour)},
		"a ping arrived":   {ID: 4, Kind: model.KindRepo, Source: repo.Source, AutoDeploy: true, Created: repo.Created, HookLog: model.HookLog{At: r.clock.now, Result: "ping"}},
		"auto deploy off":  {ID: 4, Kind: model.KindRepo, Source: repo.Source, Created: repo.Created},
		"images need none": {ID: 4, Kind: model.KindImage, Source: "ghcr.io/me/landing", AutoDeploy: true, Created: repo.Created},
	} {
		if fs := r.d.Diagnose(ctx, s, running()); len(fs) != 0 {
			t.Errorf("%s: %+v", why, fs)
		}
	}
	static := repo
	static.Kind = model.KindStatic
	if fs := r.d.Diagnose(ctx, static, running()); len(fs) != 1 {
		t.Fatalf("static sites from a repository wait for pushes too: %+v", fs)
	}
}

// 지켜보기는 진단과 멈춤·인증서를 합쳐 무거운 것부터 담는다.
func TestSnapshotCollectsFindingsHeaviestFirst(t *testing.T) {
	r := newRig()
	d := domain("blog.example.com")
	r.services.all[0].Domains = []model.Domain{{Domain: d, HTTPS: true}}
	r.dns.ips["blog.example.com"] = []string{"203.0.113.9"}
	r.certs.list = model.Certificates{{Names: []string{"blog.example.com"}, NotBefore: r.clock.now.Add(-80 * 24 * time.Hour), NotAfter: r.clock.now.Add(5 * 24 * time.Hour)}}
	r.check()
	snap := r.w.Snapshot()
	// 인증서가 곧 끝나도 아직 쓸 수 있다 — DNS는 여기로 온다고 본다
	if !slices.Equal(keys(snap.Findings), []string{"find.cert"}) || snap.Findings[0].Args[0] != "blog.example.com" || snap.Findings[0].Name != "blog" {
		t.Fatalf("%+v", snap.Findings)
	}

	r.crash()
	r.check()
	r.check()
	if got := keys(r.w.Snapshot().Findings); !slices.Equal(got, []string{"find.crashed", "find.cert"}) {
		t.Fatalf("down first: %v", got)
	}

	// 응답 없음 + 다른 포트가 응답 → "응답 없음" 대신 고칠 방법
	r.containers.states["naru-blog-3"] = model.ContainerState{Name: "naru-blog-3", Running: true, State: "running", IP: "10.0.0.3", Ports: []model.Port{8080}}
	r.ports.only["10.0.0.3"] = 8080
	r.check()
	r.check()
	snap = r.w.Snapshot()
	if got := keys(snap.Findings); !slices.Equal(got, []string{"find.port", "find.cert"}) || snap.Findings[0].FixPort != 8080 {
		t.Fatalf("%+v", snap.Findings)
	}

	// 배포 중에는 다시 보지 않고 마지막 진단을 그대로 둔다
	r.deployer.busy[1] = true
	r.ports.only["10.0.0.3"] = 80
	r.check()
	if got := keys(r.w.Snapshot().Findings); !slices.Equal(got, []string{"find.port", "find.cert"}) {
		t.Fatalf("%v", got)
	}
	// 직접 멈춘 서비스는 말하지 않는다
	r.deployer.busy[1] = false
	r.services.all[0].Live.Stopped = true
	r.check()
	if got := keys(r.w.Snapshot().Findings); !slices.Equal(got, []string{"find.cert"}) {
		t.Fatalf("%v", got)
	}
}
