package views

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

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
		}, quiet)
		view, err := v.Detail(ctx, id)
		if err != nil || view.Webhook.URL != want {
			t.Errorf("port %d: %q %v, want %q", port, view.Webhook.URL, err, want)
		}
	}
}
