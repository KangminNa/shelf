package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/docker"
	"github.com/KangminNa/naru/internal/service"
	"github.com/KangminNa/naru/internal/source"
	"github.com/KangminNa/naru/internal/store"
)

// fakeDocker는 메모리 안의 Docker다. 컨테이너가 "듣는지"는 listens로 정한다.
type fakeDocker struct {
	mu         sync.Mutex
	containers map[string]*fakeContainer
	images     map[string]docker.Image // ref 또는 ID → 이미지
	builds     []string                // 빌드한 태그
	removedImg []string
	listens    func(name string) bool // nil이면 모두 듣는다
	exits      func(name string) bool // 시작하자마자 죽는가
}

type fakeContainer struct {
	spec    docker.Spec
	running bool
	exited  bool
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{containers: map[string]*fakeContainer{}, images: map[string]docker.Image{}}
}

func (f *fakeDocker) Build(_ context.Context, tarball io.Reader, tag, _ string, log io.Writer) (string, error) {
	io.Copy(io.Discard, tarball)
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "sha256:built-" + tag
	f.builds = append(f.builds, tag)
	f.images[id] = docker.Image{ID: id}
	f.images[tag] = docker.Image{ID: id}
	fmt.Fprintln(log, "Successfully built")
	return id, nil
}

func (f *fakeDocker) Pull(_ context.Context, ref string, _ io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.images[ref]; !ok {
		return errors.New("pull access denied")
	}
	return nil
}

func (f *fakeDocker) Image(_ context.Context, ref string) (docker.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	img, ok := f.images[ref]
	if !ok {
		return img, docker.ErrNotFound
	}
	return img, nil
}

func (f *fakeDocker) RemoveImage(_ context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removedImg = append(f.removedImg, ref)
	return nil
}

func (f *fakeDocker) Create(_ context.Context, s docker.Spec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.containers[s.Name]; ok {
		return "", errors.New("name in use")
	}
	f.containers[s.Name] = &fakeContainer{spec: s}
	return s.Name, nil
}

func (f *fakeDocker) Start(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[name]
	if !ok {
		return docker.ErrNotFound
	}
	if f.exits != nil && f.exits(name) {
		c.exited = true
		return nil
	}
	c.running = true
	return nil
}

func (f *fakeDocker) Stop(_ context.Context, name string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.containers[name]; ok {
		c.running = false
	}
	return nil
}

func (f *fakeDocker) Remove(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.containers[name]; !ok {
		return docker.ErrNotFound
	}
	delete(f.containers, name)
	return nil
}

func (f *fakeDocker) Inspect(_ context.Context, name string) (docker.Inspection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[name]
	if !ok {
		return docker.Inspection{}, docker.ErrNotFound
	}
	if c.exited {
		return docker.Inspection{Status: "exited", ExitCode: 1}, nil
	}
	return docker.Inspection{Running: c.running, Status: map[bool]string{true: "running", false: "created"}[c.running]}, nil
}

func (f *fakeDocker) Logs(context.Context, string, int) (string, error) {
	return "Error: Cannot find module 'express'\n", nil
}

func (f *fakeDocker) ByLabel(_ context.Context, key, value string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for n, c := range f.containers {
		if c.spec.Labels[key] == value {
			out = append(out, n)
		}
	}
	return out, nil
}

// dial은 컨테이너가 실행 중이고 듣고 있으면 성공한다.
func (f *fakeDocker) dial(_ context.Context, addr string) error {
	host, _, _ := net.SplitHostPort(addr)
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[host]
	if !ok || !c.running || (f.listens != nil && !f.listens(host)) {
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

type rig struct {
	t        *testing.T
	d        *Deployer
	docker   *fakeDocker
	services *service.Repo
	store    *Store
	kicks    int
	sites    string
	files    map[string]string // clone이 만들 파일
	commit   source.Commit
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &rig{t: t, docker: newFakeDocker(), services: service.NewRepo(st.DB), store: NewStore(st.DB), sites: filepath.Join(dir, "sites"),
		files: map[string]string{}, commit: source.Commit{Hash: "a1b2c3d4e5f6", Message: "fix: things"}}
	r.d = New(Config{WorkDir: filepath.Join(dir, "work"), SitesDir: r.sites, Network: "naru-net"}, r.services, r.store, r.docker,
		func() { r.kicks++ }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.d.dial = r.docker.dial
	r.d.readyTimeout = 300 * time.Millisecond
	r.d.pollEvery = 10 * time.Millisecond
	r.d.clone = func(_ context.Context, url, branch, token, dir string, log io.Writer) (source.Commit, error) {
		for name, body := range r.files {
			p := filepath.Join(dir, filepath.FromSlash(name))
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(body), 0o644)
		}
		os.MkdirAll(dir, 0o755)
		return r.commit, nil
	}
	return r
}

func (r *rig) deploy(id int64) Deployment {
	r.t.Helper()
	did, err := r.d.Deploy(id, "manual")
	if err != nil {
		r.t.Fatal(err)
	}
	r.d.Wait()
	dep, _ := r.store.Get(did)
	return dep
}

func (r *rig) get(id int64) service.Service {
	s, _ := r.services.Get(id)
	return s
}

func TestImageDeployDetectsThePortAndJoinsTheAlias(t *testing.T) {
	r := newRig(t)
	r.docker.images["traefik/whoami"] = docker.Image{ID: "sha256:who", Ports: []int{80}}
	id, _ := r.services.Create(service.Service{Name: "whoami", Kind: service.KindImage, Source: "traefik/whoami"}, service.Secrets{Env: "GREETING=hi\nbad line\n"})

	dep := r.deploy(id)
	if dep.Status != Success || dep.Image != "sha256:who" {
		t.Fatalf("%+v\n%s", dep, dep.Log)
	}
	s := r.get(id)
	if s.Port != 80 || s.Container != "naru-whoami" || s.Instance != fmt.Sprintf("naru-whoami-%d", dep.ID) {
		t.Fatalf("%+v", s)
	}
	c := r.docker.containers[s.Instance]
	if c.spec.Aliases[0] != "naru-whoami" || c.spec.Network != "naru-net" || c.spec.Labels[LabelService] != fmt.Sprint(id) {
		t.Fatalf("the new container answers to the service name: %+v", c.spec)
	}
	if len(c.spec.Env) != 1 || c.spec.Env[0] != "GREETING=hi" {
		t.Fatalf("env: %v", c.spec.Env)
	}
	if r.kicks == 0 {
		t.Fatal("the web server is told")
	}
}

func TestRedeployReplacesOnlyAfterTheNewOneAnswers(t *testing.T) {
	r := newRig(t)
	r.docker.images["traefik/whoami"] = docker.Image{ID: "sha256:who", Ports: []int{80}}
	id, _ := r.services.Create(service.Service{Name: "whoami", Kind: service.KindImage, Source: "traefik/whoami"}, service.Secrets{})
	first := r.deploy(id)
	second := r.deploy(id)
	if second.Status != Success {
		t.Fatal(second.Log)
	}
	names := r.docker.names()
	if len(names) != 1 || names[0] != fmt.Sprintf("naru-whoami-%d", second.ID) {
		t.Fatalf("only the new container remains: %v (first was %d)", names, first.ID)
	}
}

func TestAFailingNewVersionLeavesTheOldOneServing(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/app"] = docker.Image{ID: "sha256:v1", Ports: []int{3000}}
	id, _ := r.services.Create(service.Service{Name: "app", Kind: service.KindImage, Source: "me/app"}, service.Secrets{})
	good := r.deploy(id)
	oldName := r.get(id).Instance

	r.docker.exits = func(name string) bool { return name != oldName }
	bad := r.deploy(id)
	if bad.Status != Failed || !strings.Contains(bad.Log, "Cannot find module") || !strings.Contains(bad.Log, "종료 코드 1") {
		t.Fatalf("a crashing container fails the deploy and shows its output:\n%s", bad.Log)
	}
	names := r.docker.names()
	if len(names) != 1 || names[0] != oldName || r.get(id).Instance != oldName {
		t.Fatalf("the old version keeps serving: %v (good #%d)", names, good.ID)
	}

	r.docker.exits = nil
	r.docker.listens = func(name string) bool { return name == oldName }
	silent := r.deploy(id)
	if silent.Status != Failed || !strings.Contains(silent.Log, "포트 3000") {
		t.Fatalf("a container that never answers fails too:\n%s", silent.Log)
	}
	if r.get(id).Instance != oldName {
		t.Fatal("still the old one")
	}
}

func TestAdoptingAV1Container(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/blog"] = docker.Image{ID: "sha256:b", Ports: []int{3000}}
	r.docker.containers["shelf-blog"] = &fakeContainer{running: true} // v1이 띄운 것, 라벨 없음
	id, _ := r.services.Create(service.Service{Name: "blog", Kind: service.KindImage, Source: "me/blog", Container: "shelf-blog", Port: 3000}, service.Secrets{})

	dep := r.deploy(id)
	if dep.Status != Success {
		t.Fatal(dep.Log)
	}
	s := r.get(id)
	if s.Container != "shelf-blog" || s.Instance != fmt.Sprintf("shelf-blog-%d", dep.ID) {
		t.Fatalf("the service keeps answering as shelf-blog: %+v", s)
	}
	if _, ok := r.docker.containers["shelf-blog"]; ok {
		t.Fatal("the v1 container is retired once the new one answers")
	}
	if r.docker.containers[s.Instance].spec.Aliases[0] != "shelf-blog" {
		t.Fatal("alias keeps the web server's target unchanged")
	}
}

func TestRepoDeployBuildsWithThePortFromTheDockerfile(t *testing.T) {
	r := newRig(t)
	r.files = map[string]string{"web/Dockerfile": "FROM node:20\nEXPOSE 3000\n", "web/server.js": "x"}
	id, _ := r.services.Create(service.Service{Name: "blog", Kind: service.KindRepo, Source: "https://github.com/me/blog", BuildPath: "web"}, service.Secrets{})
	dep := r.deploy(id)
	if dep.Status != Success || dep.Commit != "a1b2c3d4e5f6" || dep.Message != "fix: things" {
		t.Fatalf("%+v\n%s", dep, dep.Log)
	}
	if len(r.docker.builds) != 1 || r.docker.builds[0] != ImageTag("blog", dep.ID) || r.get(id).Port != 3000 {
		t.Fatalf("builds=%v port=%d", r.docker.builds, r.get(id).Port)
	}

	r.files = map[string]string{"README.md": "no dockerfile here"}
	id2, _ := r.services.Create(service.Service{Name: "plain", Kind: service.KindRepo, Source: "https://github.com/me/plain"}, service.Secrets{})
	if dep := r.deploy(id2); dep.Status != Failed || !strings.Contains(dep.Log, "정적 사이트") {
		t.Fatalf("a repo without Dockerfile points to static sites:\n%s", dep.Log)
	}
}

func TestBuildImagesArePruned(t *testing.T) {
	r := newRig(t)
	r.files = map[string]string{"Dockerfile": "FROM x\nEXPOSE 80\n"}
	id, _ := r.services.Create(service.Service{Name: "blog", Kind: service.KindRepo, Source: "https://github.com/me/blog"}, service.Secrets{})
	var deps []Deployment
	for i := 0; i < 5; i++ {
		deps = append(deps, r.deploy(id))
	}
	removed := strings.Join(r.docker.removedImg, " ")
	if !strings.Contains(removed, ImageTag("blog", deps[0].ID)) || strings.Contains(removed, ImageTag("blog", deps[4].ID)) || strings.Contains(removed, ImageTag("blog", deps[2].ID)) {
		t.Fatalf("keep the last %d builds for rollback, drop older: %v", keepImages, r.docker.removedImg)
	}
}

func TestRollbackReusesTheOldImage(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/app"] = docker.Image{ID: "sha256:v1", Ports: []int{3000}}
	id, _ := r.services.Create(service.Service{Name: "app", Kind: service.KindImage, Source: "me/app"}, service.Secrets{})
	v1 := r.deploy(id)
	r.docker.images["me/app"] = docker.Image{ID: "sha256:v2", Ports: []int{3000}}
	r.docker.images["sha256:v1"] = docker.Image{ID: "sha256:v1"}
	r.deploy(id)

	did, err := r.d.Rollback(id, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.d.Wait()
	back, _ := r.store.Get(did)
	if back.Status != Success || back.Image != "sha256:v1" || back.Trigger != "rollback" {
		t.Fatalf("%+v\n%s", back, back.Log)
	}
	if r.docker.containers[r.get(id).Instance].spec.Image != "sha256:v1" {
		t.Fatal("the restored container runs the old image")
	}
	if _, err := r.d.Rollback(id, 9999); err != ErrCannotRollback {
		t.Fatal("unknown deployments cannot be restored")
	}
}

func TestStaticSitePublishesFilesWithoutAContainer(t *testing.T) {
	r := newRig(t)
	r.files = map[string]string{"site/index.html": "<h1>hi</h1>", "site/app.css": "body{}", "README.md": "x"}
	id, _ := r.services.Create(service.Service{Name: "landing", Kind: service.KindStatic, Source: "https://github.com/me/landing", Folder: "site"}, service.Secrets{})
	dep := r.deploy(id)
	if dep.Status != Success {
		t.Fatal(dep.Log)
	}
	s := r.get(id)
	if s.Release != fmt.Sprint(dep.ID) {
		t.Fatalf("release %q", s.Release)
	}
	if _, err := os.Stat(filepath.Join(r.sites, "landing", s.Release, "index.html")); err != nil {
		t.Fatal("files are published")
	}
	if _, err := os.Stat(filepath.Join(r.sites, "landing", s.Release, "README.md")); err == nil {
		t.Fatal("only the chosen folder is published")
	}
	if len(r.docker.names()) != 0 || r.kicks == 0 {
		t.Fatal("no container; the web server is told")
	}

	r.files = map[string]string{"site/about.html": "no index"}
	if bad := r.deploy(id); bad.Status != Failed || !strings.Contains(bad.Log, "index.html") {
		t.Fatalf("a folder without index.html is refused:\n%s", bad.Log)
	}
	if r.get(id).Release != s.Release {
		t.Fatal("a failed deploy keeps the live files")
	}

	did, _ := r.d.Rollback(id, dep.ID)
	r.d.Wait()
	back, _ := r.store.Get(did)
	if back.Status != Success || r.get(id).Release != fmt.Sprint(did) {
		t.Fatalf("static rollback publishes the old files as a new release: %+v", back)
	}
	if body, _ := os.ReadFile(filepath.Join(r.sites, "landing", fmt.Sprint(did), "index.html")); string(body) != "<h1>hi</h1>" {
		t.Fatal("the old files are what is live")
	}
	// 되돌린 배포에서 다시 되돌릴 수도 있다
	again, _ := r.d.Rollback(id, did)
	r.d.Wait()
	if d, _ := r.store.Get(again); d.Status != Success {
		t.Fatal("a restored release can be restored again")
	}
}

func TestADeployWhileBusyRunsOnceMoreAfterwards(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/app"] = docker.Image{ID: "sha256:v", Ports: []int{80}}
	id, _ := r.services.Create(service.Service{Name: "app", Kind: service.KindImage, Source: "me/app"}, service.Secrets{})
	gate := make(chan struct{})
	r.docker.listens = func(string) bool {
		<-gate
		return true
	}
	if _, err := r.d.Deploy(id, "webhook"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := r.d.Deploy(id, "webhook"); err != ErrBusy {
			t.Fatalf("got %v", err)
		}
	}
	close(gate)
	r.d.Wait()
	all, _ := r.store.Recent(id, 10)
	if len(all) != 2 {
		t.Fatalf("pushes during a deploy collapse into one more deploy, got %d deploys", len(all))
	}
}

func TestDeleteRemovesEverything(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/app"] = docker.Image{ID: "sha256:v", Ports: []int{80}}
	id, _ := r.services.Create(service.Service{Name: "app", Kind: service.KindImage, Source: "me/app"}, service.Secrets{})
	r.deploy(id)
	if err := r.d.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(r.docker.names()) != 0 {
		t.Fatal("containers are removed")
	}
	if _, err := r.services.Get(id); err != service.ErrNotFound {
		t.Fatal("the service is gone")
	}
}

func TestStopMarksTheServiceStopped(t *testing.T) {
	r := newRig(t)
	r.docker.images["me/app"] = docker.Image{ID: "sha256:v", Ports: []int{80}}
	id, _ := r.services.Create(service.Service{Name: "app", Kind: service.KindImage, Source: "me/app"}, service.Secrets{})
	r.deploy(id)
	r.d.Stop(context.Background(), id)
	if !r.get(id).Stopped || r.docker.containers[r.get(id).Instance].running {
		t.Fatal("stopped on purpose")
	}
	r.d.Start(context.Background(), id)
	if r.get(id).Stopped {
		t.Fatal("started again")
	}
}

func TestInterruptedDeploysAreClosed(t *testing.T) {
	r := newRig(t)
	id, _ := r.services.Create(service.Service{Name: "app", Kind: service.KindImage, Source: "me/app"}, service.Secrets{})
	did, _ := r.store.Begin(id, "manual")
	if n, _ := r.store.Interrupted("restarted"); n != 1 {
		t.Fatal("one closed")
	}
	if d, _ := r.store.Get(did); d.Status != Failed || !strings.Contains(d.Log, "restarted") {
		t.Fatalf("%+v", d)
	}
}

func TestEnvAndVolumes(t *testing.T) {
	env := ParseEnv("A=1\n# comment\n\nB = two words\n=nokey\nC=x=y\nbad\n")
	if strings.Join(env, "|") != "A=1|B= two words|C=x=y" {
		t.Fatalf("%q", env)
	}
	for _, ok := range []string{"/srv/data:/data", "/srv/data:/data:ro"} {
		if !ValidVolume(ok) {
			t.Errorf("%s", ok)
		}
	}
	for _, bad := range []string{"data:/data", "/a:/b:zz", "/a/../etc:/x", "/a"} {
		if ValidVolume(bad) {
			t.Errorf("%s should be refused", bad)
		}
	}
}
