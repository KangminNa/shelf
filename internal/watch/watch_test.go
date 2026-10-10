package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/kinds"
	"github.com/KangminNa/naru/internal/model"
)

var ctx = context.Background()

type fakeServices struct{ all []model.Service }

func (f *fakeServices) List(context.Context) ([]model.Service, error) { return f.all, nil }
func (f *fakeServices) Get(context.Context, model.ServiceID) (model.Service, error) {
	return model.Service{}, model.ErrNotFound
}
func (f *fakeServices) NameTaken(context.Context, model.ServiceName) bool { return false }

type fakeContainers struct {
	states model.ContainerStates
	err    error
}

func (f *fakeContainers) All(context.Context) (model.ContainerStates, error) { return f.states, f.err }
func (f *fakeContainers) One(context.Context, string) (model.ContainerState, error) {
	return model.ContainerState{}, nil
}
func (f *fakeContainers) Logs(context.Context, string, int) (string, error) { return "", nil }
func (f *fakeContainers) BelongingTo(context.Context, model.ServiceID) ([]string, error) {
	return nil, nil
}

type fakePorts struct {
	deaf  map[string]bool       // 아무 포트도 응답하지 않는 주소
	only  map[string]model.Port // 이 포트만 응답하는 주소
	tried []model.Port
}

func (f *fakePorts) Answers(_ context.Context, host string, port model.Port) error {
	f.tried = append(f.tried, port)
	if f.deaf[host] {
		return errors.New("connection refused")
	}
	if p, ok := f.only[host]; ok && p != port {
		return errors.New("connection refused")
	}
	return nil
}

type fakeUsage struct {
	mu    sync.Mutex
	read  []string
	fails map[string]bool
}

func (f *fakeUsage) Usage(_ context.Context, name string) (model.ResourceUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.read = append(f.read, name)
	if f.fails[name] {
		return model.ResourceUsage{}, errors.New("no such container")
	}
	return model.ResourceUsage{CPUPercent: 12.5, MemUsed: 100 << 20, MemLimit: 8 << 30}, nil
}

type fakeDeployer struct{ busy map[model.ServiceID]bool }

func (f *fakeDeployer) Deploy(context.Context, model.ServiceID, model.DeployReason) (model.DeploymentID, error) {
	return 0, nil
}
func (f *fakeDeployer) RollBack(context.Context, model.ServiceID, model.DeploymentID) (model.DeploymentID, error) {
	return 0, nil
}
func (f *fakeDeployer) IsDeploying(id model.ServiceID) bool { return f.busy[id] }

type fakeWebServer struct{ up bool }

func (f *fakeWebServer) SyncNow(context.Context) error { return nil }
func (f *fakeWebServer) Status() model.WebServerStatus {
	if f.up {
		return model.WebServerStatus{Connected: true}
	}
	return model.WebServerStatus{LastError: "caddy: dial unix: no such file"}
}

type fakeCerts struct{ list model.Certificates }

func (f *fakeCerts) Read(context.Context) (model.Certificates, error) { return f.list, nil }

type fakeAdmin struct{ d model.DomainName }

func (f *fakeAdmin) Get(context.Context) (model.DomainName, model.SetBy) {
	return f.d, model.SetByScreen
}
func (f *fakeAdmin) Set(context.Context, model.DomainName) error { return nil }

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type recorder struct {
	mu     sync.Mutex
	events []model.Event
}

func (r *recorder) Publish(e model.Event) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }
func (r *recorder) take() []model.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

type rig struct {
	w          *healthWatcher
	services   *fakeServices
	containers *fakeContainers
	ports      *fakePorts
	deployer   *fakeDeployer
	web        *fakeWebServer
	certs      *fakeCerts
	clock      *fakeClock
	events     *recorder
	dns        *fakeDNS
	usage      *fakeUsage
}

func name(s string) model.ServiceName { n, _ := model.ParseServiceName(s); return n }

func newRig() *rig {
	r := &rig{
		services: &fakeServices{all: []model.Service{
			{ID: 1, Name: name("blog"), Kind: model.KindImage, Port: 80, Live: model.LiveState{Alias: "naru-blog", Instance: "naru-blog-3", InstanceIP: "10.0.0.3"}},
			{ID: 2, Name: name("site"), Kind: model.KindStatic, Live: model.LiveState{Release: "4"}},
			{ID: 3, Name: name("nas"), Kind: model.KindExternal, External: "192.168.0.20:5000"},
		}},
		containers: &fakeContainers{states: model.ContainerStates{"naru-blog-3": {Name: "naru-blog-3", Running: true, State: "running", IP: "10.0.0.3"}}},
		ports:      &fakePorts{deaf: map[string]bool{}, only: map[string]model.Port{}},
		deployer:   &fakeDeployer{busy: map[model.ServiceID]bool{}},
		web:        &fakeWebServer{up: true},
		certs:      &fakeCerts{},
		clock:      &fakeClock{now: time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)},
		events:     &recorder{},
	}
	r.dns = &fakeDNS{ips: map[string][]string{}}
	r.usage = &fakeUsage{fails: map[string]bool{}}
	lookup := kinds.NewLookup(kinds.Tools{})
	w, _ := NewHealthWatcher(Parts{
		Services: r.services, Kinds: lookup, Containers: r.containers, Ports: r.ports,
		Deployer: r.deployer, WebServer: r.web, Certs: r.certs, Admin: &fakeAdmin{}, Clock: r.clock, Events: r.events,
		Usage:     r.usage,
		Diagnoser: NewDiagnoser(DiagnoserParts{Kinds: lookup, Ports: r.ports, DNS: r.dns, Admin: &fakeAdmin{}, Certs: r.certs, Clock: r.clock, PublicIP: "198.51.100.24"}),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	r.w = w.(*healthWatcher)
	return r
}

func (r *rig) check() []model.Event {
	r.clock.now = r.clock.now.Add(30 * time.Second)
	r.w.check(ctx)
	return r.events.take()
}

func (r *rig) crash() {
	r.containers.states["naru-blog-3"] = model.ContainerState{Name: "naru-blog-3", State: "exited", ExitCode: 1}
}

func TestDownNeedsTwoChecksAndEachChangeIsSentOnce(t *testing.T) {
	r := newRig()
	if ev := r.check(); len(ev) != 0 {
		t.Fatalf("a healthy start says nothing: %v", ev)
	}
	r.crash()
	if ev := r.check(); len(ev) != 0 {
		t.Fatalf("one bad check is not enough (deploys and restarts blink): %v", ev)
	}
	ev := r.check()
	if len(ev) != 1 {
		t.Fatalf("the second bad check reports it: %v", ev)
	}
	down, ok := ev[0].(model.ServiceDown)
	if !ok || down.Service != 1 || down.Name != "blog" || down.Why != "crashed" {
		t.Fatalf("%+v", ev[0])
	}
	if ev := r.check(); len(ev) != 0 {
		t.Fatalf("still down is not news: %v", ev)
	}
	if snap := r.w.Snapshot(); len(snap.Down) != 1 || snap.Down[0].Why != "crashed" || !snap.DockerUp || !snap.WebServerUp {
		t.Fatalf("%+v", snap)
	}
	r.containers.states["naru-blog-3"] = model.ContainerState{Name: "naru-blog-3", Running: true, State: "running"}
	ev = r.check()
	if len(ev) != 1 {
		t.Fatalf("recovery is reported right away: %v", ev)
	}
	if up, ok := ev[0].(model.ServiceUp); !ok || up.Name != "blog" {
		t.Fatalf("%+v", ev[0])
	}
	if len(r.w.Snapshot().Down) != 0 {
		t.Fatal("no longer down")
	}
}

func TestWhatWasDownAtStartIsNotAlerted(t *testing.T) {
	r := newRig()
	r.crash()
	if ev := append(r.check(), r.check()...); len(ev) != 0 {
		t.Fatalf("a restart of Naru must not flood alerts: %v", ev)
	}
	if len(r.w.Snapshot().Down) != 1 {
		t.Fatal("but it is shown as down")
	}
}

func TestRunningButNotAnsweringIsDown(t *testing.T) {
	r := newRig()
	r.check()
	r.ports.deaf["10.0.0.3"] = true
	r.check()
	ev := r.check()
	if len(ev) != 1 || ev[0].(model.ServiceDown).Why != "noanswer" {
		t.Fatalf("%v", ev)
	}
}

func TestStoppedAndDeployingServicesAreLeftAlone(t *testing.T) {
	r := newRig()
	r.check()
	r.crash()
	r.deployer.busy[1] = true
	if ev := append(r.check(), r.check()...); len(ev) != 0 {
		t.Fatalf("a deploy swaps containers — not an outage: %v", ev)
	}
	r.deployer.busy[1] = false
	r.services.all[0].Live.Stopped = true
	if ev := append(r.check(), r.check()...); len(ev) != 0 {
		t.Fatalf("stopped on purpose is not an outage: %v", ev)
	}
}

func TestOnlyContainerServicesAreWatched(t *testing.T) {
	r := newRig()
	r.ports.deaf["192.168.0.20"] = true // 외부 연결은 감시하지 않는다
	r.check()
	if ev := append(r.check(), r.check()...); len(ev) != 0 {
		t.Fatalf("static sites and external services are not watched: %v", ev)
	}
}

func TestDockerAndWebServerOutages(t *testing.T) {
	r := newRig()
	r.check()
	r.containers.err = errors.New("dial unix /var/run/docker.sock: connect: permission denied")
	r.crash() // Docker를 못 읽을 때는 서비스를 판정하지 않는다
	if ev := r.check(); len(ev) != 0 {
		t.Fatalf("%v", ev)
	}
	ev := r.check()
	if len(ev) != 1 {
		t.Fatalf("docker down once: %v", ev)
	}
	if d, ok := ev[0].(model.DockerDown); !ok || d.Why == "" {
		t.Fatalf("%+v", ev[0])
	}
	if r.w.Snapshot().DockerUp {
		t.Fatal("snapshot says so")
	}
	r.containers.err = nil
	r.containers.states["naru-blog-3"] = model.ContainerState{Name: "naru-blog-3", Running: true, State: "running"}
	if ev := r.check(); len(ev) != 1 {
		t.Fatalf("docker back: %v", ev)
	} else if _, ok := ev[0].(model.DockerUp); !ok {
		t.Fatalf("%+v", ev[0])
	}

	r.web.up = false
	r.check()
	ev = r.check()
	if len(ev) != 1 {
		t.Fatalf("web server down once: %v", ev)
	}
	if _, ok := ev[0].(model.WebServerDown); !ok {
		t.Fatalf("%+v", ev[0])
	}
	r.web.up = true
	if ev := r.check(); len(ev) != 1 {
		t.Fatalf("%v", ev)
	} else if _, ok := ev[0].(model.WebServerUp); !ok {
		t.Fatalf("%+v", ev[0])
	}
}

func TestCertificateEndingSoonOncePerDay(t *testing.T) {
	r := newRig()
	d, _ := model.ParseDomainName("blog.example.com")
	r.services.all[0].Domains = []model.Domain{{Domain: d, HTTPS: true}}
	r.certs.list = model.Certificates{{Names: []string{"blog.example.com"}, NotBefore: r.clock.now.Add(-80 * 24 * time.Hour), NotAfter: r.clock.now.Add(5 * 24 * time.Hour)}}
	ev := r.check()
	if len(ev) != 1 {
		t.Fatalf("%v", ev)
	}
	if c, ok := ev[0].(model.CertificateEndingSoon); !ok || c.Domain != "blog.example.com" {
		t.Fatalf("%+v", ev[0])
	}
	if ev := r.check(); len(ev) != 0 {
		t.Fatalf("not again the same day: %v", ev)
	}
	r.clock.now = r.clock.now.Add(24 * time.Hour)
	if ev := r.check(); len(ev) != 1 {
		t.Fatalf("again the next day: %v", ev)
	}
}

func TestUsageOfRunningContainerServices(t *testing.T) {
	r := newRig()
	r.check()
	snap := r.w.Snapshot()
	if u, ok := snap.Usage[1]; !ok || u.CPUPercent != 12.5 || u.MemUsed != 100<<20 {
		t.Fatalf("%+v", snap.Usage)
	}
	if len(snap.Usage) != 1 || len(r.usage.read) != 1 || r.usage.read[0] != "naru-blog-3" {
		t.Fatalf("only running containers are read (not static sites or external services): %v", r.usage.read)
	}

	r.crash()
	r.usage.read = nil
	r.check()
	if len(r.w.Snapshot().Usage) != 0 || len(r.usage.read) != 0 {
		t.Fatal("a stopped container has no usage")
	}

	r.containers.states["naru-blog-3"] = model.ContainerState{Name: "naru-blog-3", Running: true, State: "running", IP: "10.0.0.3"}
	r.usage.fails["naru-blog-3"] = true
	r.check()
	if len(r.w.Snapshot().Usage) != 0 {
		t.Fatal("a failed read leaves it out")
	}

	r.usage.fails["naru-blog-3"] = false
	r.containers.err = errors.New("docker down")
	r.check()
	if len(r.w.Snapshot().Usage) != 0 {
		t.Fatal("without Docker there is nothing to read")
	}
}

func TestUsageIsReadFourAtATime(t *testing.T) {
	r := newRig()
	r.services.all = nil
	for i := 1; i <= 9; i++ {
		c := fmt.Sprintf("naru-app%d-1", i)
		r.services.all = append(r.services.all, model.Service{ID: model.ServiceID(i), Name: name(fmt.Sprintf("app%d", i)), Kind: model.KindImage, Port: 80,
			Live: model.LiveState{Alias: fmt.Sprintf("naru-app%d", i), Instance: c}})
		r.containers.states[c] = model.ContainerState{Name: c, Running: true, State: "running"}
	}
	slow := &slowUsage{}
	r.w.p.Usage = slow
	r.check()
	if got := len(r.w.Snapshot().Usage); got != 9 {
		t.Fatalf("all nine: %d", got)
	}
	if slow.peak > 4 || slow.peak < 2 {
		t.Fatalf("read in parallel, at most four at once: peak %d", slow.peak)
	}
}

type slowUsage struct {
	mu        sync.Mutex
	now, peak int
}

func (s *slowUsage) Usage(context.Context, string) (model.ResourceUsage, error) {
	s.mu.Lock()
	s.now++
	s.peak = max(s.peak, s.now)
	s.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	s.mu.Lock()
	s.now--
	s.mu.Unlock()
	return model.ResourceUsage{CPUPercent: 1}, nil
}
