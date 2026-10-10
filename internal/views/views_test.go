package views

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/deploy"
	"github.com/KangminNa/naru/internal/events"
	"github.com/KangminNa/naru/internal/kinds"
	"github.com/KangminNa/naru/internal/model"
	"github.com/KangminNa/naru/internal/settings"
	"github.com/KangminNa/naru/internal/store"
	"github.com/KangminNa/naru/internal/system"
)

type noCerts struct{}

func (noCerts) Read(context.Context) (model.Certificates, error) { return nil, nil }

type noContainers struct{}

func (noContainers) All(context.Context) (model.ContainerStates, error) { return nil, nil }
func (noContainers) One(context.Context, string) (model.ContainerState, error) {
	return model.ContainerState{}, nil
}
func (noContainers) Logs(context.Context, string, int) (string, error)              { return "", nil }
func (noContainers) BelongingTo(context.Context, model.ServiceID) ([]string, error) { return nil, nil }

func TestWebhookAddressKeepsANonStandardHTTPSPort(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	services, history := store.NewServices(db), store.NewDeployments(db)
	name, _ := model.ParseServiceName("blog")
	id, _ := services.Create(ctx, model.NewService{Name: name, Kind: model.KindImage, Source: "me/blog"})
	domain, _ := model.ParseDomainName("naru.localhost")
	admin := settings.NewAdminDomainSetting(domain, store.NewSettings(db), events.NewBus())
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	deployer, _ := deploy.NewDeployer(ctx, quiet, deploy.Parts{Lock: deploy.NewMemoryDeployLock()})

	for port, want := range map[int]string{0: "https://naru.localhost/hooks/1", 443: "https://naru.localhost/hooks/1", 8443: "https://naru.localhost:8443/hooks/1"} {
		v := NewServiceViewer(Parts{
			Services: services, Secrets: store.NewSecrets(db), History: history, Containers: noContainers{},
			Kinds: kinds.NewLookup(kinds.Tools{}), Admin: admin, Deployer: deployer, Certs: noCerts{}, Clock: system.Clock{}, HTTPSPort: port, Web: store.NewWebSettings(db),
			Watch: fakeWatch{},
		}, quiet)
		view, err := v.Detail(ctx, id)
		if err != nil || view.Webhook.URL != want {
			t.Errorf("port %d: %q %v, want %q", port, view.Webhook.URL, err, want)
		}
	}
}

type fakeAppLogs struct {
	lines []model.LogLine
	err   error
}

func (f fakeAppLogs) Recent(context.Context, string, int) ([]model.LogLine, error) {
	return f.lines, f.err
}

type fakeAccess struct {
	lines []model.LogLine
	hosts []string
}

func (f *fakeAccess) Recent(_ context.Context, hosts []string, _ int) ([]model.LogLine, error) {
	f.hosts = hosts
	return f.lines, nil
}

func TestLogsMergeAppOutputAndRequestsByTime(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	services := store.NewServices(db)
	name, _ := model.ParseServiceName("blog")
	id, _ := services.Create(ctx, model.NewService{Name: name, Kind: model.KindImage, Alias: "naru-blog", Port: 80})
	store.NewLiveStates(db).Save(ctx, id, model.LiveState{Alias: "naru-blog", Instance: "naru-blog-2"})
	d, _ := model.ParseDomainName("blog.example.com")
	store.NewDomains(db).Add(ctx, id, model.DomainInput{Domain: d, HTTPS: true})

	t0 := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	app := fakeAppLogs{lines: []model.LogLine{{At: t0, Source: model.LogApp, Text: "start"}, {At: t0.Add(2 * time.Second), Source: model.LogApp, Text: "handled"}}}
	access := &fakeAccess{lines: []model.LogLine{{At: t0.Add(time.Second), Source: model.LogRequest, Method: "GET", Path: "/", Status: 200}}}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	deployer, _ := deploy.NewDeployer(ctx, quiet, deploy.Parts{Lock: deploy.NewMemoryDeployLock()})
	v := NewServiceViewer(Parts{Services: services, Secrets: store.NewSecrets(db), History: store.NewDeployments(db), Containers: noContainers{},
		Kinds: kinds.NewLookup(kinds.Tools{}), Deployer: deployer, Certs: noCerts{}, Clock: system.Clock{}, Web: store.NewWebSettings(db),
		AppLogs: app, Access: access}, quiet)

	all, err := v.Logs(ctx, id, model.LogsAll)
	if err != nil || len(all.Lines) != 3 || all.Lines[0].Text != "start" || all.Lines[1].Source != model.LogRequest || all.Lines[2].Text != "handled" {
		t.Fatalf("oldest first, both kinds interleaved: %+v %v", all.Lines, err)
	}
	if len(access.hosts) != 1 || access.hosts[0] != "blog.example.com" || !all.HasApp || !all.HasRequests {
		t.Fatalf("requests are read for the service's addresses: %v", access.hosts)
	}
	if onlyApp, _ := v.Logs(ctx, id, model.LogsApp); len(onlyApp.Lines) != 2 {
		t.Fatal("app filter")
	}
	if onlyReq, _ := v.Logs(ctx, id, model.LogsRequests); len(onlyReq.Lines) != 1 {
		t.Fatal("request filter")
	}
	v = NewServiceViewer(Parts{Services: services, Secrets: store.NewSecrets(db), History: store.NewDeployments(db), Containers: noContainers{},
		Kinds: kinds.NewLookup(kinds.Tools{}), Deployer: deployer, Certs: noCerts{}, Clock: system.Clock{}, Web: store.NewWebSettings(db),
		AppLogs: fakeAppLogs{err: errors.New("docker: not found")}, Access: access}, quiet)
	if half, _ := v.Logs(ctx, id, model.LogsAll); half.AppError == "" || len(half.Lines) != 1 {
		t.Fatalf("one side failing still shows the other: %+v", half)
	}
}

type fakeWatch struct{ snap model.WatchSnapshot }

func (f fakeWatch) Snapshot() model.WatchSnapshot { return f.snap }

func TestFindingsReachHomeAndTheirService(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	services := store.NewServices(db)
	blog, _ := model.ParseServiceName("blog")
	api, _ := model.ParseServiceName("api")
	b, _ := services.Create(ctx, model.NewService{Name: blog, Kind: model.KindExternal, External: "192.168.0.2:80"})
	a, _ := services.Create(ctx, model.NewService{Name: api, Kind: model.KindExternal, External: "192.168.0.3:80"})
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	deployer, _ := deploy.NewDeployer(ctx, quiet, deploy.Parts{Lock: deploy.NewMemoryDeployLock()})
	parts := Parts{Services: services, Secrets: store.NewSecrets(db), History: store.NewDeployments(db), Containers: noContainers{},
		Kinds: kinds.NewLookup(kinds.Tools{}), Deployer: deployer, Certs: noCerts{}, Clock: system.Clock{}, Web: store.NewWebSettings(db),
		Watch: fakeWatch{}}

	if h, _ := NewServiceViewer(parts, quiet).Home(ctx); h.Checked || len(h.Findings) != 0 {
		t.Fatal("before the first look nothing is claimed — not even \"all good\"")
	}
	parts.Watch = fakeWatch{model.WatchSnapshot{CheckedAt: time.Now(), Findings: []model.Finding{
		{Service: a, Name: "api", Key: "find.port", Level: model.FindingUrgent, FixPort: 8080},
		{Service: b, Name: "blog", Key: "find.webhook", Level: model.FindingHint},
	}, Usage: map[model.ServiceID]model.ResourceUsage{a: {CPUPercent: 3, MemUsed: 1 << 20}}}}
	v := NewServiceViewer(parts, quiet)
	h, _ := v.Home(ctx)
	if !h.Checked || len(h.Findings) != 2 || h.Findings[0].Key != "find.port" {
		t.Fatalf("home shows them all, heaviest first: %+v", h)
	}
	if d, _ := v.Detail(ctx, b); len(d.Findings) != 1 || d.Findings[0].Key != "find.webhook" || d.Usage != nil {
		t.Fatalf("a service shows only its own: %+v", d.Findings)
	}
	if d, _ := v.Detail(ctx, a); d.Usage == nil || d.Usage.CPUPercent != 3 {
		t.Fatal("usage from the watcher")
	}
	for _, c := range h.Cards {
		if (c.ID == a) != (c.Usage != nil) {
			t.Fatalf("cards carry usage when there is one: %+v", c)
		}
	}
}
