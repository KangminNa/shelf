package kinds

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// ── 새 버전 만들기 ─────────────────────────

// RepoBuilder는 저장소를 내려받아 이미지를 빌드한다.
type RepoBuilder struct{ t Tools }

func (b RepoBuilder) Build(ctx context.Context, req model.BuildRequest, log contract.DeployLogWriter) (model.Version, error) {
	s := req.Service
	dir, giveBack, commit, err := fetchCode(ctx, b.t, req, log)
	if err != nil {
		return model.Version{}, err
	}
	defer giveBack()
	v := model.Version{Commit: commit}

	buildPath, _ := model.ParseFolderPath(s.BuildPath)
	folder, err := b.t.Work.Inside(dir, buildPath)
	if err != nil {
		return v, fmt.Errorf("빌드 경로 %q를 찾을 수 없어요 / build path not found: %w", s.BuildPath, err)
	}
	ports, found := b.t.Dockerfile.Ports(folder)
	if !found {
		return v, explain(model.ErrNoDockerfile, "Dockerfile이 없어요. HTML 파일만 있는 저장소라면 '정적 사이트'로 올리세요 / no Dockerfile — for plain HTML use a static site")
	}
	v.Port = only(ports)

	log.Step("이미지 만들기", "building the image")
	packed := b.t.Packer.Pack(folder)
	defer packed.Close()
	v.Image, err = b.t.Builder.Build(ctx, packed, model.ImageTag(s.Name, req.Deployment), log)
	return v, err
}

// ImageFetcher는 이미지를 받는다.
type ImageFetcher struct{ t Tools }

func (f ImageFetcher) Build(ctx context.Context, req model.BuildRequest, log contract.DeployLogWriter) (model.Version, error) {
	ref, err := model.ParseImageRef(req.Service.Source)
	if err != nil {
		return model.Version{}, err
	}
	log.Step("이미지 받기", "pulling the image")
	got, err := f.t.Puller.Pull(ctx, ref, log)
	if err != nil {
		return model.Version{}, err
	}
	return model.Version{Image: got.ID, Port: only(got.Ports)}, nil
}

// fetchCode는 작업 폴더를 빌려 코드를 내려받는다. 다 쓰면 giveBack을 불러 돌려준다.
func fetchCode(ctx context.Context, t Tools, req model.BuildRequest, log contract.DeployLogWriter) (dir string, giveBack func(), c model.Commit, err error) {
	s := req.Service
	repo, err := model.ParseRepoURL(s.Source)
	if err != nil {
		return "", nil, c, err
	}
	branch, err := model.ParseBranch(s.Branch)
	if err != nil {
		return "", nil, c, err
	}
	if branch.IsZero() {
		branch = model.DefaultBranch()
	}
	secrets, err := t.Secrets.Get(ctx, s.ID)
	if err != nil {
		return "", nil, c, err
	}
	dir, giveBack, err = t.Work.Borrow(fmt.Sprintf("%s-%d", s.Name, req.Deployment))
	if err != nil {
		return "", nil, c, err
	}
	log.Step("코드 가져오기", "fetching the code")
	c, err = t.Code.Download(ctx, model.CodeSource{Repo: repo, Branch: branch, Token: secrets.GitToken}, dir, log)
	if err != nil {
		giveBack()
		return "", nil, c, err
	}
	fmt.Fprintf(log, "%s %s\n", model.Deployment{Commit: c.Hash}.ShortCommit(), c.Message)
	return dir, giveBack, c, nil
}

// only는 포트가 하나일 때만 그것이다. 여럿이면 어느 것인지 모른다.
func only(ports []model.Port) model.Port {
	if len(ports) == 1 {
		return ports[0]
	}
	return 0
}

// ── 바꿔 끼우기 ────────────────────────────

// ContainerSwapper는 새 컨테이너를 같은 네트워크 별칭으로 띄우고, 응답하면 옛 것을 내린다.
// 별칭이 같으니 웹서버는 설정을 바꾸지 않아도 새 것으로 보낸다 — 그래서 요청이 끊기지 않는다.
// 새 것이 응답하지 않으면 새 것만 지우고 지금 도는 것은 그대로 둔다.
type ContainerSwapper struct{ t Tools }

func (w ContainerSwapper) Swap(ctx context.Context, req model.SwapRequest, v model.Version, log contract.DeployLogWriter) (model.LiveState, error) {
	s := req.Service
	if v.Image == "" {
		return model.LiveState{}, model.ErrCannotRollBack
	}
	if !w.t.Images.Exists(ctx, v.Image) {
		return model.LiveState{}, explain(errGone, "그때의 이미지가 지워졌어요 — 다시 배포하세요 / that image is gone, deploy again")
	}
	port := s.Port
	if port == 0 {
		port = v.Port
		if port == 0 {
			return model.LiveState{}, explain(model.ErrUnknownPort, "앱이 어느 포트를 듣는지 몰라요. Dockerfile에 EXPOSE를 쓰거나 서비스 설정에서 앱 포트를 적어 주세요 / unknown app port — add EXPOSE or set the port")
		}
		fmt.Fprintf(log, "앱 포트 %d (이미지에서 찾음) / app port %d (from the image)\n", port, port)
	}
	secrets, err := w.t.Secrets.Get(ctx, s.ID)
	if err != nil {
		return model.LiveState{}, err
	}

	alias := s.Live.Alias
	if alias == "" {
		alias = "naru-" + s.Name.String()
	}
	name := fmt.Sprintf("%s-%d", alias, req.Deployment)
	log.Step("새 컨테이너 시작", "starting the new container")
	if err := w.t.Starter.Start(ctx, model.ContainerSpec{
		Name: name, Image: v.Image, Env: secrets.Env.List(), Binds: secrets.Volumes.List(),
		Network: w.t.Network, Aliases: []string{alias}, Service: s.ID, Deploy: req.Deployment,
	}); err != nil {
		return model.LiveState{}, err
	}

	log.Step(fmt.Sprintf("포트 %d 응답 기다리기", port), fmt.Sprintf("waiting for port %d", port))
	if err := w.waitReady(ctx, name, port); err != nil {
		if logs, lerr := w.t.Watcher.Logs(context.Background(), name, 30); lerr == nil && strings.TrimSpace(logs) != "" {
			fmt.Fprintf(log, "— 컨테이너 출력 마지막 30줄 / last 30 lines —\n%s\n", logs)
		}
		w.t.Remover.Remove(context.Background(), name)
		return model.LiveState{}, fmt.Errorf("%w — 지금 돌던 것은 그대로 둡니다 / the running version is untouched", err)
	}
	fmt.Fprintln(log, "응답함 / answering")

	log.Step("옛 컨테이너 내리기", "retiring the old container")
	for _, old := range w.older(ctx, s, name) {
		w.t.Switch.TurnOff(ctx, old)
		if err := w.t.Remover.Remove(ctx, old); err != nil {
			fmt.Fprintf(log, "%s: %v\n", old, err)
		} else {
			fmt.Fprintf(log, "%s 내림 / removed\n", old)
		}
	}
	return model.LiveState{Alias: alias, Instance: name, Port: port}, nil
}

// waitReady는 새 컨테이너가 port에서 연결을 받을 때까지 기다린다. 먼저 죽으면 바로 실패한다.
func (w ContainerSwapper) waitReady(ctx context.Context, name string, port model.Port) error {
	ctx, cancel := context.WithTimeout(ctx, w.t.ReadyTimeout)
	defer cancel()
	wait := int(w.t.ReadyTimeout.Seconds())
	for {
		if st, err := w.t.Watcher.One(ctx, name); err == nil && !st.Running && (st.State == "exited" || st.State == "dead") {
			return fmt.Errorf("컨테이너가 바로 멈췄어요 (종료 코드 %d) / the container exited (code %d)", st.ExitCode, st.ExitCode)
		}
		dctx, dcancel := context.WithTimeout(ctx, 2*time.Second)
		err := w.t.Ports.Answers(dctx, name, port)
		dcancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%d초 동안 포트 %d에서 응답이 없어요. 앱이 다른 포트를 듣고 있지 않은지 확인하세요 / no answer on port %d after %ds", wait, port, port, wait)
		case <-time.After(w.t.PollEvery):
		}
	}
}

// older는 새 것(keep)을 빼고 이 서비스로 돌고 있는 컨테이너들이다 — 지금 것, 라벨이 붙은 남은 것.
func (w ContainerSwapper) older(ctx context.Context, s model.Service, keep string) []string {
	seen := map[string]bool{keep: true}
	var out []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	add(s.Live.CurrentContainer())
	if names, err := w.t.Watcher.BelongingTo(ctx, s.ID); err == nil {
		for _, n := range names {
			add(n)
		}
	}
	return out
}

// ── 보낼 곳 · 상태 ─────────────────────────

// ContainerDestination은 서비스 별칭:앱 포트다. 아직 모르면 비어 있다.
type ContainerDestination struct{}

func (ContainerDestination) Find(s model.Service) model.Destination {
	d := model.Destination{Container: true}
	if s.Live.Alias != "" && s.Port != 0 {
		d.Address = fmt.Sprintf("%s:%d", s.Live.Alias, s.Port)
	}
	return d
}

// ContainerStatus는 컨테이너 상태를 화면 상태로 바꾼다. containers가 nil이면 Docker를 읽지 못한 것이다.
type ContainerStatus struct{}

func (ContainerStatus) Read(s model.Service, containers model.ContainerStates) model.ServiceStatus {
	if containers == nil {
		return model.ServiceStatus{Key: "unknown", Tone: "muted"}
	}
	c, ok := containers[s.Live.CurrentContainer()]
	if !ok {
		if s.Live.CurrentContainer() == "" {
			return model.ServiceStatus{Key: "notdeployed", Tone: "muted"}
		}
		return model.ServiceStatus{Key: "missing", Tone: "bad"}
	}
	st := model.ServiceStatus{Detail: c.Status}
	switch c.State {
	case "running":
		st.Key, st.Tone = "running", "ok"
	case "restarting", "dead":
		st.Key, st.Tone = "restarting", "bad"
	case "exited":
		st.Key, st.Tone = "crashed", "bad"
		if s.Live.Stopped {
			st.Key, st.Tone = "stopped", "muted"
		}
	case "paused":
		st.Key, st.Tone = "paused", "warn"
	default:
		st.Key, st.Tone = "created", "muted"
	}
	return st
}
