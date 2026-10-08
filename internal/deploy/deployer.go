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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KangminNa/naru/internal/docker"
	"github.com/KangminNa/naru/internal/service"
	"github.com/KangminNa/naru/internal/source"
)

// Docker는 배포가 쓰는 Docker 기능이다. 테스트에서 가짜로 바꾼다.
type Docker interface {
	Build(ctx context.Context, tarball io.Reader, tag, dockerfile string, log io.Writer) (string, error)
	Pull(ctx context.Context, ref string, log io.Writer) error
	Image(ctx context.Context, ref string) (docker.Image, error)
	RemoveImage(ctx context.Context, ref string) error
	Create(ctx context.Context, s docker.Spec) (string, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string, grace time.Duration) error
	Remove(ctx context.Context, name string) error
	Inspect(ctx context.Context, name string) (docker.Inspection, error)
	Logs(ctx context.Context, name string, n int) (string, error)
	ByLabel(ctx context.Context, key, value string) ([]string, error)
}

// 컨테이너에 붙이는 라벨 — 어느 서비스의 몇 번째 배포인지
const (
	LabelService    = "naru.service"
	LabelDeployment = "naru.deployment"
)

const (
	keepImages   = 3 // 되돌리기용으로 남겨 둘 빌드 이미지 수
	keepReleases = 5 // 정적 사이트 배포본
	stopGrace    = 10 * time.Second
)

var (
	ErrBusy           = errors.New("a deploy is already running — this one will run right after")
	ErrNotDeployable  = errors.New("this service has nothing to deploy")
	ErrCannotRollback = errors.New("that deployment cannot be restored")
)

type Config struct {
	WorkDir  string // clone 자리 (끝나면 지운다)
	SitesDir string // 정적 사이트 배포본
	Network  string // 앱 컨테이너가 붙는 네트워크
}

type Deployer struct {
	cfg      Config
	services *service.Repo
	store    *Store
	docker   Docker
	kick     func() // 웹서버 설정을 다시 그리게 한다
	log      *slog.Logger

	// 바꿔 끼울 수 있는 것 (테스트)
	clone        func(ctx context.Context, url, branch, token, dir string, log io.Writer) (source.Commit, error)
	dial         func(ctx context.Context, addr string) error
	readyTimeout time.Duration
	pollEvery    time.Duration

	base  context.Context
	mu    sync.Mutex
	busy  map[int64]bool
	again map[int64]string
	wg    sync.WaitGroup
}

func New(cfg Config, services *service.Repo, store *Store, d Docker, kick func(), log *slog.Logger) *Deployer {
	return &Deployer{
		cfg: cfg, services: services, store: store, docker: d, kick: kick, log: log,
		clone: source.Clone,
		dial: func(ctx context.Context, addr string) error {
			var dl net.Dialer
			c, err := dl.DialContext(ctx, "tcp", addr)
			if err == nil {
				c.Close()
			}
			return err
		},
		readyTimeout: 60 * time.Second,
		pollEvery:    500 * time.Millisecond,
		base:         context.Background(),
		busy:         map[int64]bool{},
		again:        map[int64]string{},
	}
}

// Bind는 배포가 따를 수명(ctx)을 정한다. Naru가 꺼지면 진행 중인 배포도 멈춘다.
func (d *Deployer) Bind(ctx context.Context) { d.base = ctx }

// Wait는 진행 중인 배포가 모두 끝나기를 기다린다 (테스트·종료용).
func (d *Deployer) Wait() { d.wg.Wait() }

// Running은 그 서비스가 지금 배포 중인가.
func (d *Deployer) Running(serviceID int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.busy[serviceID]
}

// Deploy는 배포를 시작하고 그 번호를 돌려준다. 이미 배포 중이면 끝난 뒤 한 번 더 한다 (ErrBusy).
func (d *Deployer) Deploy(serviceID int64, trigger string) (int64, error) {
	return d.start(serviceID, trigger, nil)
}

// Rollback은 예전 배포(from)를 다시 올린다. 빌드하지 않고 그때의 이미지·파일을 그대로 쓴다.
func (d *Deployer) Rollback(serviceID, from int64) (int64, error) {
	prev, err := d.store.Get(from)
	if err != nil || prev.ServiceID != serviceID || prev.Status != Success {
		return 0, ErrCannotRollback
	}
	return d.start(serviceID, "rollback", &prev)
}

func (d *Deployer) start(serviceID int64, trigger string, from *Deployment) (int64, error) {
	svc, err := d.services.Get(serviceID)
	if err != nil {
		return 0, err
	}
	if !svc.Deployable() {
		return 0, ErrNotDeployable
	}
	d.mu.Lock()
	if d.busy[serviceID] {
		if from == nil {
			d.again[serviceID] = trigger
		}
		d.mu.Unlock()
		return 0, ErrBusy
	}
	d.busy[serviceID] = true
	d.mu.Unlock()

	id, err := d.store.Begin(serviceID, trigger)
	if err != nil {
		d.release(serviceID)
		return 0, err
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.run(svc, id, from)
		if next := d.release(serviceID); next != "" {
			d.Deploy(serviceID, next)
		}
	}()
	return id, nil
}

func (d *Deployer) release(serviceID int64) (again string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.busy, serviceID)
	again = d.again[serviceID]
	delete(d.again, serviceID)
	return again
}

// outcome은 배포 하나의 결과다.
type outcome struct {
	commit, message, image string
}

func (d *Deployer) run(svc service.Service, id int64, from *Deployment) {
	ctx, cancel := context.WithTimeout(d.base, 30*time.Minute)
	defer cancel()
	lw := newLogWriter(func(s string) { d.store.SaveLog(id, s) })
	started := time.Now()

	var out outcome
	var err error
	switch {
	case svc.Kind == service.KindStatic:
		out, err = d.deployStatic(ctx, svc, id, from, lw)
	default:
		out, err = d.deployContainer(ctx, svc, id, from, lw)
	}

	result := Deployment{ID: id, Commit: out.commit, Message: out.message, Image: out.image}
	if err != nil {
		fmt.Fprintf(lw, "\n✗ %s\n", err)
		result.Status = Failed
		d.log.Warn("deploy failed", "service", svc.Name, "deployment", id, "err", err)
	} else {
		fmt.Fprintf(lw, "\n✓ %s (%s)\n", "완료 / done", time.Since(started).Round(time.Second))
		result.Status = Success
		d.log.Info("deployed", "service", svc.Name, "deployment", id, "took", time.Since(started).Round(time.Second))
	}
	result.Log = lw.close()
	if from != nil && result.Commit == "" {
		result.Commit, result.Message = from.Commit, from.Message
	}
	if ferr := d.store.Finish(result); ferr != nil {
		d.log.Error("record deploy", "err", ferr)
	}
}

func step(w io.Writer, ko, en string) { fmt.Fprintf(w, "\n▶ %s / %s\n", ko, en) }

// ── 컨테이너 (저장소·이미지) ─────────────────────

func (d *Deployer) deployContainer(ctx context.Context, svc service.Service, id int64, from *Deployment, lw io.Writer) (outcome, error) {
	var out outcome
	port := svc.Port
	base := svc.Container
	if base == "" {
		base = "naru-" + svc.Name
		if err := d.services.SetContainer(svc.ID, base); err != nil {
			return out, err
		}
	}

	switch {
	case from != nil:
		step(lw, fmt.Sprintf("#%d 배포를 다시 올립니다", from.ID), fmt.Sprintf("restoring deployment #%d", from.ID))
		if from.Image == "" {
			return out, ErrCannotRollback
		}
		if _, err := d.docker.Image(ctx, from.Image); err != nil {
			return out, fmt.Errorf("그때의 이미지가 지워졌어요 — 다시 배포하세요 / that image is gone, deploy again: %w", err)
		}
		out.image = from.Image

	case svc.Kind == service.KindRepo:
		secrets, err := d.services.Secrets(svc.ID)
		if err != nil {
			return out, err
		}
		dir := filepath.Join(d.cfg.WorkDir, fmt.Sprintf("%s-%d", svc.Name, id))
		defer os.RemoveAll(dir)
		step(lw, "코드 가져오기", "fetching the code")
		commit, err := d.clone(ctx, svc.Source, svc.Branch, secrets.GitToken, dir, lw)
		if err != nil {
			return out, err
		}
		out.commit, out.message = commit.Hash, commit.Message
		fmt.Fprintf(lw, "%s %s\n", shortHash(commit.Hash), commit.Message)

		buildDir, err := source.Within(dir, svc.BuildPath)
		if err != nil {
			return out, fmt.Errorf("빌드 경로 %q를 찾을 수 없어요 / build path not found: %w", svc.BuildPath, err)
		}
		dockerfile := filepath.Join(buildDir, "Dockerfile")
		if _, err := os.Stat(dockerfile); err != nil {
			return out, errors.New("Dockerfile이 없어요. HTML 파일만 있는 저장소라면 '정적 사이트'로 올리세요 / no Dockerfile — for plain HTML use a static site")
		}
		if port == 0 {
			port = only(source.ExposedPorts(dockerfile))
		}
		step(lw, "이미지 만들기", "building the image")
		tarball := source.Tar(buildDir)
		imageID, err := d.docker.Build(ctx, tarball, ImageTag(svc.Name, id), "", lw)
		tarball.Close()
		if err != nil {
			return out, err
		}
		out.image = imageID

	case svc.Kind == service.KindImage:
		step(lw, "이미지 받기", "pulling the image")
		if err := d.docker.Pull(ctx, svc.Source, lw); err != nil {
			return out, err
		}
		img, err := d.docker.Image(ctx, svc.Source)
		if err != nil {
			return out, err
		}
		out.image = img.ID
		if port == 0 {
			port = only(img.Ports)
		}
	}

	if port == 0 {
		return out, errors.New("앱이 어느 포트를 듣는지 몰라요. Dockerfile에 EXPOSE를 쓰거나 서비스 설정에서 앱 포트를 적어 주세요 / unknown app port — add EXPOSE or set the port")
	}
	if svc.Port == 0 {
		fmt.Fprintf(lw, "앱 포트 %d (이미지에서 찾음) / app port %d (from the image)\n", port, port)
		if err := d.services.SetPort(svc.ID, port); err != nil {
			return out, err
		}
	}

	secrets, err := d.services.Secrets(svc.ID)
	if err != nil {
		return out, err
	}
	name := fmt.Sprintf("%s-%d", base, id)
	step(lw, "새 컨테이너 시작", "starting the new container")
	if _, err := d.docker.Create(ctx, docker.Spec{
		Name: name, Image: out.image, Env: ParseEnv(secrets.Env), Binds: ParseVolumes(secrets.Volumes),
		Labels:  map[string]string{LabelService: strconv.FormatInt(svc.ID, 10), LabelDeployment: strconv.FormatInt(id, 10)},
		Network: d.cfg.Network, Aliases: []string{base},
	}); err != nil {
		return out, err
	}
	if err := d.docker.Start(ctx, name); err != nil {
		d.docker.Remove(context.Background(), name)
		return out, err
	}

	step(lw, fmt.Sprintf("포트 %d 응답 기다리기", port), fmt.Sprintf("waiting for port %d", port))
	if err := d.waitReady(ctx, name, port); err != nil {
		if logs, lerr := d.docker.Logs(context.Background(), name, 30); lerr == nil && strings.TrimSpace(logs) != "" {
			fmt.Fprintf(lw, "— 컨테이너 출력 마지막 30줄 / last 30 lines —\n%s\n", logs)
		}
		d.docker.Remove(context.Background(), name)
		return out, fmt.Errorf("%w — 지금 돌던 것은 그대로 둡니다 / the running version is untouched", err)
	}
	fmt.Fprintln(lw, "응답함 / answering")

	// 새 컨테이너가 같은 별칭으로 요청을 받기 시작했으니 옛 것을 내린다
	step(lw, "옛 컨테이너 내리기", "retiring the old container")
	for _, old := range d.olderInstances(ctx, svc, name) {
		d.docker.Stop(ctx, old, stopGrace)
		if err := d.docker.Remove(ctx, old); err != nil && !errors.Is(err, docker.ErrNotFound) {
			fmt.Fprintf(lw, "%s: %v\n", old, err)
		} else {
			fmt.Fprintf(lw, "%s 내림 / removed\n", old)
		}
	}
	if err := d.services.SetInstance(svc.ID, name); err != nil {
		return out, err
	}
	d.services.SetStopped(svc.ID, false)
	d.kick()
	if svc.Kind == service.KindRepo {
		d.pruneImages(ctx, svc, id)
	}
	return out, nil
}

// waitReady는 새 컨테이너가 port에서 연결을 받을 때까지 기다린다. 먼저 죽으면 바로 실패한다.
func (d *Deployer) waitReady(ctx context.Context, name string, port int) error {
	ctx, cancel := context.WithTimeout(ctx, d.readyTimeout)
	defer cancel()
	addr := net.JoinHostPort(name, strconv.Itoa(port))
	for {
		if st, err := d.docker.Inspect(ctx, name); err == nil && !st.Running && (st.Status == "exited" || st.Status == "dead") {
			return fmt.Errorf("컨테이너가 바로 멈췄어요 (종료 코드 %d) / the container exited (code %d)", st.ExitCode, st.ExitCode)
		}
		dctx, dcancel := context.WithTimeout(ctx, 2*time.Second)
		err := d.dial(dctx, addr)
		dcancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%d초 동안 포트 %d에서 응답이 없어요. 앱이 다른 포트를 듣고 있지 않은지 확인하세요 / no answer on port %d after %ds",
				int(d.readyTimeout.Seconds()), port, port, int(d.readyTimeout.Seconds()))
		case <-time.After(d.pollEvery):
		}
	}
}

// olderInstances는 새 것(keep)을 빼고 이 서비스로 돌고 있는 컨테이너들이다 — 지금 것, 라벨이 붙은 남은 것.
func (d *Deployer) olderInstances(ctx context.Context, svc service.Service, keep string) []string {
	seen := map[string]bool{keep: true}
	var out []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	add(svc.CurrentContainer())
	if names, err := d.docker.ByLabel(ctx, LabelService, strconv.FormatInt(svc.ID, 10)); err == nil {
		for _, n := range names {
			add(n)
		}
	}
	return out
}

// ImageTag는 빌드한 이미지의 이름이다 — 서비스 이름과 배포 번호.
func ImageTag(name string, id int64) string { return fmt.Sprintf("naru-%s:%d", name, id) }

// pruneImages는 최근 성공한 배포 몇 개의 이미지만 남긴다. 쓰는 중인 이미지는 Docker가 지우지 않는다.
func (d *Deployer) pruneImages(ctx context.Context, svc service.Service, current int64) {
	ids, err := d.store.Successful(svc.ID)
	if err != nil {
		return
	}
	keep := map[int64]bool{current: true}
	for i, id := range ids {
		if i < keepImages-1 {
			keep[id] = true
		}
	}
	for _, id := range ids {
		if !keep[id] {
			d.docker.RemoveImage(ctx, ImageTag(svc.Name, id))
		}
	}
}

// ── 정적 사이트 ────────────────────────────

func (d *Deployer) deployStatic(ctx context.Context, svc service.Service, id int64, from *Deployment, lw io.Writer) (outcome, error) {
	var out outcome
	siteDir := filepath.Join(d.cfg.SitesDir, svc.Name)
	release := strconv.FormatInt(id, 10)

	if from != nil {
		// 옛 파일을 이번 배포 번호로 복사한다 — 배포 기록마다 자기 파일을 가져야 "지금"과 되돌리기가 맞는다
		step(lw, fmt.Sprintf("#%d 배포를 다시 올립니다", from.ID), fmt.Sprintf("restoring deployment #%d", from.ID))
		old := filepath.Join(siteDir, strconv.FormatInt(from.ID, 10))
		if _, err := os.Stat(old); err != nil {
			return out, fmt.Errorf("그때의 파일이 지워졌어요 / those files are gone: %w", err)
		}
		dst := filepath.Join(siteDir, release)
		if _, err := source.CopyTree(old, dst); err != nil {
			os.RemoveAll(dst)
			return out, err
		}
	} else {
		secrets, err := d.services.Secrets(svc.ID)
		if err != nil {
			return out, err
		}
		dir := filepath.Join(d.cfg.WorkDir, fmt.Sprintf("%s-%d", svc.Name, id))
		defer os.RemoveAll(dir)
		step(lw, "코드 가져오기", "fetching the code")
		commit, err := d.clone(ctx, svc.Source, svc.Branch, secrets.GitToken, dir, lw)
		if err != nil {
			return out, err
		}
		out.commit, out.message = commit.Hash, commit.Message
		fmt.Fprintf(lw, "%s %s\n", shortHash(commit.Hash), commit.Message)

		src, err := source.Within(dir, svc.Folder)
		if err != nil {
			return out, fmt.Errorf("폴더 %q를 찾을 수 없어요 / folder not found: %w", svc.Folder, err)
		}
		if _, err := os.Stat(filepath.Join(src, "index.html")); err != nil {
			return out, fmt.Errorf("%s 에 index.html이 없어요 — 보여줄 폴더를 확인하세요 / no index.html in %q", displayFolder(svc.Folder), displayFolder(svc.Folder))
		}
		step(lw, "파일 올리기", "publishing the files")
		dst := filepath.Join(siteDir, release)
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return out, err
		}
		n, err := source.CopyTree(src, dst)
		if err != nil {
			os.RemoveAll(dst)
			return out, err
		}
		fmt.Fprintf(lw, "파일 %d개 / %d files\n", n, n)
	}

	if err := d.services.SetRelease(svc.ID, release); err != nil {
		return out, err
	}
	d.kick()
	pruneReleases(siteDir, release)
	return out, nil
}

func displayFolder(f string) string {
	if f == "" {
		return "/"
	}
	return f
}

// pruneReleases는 최근 배포본 몇 개만 남긴다 (지금 것은 언제나 남는다).
func pruneReleases(siteDir, current string) {
	entries, err := os.ReadDir(siteDir)
	if err != nil {
		return
	}
	var ids []int
	for _, e := range entries {
		if n, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() {
			ids = append(ids, n)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ids)))
	for i, n := range ids {
		if i >= keepReleases && strconv.Itoa(n) != current {
			os.RemoveAll(filepath.Join(siteDir, strconv.Itoa(n)))
		}
	}
}

// ── 멈추기·지우기 ──────────────────────────

func (d *Deployer) Stop(ctx context.Context, serviceID int64) error {
	svc, err := d.services.Get(serviceID)
	if err != nil {
		return err
	}
	if !svc.HasContainer() {
		return ErrNotDeployable
	}
	if err := d.services.SetStopped(serviceID, true); err != nil {
		return err
	}
	return d.docker.Stop(ctx, svc.CurrentContainer(), stopGrace)
}

func (d *Deployer) Start(ctx context.Context, serviceID int64) error {
	svc, err := d.services.Get(serviceID)
	if err != nil {
		return err
	}
	if !svc.HasContainer() {
		return ErrNotDeployable
	}
	if err := d.docker.Start(ctx, svc.CurrentContainer()); err != nil {
		return err
	}
	return d.services.SetStopped(serviceID, false)
}

// Delete는 서비스와 그 컨테이너·빌드 이미지·정적 파일을 모두 지운다. 다른 곳의 데이터(볼륨)는 건드리지 않는다.
func (d *Deployer) Delete(ctx context.Context, serviceID int64) error {
	if d.Running(serviceID) {
		return ErrBusy
	}
	svc, err := d.services.Get(serviceID)
	if err != nil {
		return err
	}
	if svc.HasContainer() {
		for _, n := range d.olderInstances(ctx, svc, "") {
			d.docker.Remove(ctx, n)
		}
	}
	if svc.Kind == service.KindRepo {
		if ids, err := d.store.Successful(serviceID); err == nil {
			for _, id := range ids {
				d.docker.RemoveImage(ctx, ImageTag(svc.Name, id))
			}
		}
	}
	if svc.Kind == service.KindStatic && svc.Name != "" {
		os.RemoveAll(filepath.Join(d.cfg.SitesDir, svc.Name))
	}
	if err := d.services.Delete(serviceID); err != nil {
		return err
	}
	d.kick()
	d.log.Info("service deleted", "service", svc.Name)
	return nil
}

// ── 값 다루기 ──────────────────────────────

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseEnv는 "KEY=value" 줄들을 읽는다. 잘못된 줄은 건너뛴다 (화면에서 이미 검사한다).
func ParseEnv(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		k, _, ok := strings.Cut(line, "=")
		if ok && envKey.MatchString(strings.TrimSpace(k)) {
			out = append(out, strings.TrimSpace(k)+"="+line[len(k)+1:])
		}
	}
	return out
}

// ParseVolumes는 "/호스트경로:/컨테이너경로[:ro]" 줄들을 읽는다.
func ParseVolumes(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && ValidVolume(line) {
			out = append(out, line)
		}
	}
	return out
}

func ValidVolume(v string) bool {
	parts := strings.Split(v, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, p := range parts[:2] {
		if !strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
			return false
		}
	}
	return len(parts) == 2 || parts[2] == "ro" || parts[2] == "rw"
}

// ValidEnvLine은 화면에서 쓴다 — 줄 하나가 KEY=value 모양인가.
func ValidEnvLine(line string) bool {
	k, _, ok := strings.Cut(line, "=")
	return ok && envKey.MatchString(strings.TrimSpace(k))
}

func only(ports []int) int {
	if len(ports) == 1 {
		return ports[0]
	}
	return 0
}

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// ── 로그 ──────────────────────────────────

const maxLog = 512 << 10

// logWriter는 배포 로그를 모으고 1초마다 DB에 남긴다 — 화면이 진행 중에도 볼 수 있게.
type logWriter struct {
	mu    sync.Mutex
	buf   []byte
	dirty bool
	save  func(string)
	done  chan struct{}
	wg    sync.WaitGroup
}

func newLogWriter(save func(string)) *logWriter {
	w := &logWriter{save: save, done: make(chan struct{})}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-w.done:
				return
			case <-t.C:
				w.flush()
			}
		}
	}()
	return w
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > maxLog { // 앞을 잘라 끝을 남긴다 — 실패 원인은 대개 끝에 있다
		w.buf = append([]byte("…\n"), w.buf[len(w.buf)-maxLog:]...)
	}
	w.dirty = true
	w.mu.Unlock()
	return len(p), nil
}

func (w *logWriter) flush() {
	w.mu.Lock()
	if !w.dirty {
		w.mu.Unlock()
		return
	}
	s := string(w.buf)
	w.dirty = false
	w.mu.Unlock()
	w.save(s)
}

func (w *logWriter) close() string {
	close(w.done)
	w.wg.Wait()
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}
