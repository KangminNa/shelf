package model

import (
	"strconv"
	"time"
)

type DeployStatus string

const (
	DeployRunning DeployStatus = "running"
	DeploySuccess DeployStatus = "success"
	DeployFailed  DeployStatus = "failed"
)

// DeployReason은 왜 배포했는가다.
type DeployReason string

const (
	ReasonManual   DeployReason = "manual"
	ReasonPush     DeployReason = "webhook"
	ReasonCreate   DeployReason = "create"
	ReasonRollBack DeployReason = "rollback"
)

// Deployment는 배포 한 번의 기록이다.
type Deployment struct {
	ID         DeploymentID
	ServiceID  ServiceID
	Status     DeployStatus
	Reason     DeployReason
	Commit     string
	Message    string
	Image      ImageID
	Log        string
	StartedAt  time.Time
	FinishedAt time.Time
}

// Duration은 걸린 시간이다. 끝나지 않았으면 지금까지.
func (d Deployment) Duration() time.Duration {
	end := d.FinishedAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(d.StartedAt).Round(time.Second)
}

// ShortCommit은 커밋 앞 7자리다.
func (d Deployment) ShortCommit() string {
	if len(d.Commit) > 7 {
		return d.Commit[:7]
	}
	return d.Commit
}

// ImageID는 Docker 이미지 ID다.
type ImageID string

// ImageTag는 빌드한 이미지의 이름이다 — 서비스 이름과 배포 번호. 되돌리기와 정리가 이 이름으로 찾는다.
func ImageTag(n ServiceName, d DeploymentID) string {
	return "naru-" + n.String() + ":" + strconv.FormatInt(int64(d), 10)
}

// Commit은 가져온 커밋이다.
type Commit struct {
	Hash    string
	Message string
}

// Version은 배포할 수 있게 만든 것이다 — 이미지 또는 정적 파일 폴더.
type Version struct {
	Image  ImageID
	Folder SiteFolder
	Commit Commit
	Port   Port // 찾은 앱 포트 (0이면 서비스 설정을 따른다)
}

// SiteFolder는 정적 사이트 배포본 하나다.
type SiteFolder struct {
	Site    ServiceName
	Release DeploymentID
}

func (f SiteFolder) ReleaseName() string { return strconv.FormatInt(int64(f.Release), 10) }

// BuildRequest는 새 버전 만들기에 넘기는 값이다.
type BuildRequest struct {
	Service    Service
	Deployment DeploymentID
}

// SwapRequest는 바꿔 끼우기에 넘기는 값이다.
type SwapRequest struct {
	Service    Service
	Deployment DeploymentID
}

// CodeSource는 내려받을 코드다.
type CodeSource struct {
	Repo   RepoURL
	Branch Branch
	Token  string
}

// ImageDetails는 받은 이미지의 ID와 열어둔 포트다.
type ImageDetails struct {
	ID    ImageID
	Ports []Port
}

// ContainerSpec은 띄울 컨테이너다.
type ContainerSpec struct {
	Name    string
	Image   ImageID
	Env     []string
	Binds   []string
	Network string
	Aliases []string
	Service ServiceID
	Deploy  DeploymentID
}

// ContainerState는 컨테이너 하나의 상태다.
type ContainerState struct {
	Name     string
	Running  bool
	State    string // running · restarting · exited · created · paused · dead
	Status   string // "Up 3 hours"
	ExitCode int
}

// ContainerStates는 이름 → 상태. nil이면 Docker를 읽지 못했다.
type ContainerStates map[string]ContainerState
