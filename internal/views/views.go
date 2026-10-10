// Package views는 화면에 보여줄 서비스 모습을 모은다 (CQRS의 읽기 쪽).
// 고치는 일은 하지 않는다. 비밀은 화면에 필요한 것(웹훅 시크릿, 토큰이 있는지)만 꺼낸다 — 토큰 원문은 담지 않는다.
package views

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// Parts는 화면 모으기가 쓰는 것들이다.
type Parts struct {
	Services   contract.ServiceReader
	Secrets    contract.SecretStore
	History    contract.DeployHistoryReader
	Containers contract.ContainerWatcher
	Kinds      contract.KindLookup
	Admin      contract.AdminDomainSetting
	Deployer   contract.Deployer
	Certs      contract.CertificateReader
	Clock      contract.Clock
}

type serviceViewer struct {
	p   Parts
	log *slog.Logger
}

func NewServiceViewer(p Parts, log *slog.Logger) contract.ServiceViewer { return serviceViewer{p, log} }

// containers는 모든 컨테이너 상태다. Docker를 읽지 못하면 nil이다.
func (v serviceViewer) containers(ctx context.Context) model.ContainerStates {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	all, err := v.p.Containers.All(ctx)
	if err != nil {
		v.log.Warn("docker unreachable", "err", err)
		return nil
	}
	if all == nil {
		all = model.ContainerStates{}
	}
	return all
}

func (v serviceViewer) Home(ctx context.Context) (model.HomeView, error) {
	all, err := v.p.Services.List(ctx)
	if err != nil {
		return model.HomeView{}, err
	}
	type entry struct {
		s     model.Service
		tools contract.KindTools
	}
	var list []entry
	needDocker := false
	for _, s := range all {
		tools, ok := v.p.Kinds.Find(s.Kind)
		if !ok {
			continue
		}
		needDocker = needDocker || tools.Destination.Find(s).Container
		list = append(list, entry{s, tools})
	}
	var containers model.ContainerStates
	var h model.HomeView
	if needDocker {
		containers = v.containers(ctx)
		h.DockerDown = containers == nil
	}
	for _, e := range list {
		h.Cards = append(h.Cards, model.ServiceCard{
			ID: e.s.ID, Name: e.s.Name.String(), Kind: e.s.Kind, Domain: e.s.PrimaryDomain(),
			Status: e.tools.Status.Read(e.s, containers),
		})
	}
	return h, nil
}

func (v serviceViewer) Detail(ctx context.Context, id model.ServiceID) (model.ServiceView, error) {
	s, err := v.p.Services.Get(ctx, id)
	if err != nil {
		return model.ServiceView{}, err
	}
	tools, ok := v.p.Kinds.Find(s.Kind)
	if !ok {
		return model.ServiceView{}, model.ErrNotFound
	}
	to := tools.Destination.Find(s)
	var containers model.ContainerStates
	if to.Container {
		containers = v.containers(ctx)
	}
	sec, err := v.p.Secrets.Get(ctx, id)
	if err != nil {
		return model.ServiceView{}, err
	}
	view := model.ServiceView{
		Service: s, Status: tools.Status.Read(s, containers), Target: to.Address, Container: to.Container,
		Deployable: tools.Builder != nil, Deploying: v.p.Deployer.IsDeploying(id),
		Form: model.ServiceForm{
			Source: s.Source, Branch: s.Branch, BuildPath: s.BuildPath, Folder: s.Folder, External: s.External, Port: s.Port,
			AutoDeploy: s.AutoDeploy, EnvText: sec.Env.Text(), Volumes: sec.Volumes.Text(), HasToken: sec.GitToken != "",
		},
	}
	if to.Folder != "" {
		view.Target = to.Folder
	}
	certs, _ := v.p.Certs.Read(ctx) // 못 읽으면 "아직 없음"으로 보인다
	now := v.p.Clock.Now()
	for _, d := range s.Domains {
		dv := model.DomainView{Domain: d}
		if d.HTTPS {
			dv.Certificate = certs.For(d.Domain, now)
		}
		view.Domains = append(view.Domains, dv)
	}
	if view.Deployable {
		view.Deploys, _ = v.p.History.Recent(ctx, id, 10)
		view.LiveID = s.Live.LiveDeployment()
		view.Webhook.Secret = sec.WebhookSecret
		if domain, _ := v.p.Admin.Get(ctx); !domain.IsZero() {
			view.Webhook.URL = fmt.Sprintf("https://%s/hooks/%d", domain, id)
		}
	}
	return view, nil
}

func (v serviceViewer) Deployment(ctx context.Context, id model.ServiceID, did model.DeploymentID) (model.DeploymentView, error) {
	s, err := v.p.Services.Get(ctx, id)
	if err != nil {
		return model.DeploymentView{}, err
	}
	d, err := v.p.History.Get(ctx, did)
	if err != nil || d.ServiceID != id {
		return model.DeploymentView{}, model.ErrNotFound
	}
	return model.DeploymentView{Service: s, Deployment: d, Steps: stepsOf(d)}, nil
}

// stepsOf는 배포 기록의 "▶ 한국어 / English" 줄에서 단계를 읽는다.
func stepsOf(d model.Deployment) []model.StepView {
	var steps []model.StepView
	for _, line := range strings.Split(d.Log, "\n") {
		label, ok := strings.CutPrefix(line, "▶ ")
		if !ok {
			continue
		}
		ko, en, _ := strings.Cut(label, " / ")
		steps = append(steps, model.StepView{Ko: ko, En: en, State: "done"})
	}
	if n := len(steps); n > 0 {
		switch d.Status {
		case model.DeployRunning:
			steps[n-1].State = "doing"
		case model.DeployFailed:
			steps[n-1].State = "failed"
		}
	}
	return steps
}
