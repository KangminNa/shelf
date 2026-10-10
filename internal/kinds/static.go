package kinds

import (
	"context"
	"fmt"
	"path"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// StaticBuilder는 저장소를 내려받아 보여줄 폴더를 배포본으로 올린다. 컨테이너는 없다.
type StaticBuilder struct{ t Tools }

func (b StaticBuilder) Build(ctx context.Context, req model.BuildRequest, log contract.DeployLogWriter) (model.Version, error) {
	s := req.Service
	dir, giveBack, commit, err := fetchCode(ctx, b.t, req, log)
	if err != nil {
		return model.Version{}, err
	}
	defer giveBack()
	v := model.Version{Commit: commit, Folder: model.SiteFolder{Site: s.Name, Release: req.Deployment}}

	shown := s.Folder
	if shown == "" {
		shown = "/"
	}
	folder, _ := model.ParseFolderPath(s.Folder)
	from, err := b.t.Work.Inside(dir, folder)
	if err != nil {
		return v, fmt.Errorf("폴더 %q를 찾을 수 없어요 / folder not found: %w", s.Folder, err)
	}
	log.Step("파일 올리기", "publishing the files")
	n, err := b.t.Files.Publish(from, v.Folder)
	if err == model.ErrNoIndexHTML {
		return v, explain(err, fmt.Sprintf("%s 에 index.html이 없어요 — 보여줄 폴더를 확인하세요 / no index.html in %q", shown, shown))
	}
	if err != nil {
		return v, err
	}
	fmt.Fprintf(log, "파일 %d개 / %d files\n", n, n)
	return v, nil
}

// FolderSwapper는 웹서버가 서빙할 배포본을 바꾼다. 되돌릴 때는 옛 파일을 이번 배포 번호로 복사한다 —
// 배포 기록마다 자기 파일을 가져야 "지금 서빙 중"과 되돌리기가 맞는다.
type FolderSwapper struct{ t Tools }

func (w FolderSwapper) Swap(ctx context.Context, req model.SwapRequest, v model.Version, log contract.DeployLogWriter) (model.LiveState, error) {
	target := model.SiteFolder{Site: req.Service.Name, Release: req.Deployment}
	if v.Folder != target {
		if v.Folder.Site.IsZero() || !w.t.Files.Exists(v.Folder) {
			return model.LiveState{}, explain(errGone, "그때의 파일이 지워졌어요 / those files are gone")
		}
		if err := w.t.Files.Copy(v.Folder, target); err != nil {
			return model.LiveState{}, err
		}
	}
	return model.LiveState{Release: target.ReleaseName()}, nil
}

// Retire는 할 일이 없다 — 웹서버가 새 배포본을 가리키면 옛 것은 그냥 남는다 (정리는 OldVersionCleaner가).
func (FolderSwapper) Retire(context.Context, model.Service, model.LiveState, contract.DeployLogWriter) {
}

// FolderDestination은 웹서버 컨테이너에서 본 배포본 폴더다. 아직 배포하지 않았으면 비어 있다.
type FolderDestination struct{ shown string }

func (f FolderDestination) Find(s model.Service) model.Destination {
	if s.Live.Release == "" {
		return model.Destination{}
	}
	root := f.shown
	if root == "" {
		root = "/srv/sites"
	}
	return model.Destination{Folder: path.Join(root, s.Name.String(), s.Live.Release)}
}

// StaticStatus는 배포본이 있으면 "파일 서빙 중"이다.
type StaticStatus struct{}

func (StaticStatus) Read(s model.Service, _ model.ContainerStates) model.ServiceStatus {
	if s.Live.Release == "" {
		return model.ServiceStatus{Key: "notdeployed", Tone: "muted"}
	}
	return model.ServiceStatus{Key: "static", Tone: "ok"}
}
