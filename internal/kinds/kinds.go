// Package kinds는 서비스 종류(저장소·이미지·정적 사이트·외부 연결)마다 다른 일을 맡는다.
// 종류에 따른 분기는 이 패키지에만 있다 — 다른 곳은 KindLookup으로 담당자 묶음을 받아 쓴다.
package kinds

import (
	"errors"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// Tools는 종류 담당자들이 쓰는 도구다. 모두 인터페이스다.
type Tools struct {
	Work       contract.WorkFolder
	Code       contract.CodeDownloader
	Secrets    contract.SecretStore
	Dockerfile contract.DockerfilePortReader
	Packer     contract.BuildContextPacker
	Builder    contract.ImageBuilder
	Puller     contract.ImagePuller
	Images     contract.ImageCleaner
	Starter    contract.ContainerStarter
	Remover    contract.ContainerRemover
	Switch     contract.ContainerSwitch
	Watcher    contract.ContainerWatcher
	Ports      contract.PortChecker
	Files      contract.SiteFiles

	// 웹서버가 컨테이너·"이 서버"에 닿는 방법 — 설치 방식에 따라 app이 고른다 (비면 Docker 방식).
	ContainerDestination contract.DestinationFinder
	ExternalDestination  contract.DestinationFinder

	Network    string // 앱 컨테이너가 붙는 네트워크
	SitesShown string // 웹서버 컨테이너에서 본 정적 사이트 폴더 (기본 /srv/sites)

	ReadyTimeout time.Duration // 새 컨테이너가 응답하기를 기다리는 시간 (기본 60초)
	PollEvery    time.Duration // 그 사이 확인 간격 (기본 0.5초)
}

// kindLookup은 종류 이름 → 담당자 묶음이다.
type kindLookup map[model.KindName]contract.KindTools

// NewLookup은 네 종류의 담당자 묶음을 만든다.
func NewLookup(t Tools) contract.KindLookup {
	if t.ReadyTimeout == 0 {
		t.ReadyTimeout = 60 * time.Second
	}
	if t.PollEvery == 0 {
		t.PollEvery = 500 * time.Millisecond
	}
	swapper := ContainerSwapper{t}
	container := t.ContainerDestination
	if container == nil {
		container = ContainerAliasDestination{}
	}
	external := t.ExternalDestination
	if external == nil {
		external = ExternalDestination{}
	}
	status := ContainerStatus{}
	return kindLookup{
		model.KindRepo: {
			Input: RepoInput{}, Builder: RepoBuilder{t}, Swapper: swapper, Destination: container, Status: status,
		},
		model.KindImage: {
			Input: ImageInput{}, Builder: ImageFetcher{t}, Swapper: swapper, Destination: container, Status: status,
		},
		model.KindStatic: {
			Input: StaticInput{}, Builder: StaticBuilder{t}, Swapper: FolderSwapper{t}, Destination: FolderDestination{t.SitesShown}, Status: StaticStatus{},
		},
		model.KindExternal: {
			Input: ExternalInput{}, Destination: external, Status: ExternalStatus{},
		},
	}
}

func (l kindLookup) Find(k model.KindName) (contract.KindTools, bool) {
	tools, ok := l[k]
	return tools, ok
}

// explained는 배포 기록에 남길 사람 말 문구와, 객체들이 errors.Is로 알아볼 실패를 함께 갖는다.
type explained struct {
	is   error
	text string
}

func (e explained) Error() string { return e.text }
func (e explained) Unwrap() error { return e.is }

func explain(is error, text string) error { return explained{is, text} }

var errGone = errors.New("the files or image of that deployment are gone")
