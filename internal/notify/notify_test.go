package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/events"
	"github.com/KangminNa/naru/internal/model"
)

var ctx = context.Background()

type memChannels struct {
	mu   sync.Mutex
	next model.ChannelID
	all  []model.AlertChannel
}

func (m *memChannels) List(context.Context) ([]model.AlertChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.AlertChannel{}, m.all...), nil
}
func (m *memChannels) Add(_ context.Context, c model.AlertChannel) (model.ChannelID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	c.ID = m.next
	m.all = append(m.all, c)
	return c.ID, nil
}
func (m *memChannels) Remove(_ context.Context, id model.ChannelID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, c := range m.all {
		if c.ID == id {
			m.all = append(m.all[:i], m.all[i+1:]...)
			return nil
		}
	}
	return model.ErrNotFound
}

type memDeliveries struct {
	mu  sync.Mutex
	all []model.Delivery
}

func (m *memDeliveries) Save(_ context.Context, d model.Delivery) error {
	m.mu.Lock()
	m.all = append(m.all, d)
	m.mu.Unlock()
	return nil
}
func (m *memDeliveries) Recent(_ context.Context, n int) ([]model.Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Delivery{}, m.all...), nil
}

type sent struct {
	ch model.AlertChannel
	a  model.Alert
}

type fakeSender struct {
	mu   sync.Mutex
	sent []sent
	fail map[string]error // 주소 → 실패
}

func (f *fakeSender) Send(_ context.Context, ch model.AlertChannel, a model.Alert) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{ch, a})
	return f.fail[ch.URL]
}
func (f *fakeSender) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.sent) }

type fakeServices struct{}

func (fakeServices) List(context.Context) ([]model.Service, error) { return nil, nil }
func (fakeServices) Get(_ context.Context, id model.ServiceID) (model.Service, error) {
	if id == 1 {
		n, _ := model.ParseServiceName("blog")
		return model.Service{ID: 1, Name: n}, nil
	}
	return model.Service{}, model.ErrNotFound
}
func (fakeServices) NameTaken(context.Context, model.ServiceName) bool { return false }

type clock struct{}

func (clock) Now() time.Time { return time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC) }

func newRig(t *testing.T) (*memChannels, *memDeliveries, *fakeSender, *events.Bus, *alerts) {
	t.Helper()
	ch, dl, s, bus := &memChannels{}, &memDeliveries{}, &fakeSender{fail: map[string]error{}}, events.NewBus()
	a, run := NewAlerts(Parts{Channels: ch, Deliveries: dl, Sender: s, Services: fakeServices{}, Events: bus, Clock: clock{},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	c, cancel := context.WithCancel(ctx)
	go run(c)
	t.Cleanup(cancel)
	return ch, dl, s, bus, a.(*alerts)
}

func url(s string) model.AlertURL {
	u, err := model.ParseAlertURL(s)
	if err != nil {
		panic(err)
	}
	return u
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestEventsBecomeAlertsForEveryChannel(t *testing.T) {
	_, dl, s, bus, a := newRig(t)
	a.Add(ctx, model.ChannelInput{Name: "team", URL: url("https://discord.com/api/webhooks/1/abc")})
	a.Add(ctx, model.ChannelInput{URL: url("https://hooks.example.com/naru"), Secret: "s3cret"})
	s.fail["https://hooks.example.com/naru"] = errors.New("503 Service Unavailable")

	bus.Publish(model.ServiceDown{Service: 1, Name: "blog", Why: "crashed"})
	waitFor(t, func() bool { return s.count() == 2 })
	got := s.sent[0].a
	if got.Event != "service.down" || got.Level != model.AlertProblem || got.Service != "blog" || !strings.Contains(got.Title, "blog") {
		t.Fatalf("%+v", got)
	}
	waitFor(t, func() bool { d, _ := dl.Recent(ctx, 10); return len(d) == 2 })
	d, _ := dl.Recent(ctx, 10)
	ok, failed := 0, 0
	for _, x := range d {
		if x.OK {
			ok++
		} else if strings.Contains(x.Detail, "503") {
			failed++
		}
	}
	if ok != 1 || failed != 1 {
		t.Fatalf("every send is recorded with its result: %+v", d)
	}
}

func TestEachEventKind(t *testing.T) {
	_, _, s, bus, a := newRig(t)
	a.Add(ctx, model.ChannelInput{URL: url("https://hooks.example.com/naru")})
	cases := []struct {
		e     model.Event
		event string
		level model.AlertLevel
	}{
		{model.ServiceUp{Service: 1, Name: "blog"}, "service.up", model.AlertRecovery},
		{model.DeployFinished{Service: 1, Deployment: 7, OK: false}, "deploy.failed", model.AlertProblem},
		{model.WebServerDown{Why: "dial unix"}, "webserver.down", model.AlertProblem},
		{model.WebServerUp{}, "webserver.up", model.AlertRecovery},
		{model.DockerDown{Why: "permission denied"}, "docker.down", model.AlertProblem},
		{model.DockerUp{}, "docker.up", model.AlertRecovery},
		{model.CertificateEndingSoon{Domain: "blog.example.com", NotAfter: time.Now().Add(5 * 24 * time.Hour)}, "certificate.ending", model.AlertProblem},
	}
	for i, c := range cases {
		bus.Publish(c.e)
		waitFor(t, func() bool { return s.count() == i+1 })
		got := s.sent[i].a
		if got.Event != c.event || got.Level != c.level || got.Title == "" {
			t.Errorf("%T → %+v", c.e, got)
		}
	}
	if s.sent[1].a.Service != "blog" || !strings.Contains(s.sent[1].a.Detail, "#7") {
		t.Errorf("a failed deploy names its service and deployment: %+v", s.sent[1].a)
	}
	bus.Publish(model.DeployFinished{Service: 1, Deployment: 8, OK: true})
	bus.Publish(model.SiteMapChanged{Reason: "x"})
	time.Sleep(30 * time.Millisecond)
	if s.count() != len(cases) {
		t.Fatal("successful deploys and other events are not alerts")
	}
}

func TestChannelsAreShownMasked(t *testing.T) {
	_, _, _, _, a := newRig(t)
	a.Add(ctx, model.ChannelInput{Name: "team", URL: url("https://discord.com/api/webhooks/123/very-secret-token")})
	a.Add(ctx, model.ChannelInput{URL: url("https://hooks.slack.com/services/T0/B0/xyz")})
	a.Add(ctx, model.ChannelInput{URL: url("https://ops.example.com/hook?key=abc"), Secret: "s"})
	views, _ := a.Channels(ctx)
	want := []model.ChannelView{
		{ID: 1, Name: "team", Target: "discord.com/…", Format: "discord"},
		{ID: 2, Name: "hooks.slack.com", Target: "hooks.slack.com/…", Format: "slack"},
		{ID: 3, Name: "ops.example.com", Target: "ops.example.com/…", Format: "json", HasSecret: true},
	}
	if len(views) != 3 {
		t.Fatalf("%+v", views)
	}
	for i := range want {
		if views[i] != want[i] {
			t.Errorf("%+v, want %+v", views[i], want[i])
		}
	}
}

func TestTestSendsNowAndSaysWhetherItWorked(t *testing.T) {
	_, _, s, _, a := newRig(t)
	a.Add(ctx, model.ChannelInput{URL: url("https://hooks.example.com/ok")})
	a.Add(ctx, model.ChannelInput{URL: url("https://hooks.example.com/bad")})
	s.fail["https://hooks.example.com/bad"] = errors.New("404 Not Found")
	if err := a.Test(ctx, 1); err != nil || s.count() != 1 || s.sent[0].a.Event != "test" {
		t.Fatalf("%v %+v", err, s.sent)
	}
	if err := a.Test(ctx, 2); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("the reason comes back: %v", err)
	}
	if err := a.Test(ctx, 99); !errors.Is(err, model.ErrNotFound) {
		t.Fatal(err)
	}
	a.Remove(ctx, 1)
	if v, _ := a.Channels(ctx); len(v) != 1 {
		t.Fatal("removed")
	}
}
