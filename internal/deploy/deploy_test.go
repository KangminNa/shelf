package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/events"
	"github.com/KangminNa/naru/internal/files"
	"github.com/KangminNa/naru/internal/kinds"
	"github.com/KangminNa/naru/internal/model"
	"github.com/KangminNa/naru/internal/store"
)

var ctx = context.Background()

// fakeDocker는 메모리 안의 Docker다. 컨테이너가 "듣는지"는 listens로 정한다.
type fakeDocker struct {
	mu         sync.Mutex
	containers map[string]*fakeContainer
	images     map[string]model.ImageDetails // ref 또는 ID → 이미지
	builds     []string
	removedImg []string
	listens    func(name string) bool // nil이면 모두 듣는다
	exits      func(name string) bool // 시작하자마자 죽는가
}

type fakeContainer struct {
	spec    model.ContainerSpec
	running bool
	exited  bool
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{containers: map[string]*fakeContainer{}, images: map[string]model.ImageDetails{}}
}

func (f *fakeDocker) Build(_ context.Context, folder io.Reader, tag string, log io.Writer) (model.ImageID, error) {
	io.Copy(io.Discard, folder)
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "sha256:built-" + tag
	f.builds = append(f.builds, tag)
	f.images[id] = model.ImageDetails{ID: model.ImageID(id)}
	fmt.Fprintln(log, "Successfully built")
	return model.ImageID(id), nil
}

func (f *fakeDocker) Pull(_ context.Context, ref model.ImageRef, _ io.Writer) (model.ImageDetails, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	img, ok := f.images[ref.String()]
	if !ok {
		return img, errors.New("pull access denied")
	}
	f.images[string(img.ID)] = img
	return img, nil
}

func (f *fakeDocker) Exists(_ context.Context, id model.ImageID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.images[string(id)]
	return ok
}

func (f *fakeDocker) Remove(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(name, ":") {
		f.removedImg = append(f.removedImg, name)
		return nil
	}
	delete(f.containers, name)
	return nil
}

func (f *fakeDocker) Start(_ context.Context, s model.ContainerSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.containers[s.Name]; ok {
		return errors.New("name in use")
	}
	c := &fakeContainer{spec: s, running: true}
	if f.exits != nil && f.exits(s.Name) {
		c.running, c.exited = false, true
	}
	f.containers[s.Name] = c
	return nil
}

func (f *fakeDocker) TurnOff(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.containers[name]; ok {
		c.running = false
	}
	return nil
}

func (f *fakeDocker) TurnOn(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.containers[name]; ok {
		c.running = true
	}
	return nil
}

func (f *fakeDocker) All(context.Context) (model.ContainerStates, error) {
	return model.ContainerStates{}, nil
}

func (f *fakeDocker) One(_ context.Context, name string) (model.ContainerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[name]
	if !ok {
		return model.ContainerState{}, errors.New("no such container")
	}
	if c.exited {
		return model.ContainerState{Name: name, State: "exited", ExitCode: 1}, nil
	}
	return model.ContainerState{Name: name, Running: c.running, State: map[bool]string{true: "running", false: "created"}[c.running]}, nil
}

func (f *fakeDocker) Logs(context.Context, string, int) (string, error) {
	return "Error: Cannot find module 'express'\n", nil
}

func (f *fakeDocker) BelongingTo(_ context.Context, id model.ServiceID) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for n, c := range f.containers {
		if c.spec.Service == id {
			out = append(out, n)
		}
	}
	return out, nil
}

// Answers는 컨테이너가 실행 중이고 듣고 있으면 성공한다.
func (f *fakeDocker) Answers(_ context.Context, host string, _ model.Port) error {
	f.mu.Lock()
	c, ok := f.containers[host]
	listens := f.listens
	f.mu.Unlock()
	if !ok || !c.running || (listens != nil && !listens(host)) {
		return errors.New("connection refused")
	}
	return nil
}

func (f *fakeDocker) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for n := range f.containers {
		out = append(out, n)
	}
	return out
}

// fakeGit은 정해 둔 파일을 "내려받는다".
type fakeGit struct {
	files  map[string]string
	commit model.Commit
}

func (g *fakeGit) Download(_ context.Context, _ model.CodeSource, into string, _ io.Writer) (model.Commit, error) {
	os.MkdirAll(into, 0o755)
	for name, body := range g.files {
		p := filepath.Join(into, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	return g.commit, nil
}

type rig struct {
	t        *testing.T
	d        contract.Deployer
	wait     func()
	control  contract.ServiceControl
	docker   *fakeDocker
	git      *fakeGit
	db       *store.DB
	services store.Services
	history  store.Deployments
	sites    string
	changes  int
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	r := &rig{t: t, docker: newFakeDocker(), db: db, services: store.NewServices(db), history: store.NewDeployments(db),
		sites: filepath.Join(dir, "sites"), git: &fakeGit{files: map[string]string{}, commit: model.Commit{Hash: "a1b2c3d4e5f6", Message: "fix: things"}}}
	bus := events.NewBus()
	bus.Subscribe(func(e model.Event) {
		if _, ok := e.(model.SiteMapChanged); ok {
			r.changes++
		}
	})
	siteFiles := files.NewSiteFolders(r.sites)
	secrets := store.NewSecrets(db)
	lookup := kinds.NewLookup(kinds.Tools{
		Work: files.NewTempFolders(filepath.Join(dir, "work")), Code: r.git, Secrets: secrets,
		Dockerfile: files.DockerfileReader{}, Packer: files.TarPacker{}, Builder: r.docker, Puller: r.docker, Images: r.docker,
		Starter: r.docker, Remover: r.docker, Switch: r.docker, Watcher: r.docker, Ports: r.docker, Files: siteFiles,
		Network: "naru-net", ReadyTimeout: 300 * time.Millisecond, PollEvery: 10 * time.Millisecond,
	})
	lock := NewMemoryDeployLock()
	r.d, r.wait = NewDeployer(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), Parts{
		Lock: lock, History: r.history, Past: r.history, Logs: NewDeployLog(r.history), Services: r.services,
		Live: store.NewLiveStates(db), Kinds: lookup, Cleaner: NewOldVersionCleaner(r.history, r.docker, siteFiles), Events: bus,
	})
	r.control = NewServiceControl(ControlParts{
		Services: r.services, Store: r.services, Live: store.NewLiveStates(db), Lock: lock, Switch: r.docker, Remover: r.docker,
		Watcher: r.docker, Images: r.docker, Files: siteFiles, Past: r.history, Events: bus,
	})
	return r
}

func (r *rig) create(s model.NewService, env string) model.ServiceID {
	r.t.Helper()
	id, err := r.services.Create(ctx, s)
	if err != nil {
		r.t.Fatal(err)
	}
	e, err := model.ParseEnvVars(env)
	if err != nil {
		r.t.Fatal(err)
	}
	store.NewSecrets(r.db).Set(ctx, id, model.ServiceSecrets{Env: e})
	return id
}

func (r *rig) deploy(id model.ServiceID) model.Deployment {
	r.t.Helper()
	did, err := r.d.Deploy(ctx, id, model.ReasonManual)
	if err != nil {
		r.t.Fatal(err)
	}
	r.wait()
	dep, _ := r.history.Get(ctx, did)
	return dep
}

func (r *rig) get(id model.ServiceID) model.Service {
	s, _ := r.services.Get(ctx, id)
	return s
}

func name(s string) model.ServiceName {
	n, _ := model.ParseServiceName(s)
	return n
}

func TestImageDeployDetectsThePortAndJoinsTheAlias(t *testing.T) {
	r := newRig(t)
	r.docker.images["traefik/whoami"] = model.ImageDetails{ID: "sha256:who", Ports: []model.Port{80}}
	id := r.create(model.NewService{Name: name("whoami"), Kind: model.KindImage, Source: "traefik/whoami"}, "GREETING=hi\n# note\n")

	dep := r.deploy(id)
	if dep.Status != model.DeploySuccess || dep.Image != "sha256:who" {
		t.Fatalf("%+v\n%s", dep, dep.Log)
	}
	s := r.get(id)
	if s.Port != 80 || s.Live.Alias != "naru-whoami" || s.Live.Instance != fmt.Sprintf("naru-whoami-%d", dep.ID) {
		t.Fatalf("%+v", s)
	}
	c := r.docker.containers[s.Live.Instance]
	if c.spec.Aliases[0] != "naru-whoami" || c.spec.Network != "naru-net" || c.spec.Service != id {
		t.Fatalf("the new container answers to the service name: %+v", c.spec)
	}
	if len(c.spec.Env) != 1 || c.spec.Env[0] != "GREETING=hi" {
		t.Fatalf("env: %v", c.spec.Env)
	}
	if r.changes == 0 {
		t.Fatal("the web server is told")
	}
}

func TestRedeployReplacesOnlyAfterTheNewOneAnswers(t *testing.T) {
	r := newRig(t)
	r.docker.images["traefik/whoami"] = model.ImageDetails{ID: "sha256:who", Ports: []model.Port{80}}
	id := r.create(model.NewService{Name: name("whoami"), Kind: model.KindImage, Source: "traefik/whoami"}, "")
	first := r.deploy(id)
	second := r.deploy(id)
	if second.Status != model.DeploySuccess {
		t.Fatal(second.Log)
	}
	names := r.docker.names()
	if len(names) != 1 || names[0] != fmt.Sprintf("naru-whoami-%d", second.ID) {
		t.Fatalf("only the new container remains: %v (first was %d)", names, first.ID)
	}
}

func TestAFailingNewVersionLeavesTheOldOneServing(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/app"] = model.ImageDetails{ID: "sha256:v1", Ports: []model.Port{3000}}
	id := r.create(model.NewService{Name: name("app"), Kind: model.KindImage, Source: "me/app"}, "")
	good := r.deploy(id)
	oldName := r.get(id).Live.Instance

	r.docker.exits = func(name string) bool { return name != oldName }
	bad := r.deploy(id)
	if bad.Status != model.DeployFailed || !strings.Contains(bad.Log, "Cannot find module") || !strings.Contains(bad.Log, "종료 코드 1") {
		t.Fatalf("a crashing container fails the deploy and shows its output:\n%s", bad.Log)
	}
	names := r.docker.names()
	if len(names) != 1 || names[0] != oldName || r.get(id).Live.Instance != oldName {
		t.Fatalf("the old version keeps serving: %v (good #%d)", names, good.ID)
	}

	r.docker.exits = nil
	r.docker.listens = func(name string) bool { return name == oldName }
	silent := r.deploy(id)
	if silent.Status != model.DeployFailed || !strings.Contains(silent.Log, "포트 3000") {
		t.Fatalf("a container that never answers fails too:\n%s", silent.Log)
	}
	if r.get(id).Live.Instance != oldName {
		t.Fatal("still the old one")
	}
}

func TestAdoptingAV1Container(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/blog"] = model.ImageDetails{ID: "sha256:b", Ports: []model.Port{3000}}
	r.docker.containers["shelf-blog"] = &fakeContainer{running: true} // v1이 띄운 것, 라벨 없음
	id := r.create(model.NewService{Name: name("blog"), Kind: model.KindImage, Source: "me/blog", Alias: "shelf-blog", Port: 3000}, "")

	dep := r.deploy(id)
	if dep.Status != model.DeploySuccess {
		t.Fatal(dep.Log)
	}
	s := r.get(id)
	if s.Live.Alias != "shelf-blog" || s.Live.Instance != fmt.Sprintf("shelf-blog-%d", dep.ID) {
		t.Fatalf("the service keeps answering as shelf-blog: %+v", s)
	}
	if _, ok := r.docker.containers["shelf-blog"]; ok {
		t.Fatal("the v1 container is retired once the new one answers")
	}
	if r.docker.containers[s.Live.Instance].spec.Aliases[0] != "shelf-blog" {
		t.Fatal("alias keeps the web server's target unchanged")
	}
}

func TestRepoDeployBuildsWithThePortFromTheDockerfile(t *testing.T) {
	r := newRig(t)
	r.git.files = map[string]string{"web/Dockerfile": "FROM node:20\nEXPOSE 3000\n", "web/server.js": "x"}
	id := r.create(model.NewService{Name: name("blog"), Kind: model.KindRepo, Source: "https://github.com/me/blog", Branch: "main", BuildPath: "web"}, "")
	dep := r.deploy(id)
	if dep.Status != model.DeploySuccess || dep.Commit != "a1b2c3d4e5f6" || dep.Message != "fix: things" {
		t.Fatalf("%+v\n%s", dep, dep.Log)
	}
	if len(r.docker.builds) != 1 || r.docker.builds[0] != model.ImageTag(name("blog"), dep.ID) || r.get(id).Port != 3000 {
		t.Fatalf("builds=%v port=%d", r.docker.builds, r.get(id).Port)
	}

	r.git.files = map[string]string{"README.md": "no dockerfile here"}
	id2 := r.create(model.NewService{Name: name("plain"), Kind: model.KindRepo, Source: "https://github.com/me/plain"}, "")
	if dep := r.deploy(id2); dep.Status != model.DeployFailed || !strings.Contains(dep.Log, "정적 사이트") {
		t.Fatalf("a repo without Dockerfile points to static sites:\n%s", dep.Log)
	}
}

func TestBuildImagesArePruned(t *testing.T) {
	r := newRig(t)
	r.git.files = map[string]string{"Dockerfile": "FROM x\nEXPOSE 80\n"}
	id := r.create(model.NewService{Name: name("blog"), Kind: model.KindRepo, Source: "https://github.com/me/blog"}, "")
	var deps []model.Deployment
	for i := 0; i < 5; i++ {
		deps = append(deps, r.deploy(id))
	}
	removed := strings.Join(r.docker.removedImg, " ")
	tag := func(i int) string { return model.ImageTag(name("blog"), deps[i].ID) + " " }
	removed += " "
	if !strings.Contains(removed, tag(0)) || !strings.Contains(removed, tag(1)) || strings.Contains(removed, tag(2)) || strings.Contains(removed, tag(4)) {
		t.Fatalf("keep the last %d builds for rollback, drop older: %v", keepImages, r.docker.removedImg)
	}
}

func TestRollbackReusesTheOldImage(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/app"] = model.ImageDetails{ID: "sha256:v1", Ports: []model.Port{3000}}
	id := r.create(model.NewService{Name: name("app"), Kind: model.KindImage, Source: "me/app"}, "")
	v1 := r.deploy(id)
	r.docker.images["me/app"] = model.ImageDetails{ID: "sha256:v2", Ports: []model.Port{3000}}
	r.deploy(id)

	did, err := r.d.RollBack(ctx, id, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.wait()
	back, _ := r.history.Get(ctx, did)
	if back.Status != model.DeploySuccess || back.Image != "sha256:v1" || back.Reason != model.ReasonRollBack {
		t.Fatalf("%+v\n%s", back, back.Log)
	}
	if r.docker.containers[r.get(id).Live.Instance].spec.Image != "sha256:v1" {
		t.Fatal("the restored container runs the old image")
	}
	if _, err := r.d.RollBack(ctx, id, 9999); !errors.Is(err, model.ErrCannotRollBack) {
		t.Fatal("unknown deployments cannot be restored")
	}

	delete(r.docker.images, "sha256:v1")
	did, _ = r.d.RollBack(ctx, id, v1.ID)
	r.wait()
	if gone, _ := r.history.Get(ctx, did); gone.Status != model.DeployFailed || !strings.Contains(gone.Log, "이미지가 지워졌어요") {
		t.Fatalf("a deleted image cannot come back:\n%s", gone.Log)
	}
}

func TestStaticSitePublishesFilesWithoutAContainer(t *testing.T) {
	r := newRig(t)
	r.git.files = map[string]string{"site/index.html": "<h1>hi</h1>", "site/app.css": "body{}", "README.md": "x"}
	id := r.create(model.NewService{Name: name("landing"), Kind: model.KindStatic, Source: "https://github.com/me/landing", Folder: "site"}, "")
	dep := r.deploy(id)
	if dep.Status != model.DeploySuccess {
		t.Fatal(dep.Log)
	}
	s := r.get(id)
	if s.Live.Release != fmt.Sprint(dep.ID) {
		t.Fatalf("release %q", s.Live.Release)
	}
	if _, err := os.Stat(filepath.Join(r.sites, "landing", s.Live.Release, "index.html")); err != nil {
		t.Fatal("files are published")
	}
	if _, err := os.Stat(filepath.Join(r.sites, "landing", s.Live.Release, "README.md")); err == nil {
		t.Fatal("only the chosen folder is published")
	}
	if len(r.docker.names()) != 0 || r.changes == 0 {
		t.Fatal("no container; the web server is told")
	}

	r.git.files = map[string]string{"site/about.html": "no index"}
	if bad := r.deploy(id); bad.Status != model.DeployFailed || !strings.Contains(bad.Log, "index.html") {
		t.Fatalf("a folder without index.html is refused:\n%s", bad.Log)
	}
	if r.get(id).Live.Release != s.Live.Release {
		t.Fatal("a failed deploy keeps the live files")
	}

	did, _ := r.d.RollBack(ctx, id, dep.ID)
	r.wait()
	back, _ := r.history.Get(ctx, did)
	if back.Status != model.DeploySuccess || r.get(id).Live.Release != fmt.Sprint(did) || back.Commit != "a1b2c3d4e5f6" {
		t.Fatalf("static rollback publishes the old files as a new release: %+v\n%s", back, back.Log)
	}
	if body, _ := os.ReadFile(filepath.Join(r.sites, "landing", fmt.Sprint(did), "index.html")); string(body) != "<h1>hi</h1>" {
		t.Fatal("the old files are what is live")
	}
	// 되돌린 배포에서 다시 되돌릴 수도 있다
	again, _ := r.d.RollBack(ctx, id, did)
	r.wait()
	if d, _ := r.history.Get(ctx, again); d.Status != model.DeploySuccess {
		t.Fatal("a restored release can be restored again")
	}
}

func TestADeployWhileBusyRunsOnceMoreAfterwards(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/app"] = model.ImageDetails{ID: "sha256:v", Ports: []model.Port{80}}
	id := r.create(model.NewService{Name: name("app"), Kind: model.KindImage, Source: "me/app"}, "")
	gate := make(chan struct{})
	r.docker.listens = func(string) bool {
		<-gate
		return true
	}
	if _, err := r.d.Deploy(ctx, id, model.ReasonPush); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := r.d.Deploy(ctx, id, model.ReasonPush); !errors.Is(err, model.ErrQueued) {
			t.Fatalf("got %v", err)
		}
	}
	if !r.d.IsDeploying(id) {
		t.Fatal("deploying")
	}
	if _, err := r.d.RollBack(ctx, id, 1); !errors.Is(err, model.ErrCannotRollBack) && !errors.Is(err, model.ErrBusy) {
		t.Fatalf("got %v", err)
	}
	if err := r.control.Remove(ctx, id); !errors.Is(err, model.ErrBusy) {
		t.Fatal("a service is not removed while deploying")
	}
	close(gate)
	r.wait()
	all, _ := r.history.Recent(ctx, id, 10)
	if len(all) != 2 {
		t.Fatalf("pushes during a deploy collapse into one more deploy, got %d deploys", len(all))
	}
}

func TestExternalServicesHaveNothingToDeploy(t *testing.T) {
	r := newRig(t)
	id := r.create(model.NewService{Name: name("nas"), Kind: model.KindExternal, External: "host.docker.internal:5000"}, "")
	if _, err := r.d.Deploy(ctx, id, model.ReasonManual); !errors.Is(err, model.ErrNothingToDeploy) {
		t.Fatalf("got %v", err)
	}
}

func TestRemoveDeletesEverything(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/app"] = model.ImageDetails{ID: "sha256:v", Ports: []model.Port{80}}
	id := r.create(model.NewService{Name: name("app"), Kind: model.KindImage, Source: "me/app"}, "")
	r.deploy(id)
	if err := r.control.Remove(ctx, id); err != nil {
		t.Fatal(err)
	}
	if len(r.docker.names()) != 0 {
		t.Fatal("containers are removed")
	}
	if _, err := r.services.Get(ctx, id); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("the service is gone")
	}
}

func TestStopMarksTheServiceStopped(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/app"] = model.ImageDetails{ID: "sha256:v", Ports: []model.Port{80}}
	id := r.create(model.NewService{Name: name("app"), Kind: model.KindImage, Source: "me/app"}, "")
	r.deploy(id)
	r.control.Stop(ctx, id)
	if s := r.get(id); !s.Live.Stopped || r.docker.containers[s.Live.Instance].running || s.Port != 80 {
		t.Fatalf("stopped on purpose: %+v", s)
	}
	r.control.Start(ctx, id)
	if r.get(id).Live.Stopped {
		t.Fatal("started again")
	}
}

func TestEnvAndVolumes(t *testing.T) {
	if _, err := model.ParseEnvVars("A=1\nbad\n"); err == nil {
		t.Fatal("a line without = is refused on input")
	}
	env := model.StoredEnvVars("A=1\n# comment\n\nB = two words\n=nokey\nC=x=y\nbad\n").List()
	if strings.Join(env, "|") != "A=1|B= two words|C=x=y" {
		t.Fatalf("stored text keeps the good lines: %q", env)
	}
	for _, ok := range []string{"/srv/data:/data", "/srv/data:/data:ro"} {
		if _, err := model.ParseVolumes(ok); err != nil {
			t.Errorf("%s", ok)
		}
	}
	for _, bad := range []string{"data:/data", "/a:/b:zz", "/a/../etc:/x", "/a"} {
		if _, err := model.ParseVolumes(bad); err == nil {
			t.Errorf("%s should be refused", bad)
		}
	}
}
