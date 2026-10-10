// Package services는 서비스를 만들고·고치고, 주소를 붙이고 떼는 일을 맡는다.
// 종류마다 다른 것은 KindLookup이 준 담당자에게 묻는다 — 이 패키지는 종류 이름을 모른다.
package services

import (
	"context"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// ── 이름 · 주소 확인 ─────────────────────────

type nameChooser struct{ services contract.ServiceReader }

func NewNameChooser(services contract.ServiceReader) contract.NameChooser {
	return nameChooser{services}
}

// Choose는 적은 이름이 겹치지 않는지 본다. 비워 두면 주소·저장소·이미지·연결 주소에서 지어 주고, 겹치면 -2, -3을 붙인다.
func (c nameChooser) Choose(ctx context.Context, in model.ServiceInput) (model.ServiceName, error) {
	if !in.Name.IsZero() {
		if c.services.NameTaken(ctx, in.Name) {
			return in.Name, model.InputError{Field: "name", Code: "nametaken"}
		}
		return in.Name, nil
	}
	var from string
	switch {
	case !in.Domain.IsZero():
		from = in.Domain.FirstLabel()
	case !in.Repo.IsZero():
		from = in.Repo.Name()
	case !in.Image.IsZero():
		from = in.Image.Name()
	case !in.External.IsZero():
		from = in.External.Host()
	}
	base := model.SuggestServiceName(from)
	name := base
	for i := 2; c.services.NameTaken(ctx, name); i++ {
		name = base.WithSuffix(i)
	}
	return name, nil
}

type domainChecker struct {
	domains contract.DomainStore
	admin   contract.AdminDomainSetting
}

func NewDomainChecker(domains contract.DomainStore, admin contract.AdminDomainSetting) contract.DomainChecker {
	return domainChecker{domains, admin}
}

// Check는 주소가 관리 화면이나 다른 서비스와 겹치는지 본다.
func (c domainChecker) Check(ctx context.Context, d model.DomainName) error {
	if admin, _ := c.admin.Get(ctx); !admin.IsZero() && admin == d {
		return model.InputError{Field: "domain", Code: "admindomain"}
	}
	if _, taken := c.domains.FindOwner(ctx, d); taken {
		return model.InputError{Field: "domain", Code: "domaintaken"}
	}
	return nil
}

// ── 만들기 · 고치기 ─────────────────────────

type serviceEditor struct {
	reader  contract.ServiceReader
	store   contract.ServiceStore
	domains contract.DomainStore
	secrets contract.SecretStore
	names   contract.NameChooser
	checker contract.DomainChecker
	kinds   contract.KindLookup
	random  contract.RandomTokens
	events  contract.EventPublisher
}

func NewServiceEditor(reader contract.ServiceReader, store contract.ServiceStore, domains contract.DomainStore, secrets contract.SecretStore,
	names contract.NameChooser, checker contract.DomainChecker, kinds contract.KindLookup, random contract.RandomTokens, events contract.EventPublisher) contract.ServiceEditor {
	return serviceEditor{reader, store, domains, secrets, names, checker, kinds, random, events}
}

func (e serviceEditor) toolsFor(k model.KindName) (contract.KindTools, error) {
	tools, ok := e.kinds.Find(k)
	if !ok {
		return tools, model.InputError{Field: "kind", Code: "kind"}
	}
	return tools, nil
}

// Create는 서비스를 만든다. 웹훅 시크릿은 여기서 만든다. 첫 배포는 하지 않는다 (ServiceLauncher가 한다).
func (e serviceEditor) Create(ctx context.Context, in model.ServiceInput) (model.ServiceID, error) {
	tools, err := e.toolsFor(in.Kind)
	if err != nil {
		return 0, err
	}
	if in, err = tools.Input.Check(in); err != nil {
		return 0, err
	}
	name, err := e.names.Choose(ctx, in)
	if err != nil {
		return 0, err
	}
	if !in.Domain.IsZero() {
		if err := e.checker.Check(ctx, in.Domain); err != nil {
			return 0, err
		}
	}
	id, err := e.store.Create(ctx, model.NewService{
		Name: name, Kind: in.Kind, Source: in.Source(), Branch: in.Branch.String(), BuildPath: in.BuildPath.String(),
		Folder: in.Folder.String(), External: in.External.String(), Port: in.Port, AutoDeploy: true,
	})
	if err != nil {
		return 0, err
	}
	if err := e.secrets.Set(ctx, id, model.ServiceSecrets{
		Env: in.Env, Volumes: in.Volumes, GitToken: in.Token, WebhookSecret: e.random.New(24),
	}); err != nil {
		return id, err
	}
	if !in.Domain.IsZero() {
		if err := e.domains.Add(ctx, id, model.DomainInput{Domain: in.Domain, HTTPS: true}); err != nil {
			return id, err
		}
	}
	e.events.Publish(model.SiteMapChanged{Reason: "service created"})
	return id, nil
}

// Update는 설정을 고친다. 다시 배포해야 반영되는 것이 바뀌었으면 needsRedeploy가 참이다
// (저장소·브랜치·폴더·토큰, 컨테이너라면 환경 변수·볼륨 — 컨테이너는 만들 때의 환경으로 돈다).
func (e serviceEditor) Update(ctx context.Context, id model.ServiceID, in model.ServiceInput) (bool, error) {
	s, err := e.reader.Get(ctx, id)
	if err != nil {
		return false, err
	}
	in.Kind = s.Kind
	tools, err := e.toolsFor(s.Kind)
	if err != nil {
		return false, err
	}
	if in, err = tools.Input.Check(in); err != nil {
		return false, err
	}
	old, err := e.secrets.Get(ctx, id)
	if err != nil {
		return false, err
	}

	// 무엇이 바뀌었는지는 저장하기 전에 본다
	deployable, container := tools.Builder != nil, tools.Destination.Find(s).Container
	redeploy := deployable && (in.Source() != s.Source || in.Branch.String() != s.Branch ||
		in.BuildPath.String() != s.BuildPath || in.Folder.String() != s.Folder || in.Token != "")
	if container && (old.Env.Text() != in.Env.Text() || old.Volumes.Text() != in.Volumes.Text()) {
		redeploy = true
	}
	auto := in.AutoDeploy
	if !deployable {
		auto = s.AutoDeploy
	}

	if err := e.store.Update(ctx, id, model.ServiceSettings{
		Source: in.Source(), Branch: in.Branch.String(), BuildPath: in.BuildPath.String(), Folder: in.Folder.String(),
		External: in.External.String(), Port: in.Port, AutoDeploy: auto,
	}); err != nil {
		return false, err
	}
	next := old
	next.Env, next.Volumes = in.Env, in.Volumes
	if in.Token != "" {
		next.GitToken = in.Token
	} else if in.ClearToken {
		next.GitToken = ""
	}
	if err := e.secrets.Set(ctx, id, next); err != nil {
		return false, err
	}
	e.events.Publish(model.SiteMapChanged{Reason: "service settings"}) // 포트·연결 대상이 바뀌었을 수 있다
	return redeploy, nil
}

func (e serviceEditor) AddDomain(ctx context.Context, id model.ServiceID, d model.DomainInput) error {
	if _, err := e.reader.Get(ctx, id); err != nil {
		return err
	}
	if err := e.checker.Check(ctx, d.Domain); err != nil {
		return err
	}
	if err := e.domains.Add(ctx, id, d); err != nil {
		return err
	}
	e.events.Publish(model.SiteMapChanged{Reason: "domain added"})
	return nil
}

func (e serviceEditor) RemoveDomain(ctx context.Context, id model.ServiceID, d model.DomainID) error {
	if err := e.domains.Remove(ctx, id, d); err != nil {
		return err
	}
	e.events.Publish(model.SiteMapChanged{Reason: "domain removed"})
	return nil
}

// SetPort는 앱 포트만 바꾼다 — 진단의 [포트 바꾸기]. 다시 배포하지 않는다: 앱은 이미 그 포트로 듣고 있고, 웹서버만 따라가면 된다.
func (e serviceEditor) SetPort(ctx context.Context, id model.ServiceID, p model.Port) error {
	s, err := e.reader.Get(ctx, id)
	if err != nil {
		return err
	}
	tools, err := e.toolsFor(s.Kind)
	if err != nil {
		return err
	}
	if p == 0 || !tools.Destination.Find(s).Container {
		return model.InputError{Field: "port", Code: "port"}
	}
	if err := e.store.Update(ctx, id, model.ServiceSettings{
		Source: s.Source, Branch: s.Branch, BuildPath: s.BuildPath, Folder: s.Folder, External: s.External, Port: p, AutoDeploy: s.AutoDeploy,
	}); err != nil {
		return err
	}
	e.events.Publish(model.SiteMapChanged{Reason: "port changed"})
	return nil
}

// ── 만들고 바로 배포 ─────────────────────────

type serviceLauncher struct {
	editor   contract.ServiceEditor
	kinds    contract.KindLookup
	deployer contract.Deployer
}

func NewServiceLauncher(editor contract.ServiceEditor, kinds contract.KindLookup, deployer contract.Deployer) contract.ServiceLauncher {
	return serviceLauncher{editor, kinds, deployer}
}

// Launch는 서비스를 만들고, 배포가 있는 종류면 첫 배포를 시작한다.
// 만든 뒤 배포를 시작하지 못하면 서비스 번호와 함께 그 실패를 돌려준다.
func (l serviceLauncher) Launch(ctx context.Context, in model.ServiceInput) (model.ServiceID, model.DeploymentID, error) {
	id, err := l.editor.Create(ctx, in)
	if err != nil {
		return id, 0, err
	}
	if tools, ok := l.kinds.Find(in.Kind); !ok || tools.Builder == nil {
		return id, 0, nil
	}
	did, err := l.deployer.Deploy(ctx, id, model.ReasonCreate)
	return id, did, err
}
