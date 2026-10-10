package webserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/caddy"
	"github.com/KangminNa/naru/internal/events"
	"github.com/KangminNa/naru/internal/kinds"
	"github.com/KangminNa/naru/internal/model"
	"github.com/KangminNa/naru/internal/settings"
	"github.com/KangminNa/naru/internal/store"
)

var ctx = context.Background()

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func domain(s string) model.DomainName {
	d, err := model.ParseDomainName(s)
	if err != nil {
		panic(err)
	}
	return d
}

func name(s string) model.ServiceName {
	n, _ := model.ParseServiceName(s)
	return n
}

func TestSiteMapFollowsServicesAndSettings(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bus := events.NewBus()
	set := store.NewSettings(db)
	admin := settings.NewAdminDomainSetting(model.DomainName{}, set, bus)
	setup := settings.NewSetupProgress(set, bus)
	services, domains, live := store.NewServices(db), store.NewDomains(db), store.NewLiveStates(db)

	blog, _ := services.Create(ctx, model.NewService{Name: name("blog"), Kind: model.KindRepo, Alias: "shelf-blog", Port: 3000})
	domains.Add(ctx, blog, model.DomainInput{Domain: domain("blog.example.com"), HTTPS: true})
	domains.Add(ctx, blog, model.DomainInput{Domain: domain("naru.example.com")})
	site, _ := services.Create(ctx, model.NewService{Name: name("landing"), Kind: model.KindStatic})
	domains.Add(ctx, site, model.DomainInput{Domain: domain("www.example.com"), HTTPS: true})
	live.Save(ctx, site, model.LiveState{Release: "12"})
	nas, _ := services.Create(ctx, model.NewService{Name: name("nas"), Kind: model.KindExternal, External: "host.docker.internal:5000"})
	domains.Add(ctx, nas, model.DomainInput{Domain: domain("nas.example.com")})

	b := NewSiteMapBuilder(Fixed{AdminSocket: "/run/caddy/admin.sock", AdminUpstream: "naru:8080"}, services,
		kinds.NewLookup(kinds.Tools{}), admin, settings.NewCertEmailSetting(model.Email{}, set, bus), setup)

	m, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !m.OpenFallback || len(m.AdminHosts) != 0 {
		t.Fatal("before setup any address reaches the admin screen")
	}
	to := map[string]model.Destination{}
	for _, s := range m.Sites {
		to[s.Hosts[0]] = s.Destination
	}
	if to["blog.example.com"].Address != "shelf-blog:3000" || to["nas.example.com"].Address != "host.docker.internal:5000" ||
		to["www.example.com"].Folder != "/srv/sites/landing/12" {
		t.Fatalf("%+v", to)
	}

	admin.Set(ctx, domain("naru.example.com"))
	setup.MarkDone(ctx)
	m, _ = b.Build(ctx)
	if m.OpenFallback || m.AdminHosts[0] != "naru.example.com" || !m.AdminHTTPS {
		t.Fatalf("%+v", m)
	}
	for _, s := range m.Sites {
		if s.Hosts[0] == "naru.example.com" {
			t.Fatal("the admin address wins over a service that also claims it")
		}
	}
	if _, err := (caddy.JSONWriter{}).Write(m); err != nil {
		t.Fatal("the map is writable:", err)
	}
}

type fakeBuilder struct {
	m   model.SiteMap
	err error
}

func (f *fakeBuilder) Build(context.Context) (model.SiteMap, error) { return f.m, f.err }

type fakeSender struct {
	mu    sync.Mutex
	loads [][]byte
	pings int
	fail  error
}

func (f *fakeSender) Send(_ context.Context, cfg []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.loads = append(f.loads, cfg)
	return nil
}

func (f *fakeSender) Ping(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pings++
	return f.fail
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.loads)
}

func TestSyncSendsOnceAndThenOnlyChecks(t *testing.T) {
	b := &fakeBuilder{m: model.SiteMap{AdminSocket: "/run/caddy/admin.sock", AdminUpstream: "naru:8080",
		Sites: []model.Site{{Hosts: []string{"blog.example.com"}, Destination: model.Destination{Address: "shelf-blog:3000"}}}}}
	sender := &fakeSender{}
	s, _ := NewWebServerSync(b, caddy.JSONWriter{}, sender, events.NewBus(), quiet)

	if err := s.SyncNow(ctx); err != nil {
		t.Fatal(err)
	}
	s.SyncNow(ctx)
	if len(sender.loads) != 1 || sender.pings != 1 {
		t.Fatalf("an unchanged config is not sent again: loads=%d pings=%d", len(sender.loads), sender.pings)
	}
	b.m.Sites = append(b.m.Sites, model.Site{Hosts: []string{"new.example.com"}, Destination: model.Destination{Address: "naru-new:80"}})
	s.SyncNow(ctx)
	if len(sender.loads) != 2 || !strings.Contains(string(sender.loads[1]), "new.example.com") {
		t.Fatal("a change is sent")
	}
	if !s.Status().Connected {
		t.Fatal("status is connected")
	}
}

func TestSyncReportsRefusalAndRetries(t *testing.T) {
	sender := &fakeSender{fail: errors.New("caddy: POST /load: 400 loading config: bad")}
	s, _ := NewWebServerSync(&fakeBuilder{m: model.SiteMap{AdminSocket: "/s", AdminUpstream: "naru:8080"}}, caddy.JSONWriter{}, sender, events.NewBus(), quiet)
	if err := s.SyncNow(ctx); err == nil {
		t.Fatal("a refused config is an error")
	}
	if st := s.Status(); st.Connected || !strings.Contains(st.LastError, "loading config") {
		t.Fatalf("%+v", st)
	}
	sender.fail = nil
	if err := s.SyncNow(ctx); err != nil || len(sender.loads) != 1 {
		t.Fatal("after a failure the next sync sends again")
	}

	broken, _ := NewWebServerSync(&fakeBuilder{err: errors.New("db gone")}, caddy.JSONWriter{}, &fakeSender{}, events.NewBus(), quiet)
	if broken.SyncNow(ctx) == nil || !strings.Contains(broken.Status().LastError, "db gone") {
		t.Fatal("build errors surface in the status")
	}
}

func TestAChangeIsSyncedRightAway(t *testing.T) {
	bus := events.NewBus()
	sender := &fakeSender{}
	b := &fakeBuilder{m: model.SiteMap{AdminSocket: "/s", AdminUpstream: "naru:8080"}}
	_, run := NewWebServerSync(b, caddy.JSONWriter{}, sender, bus, quiet)
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	go run(c)
	wait := func(n int) {
		for i := 0; i < 200 && sender.count() < n; i++ {
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait(1)
	b.m.Sites = []model.Site{{Hosts: []string{"x.example.com"}, Destination: model.Destination{Address: "x:1"}}}
	bus.Publish(model.SiteMapChanged{Reason: "test"})
	wait(2)
	if sender.count() != 2 {
		t.Fatalf("a SiteMapChanged event is applied without waiting 30s: %d loads", sender.count())
	}
}
