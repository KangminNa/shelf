package model

import (
	"strconv"
	"time"
)

type (
	ServiceID    int64
	DomainID     int64
	DeploymentID int64
	AccountID    int64
)

// KindName은 서비스 종류다. 종류마다 다른 행동은 kinds 패키지만 안다.
type KindName string

const (
	KindRepo     KindName = "repo"
	KindImage    KindName = "image"
	KindStatic   KindName = "static"
	KindExternal KindName = "external"
)

// AllKinds는 화면에 보여줄 순서다.
func AllKinds() []KindName { return []KindName{KindRepo, KindImage, KindStatic, KindExternal} }

// ParseKindName은 아는 종류 이름인지 본다.
func ParseKindName(s string) (KindName, error) {
	for _, k := range AllKinds() {
		if string(k) == s {
			return k, nil
		}
	}
	return "", InputError{Field: "kind", Code: "kind"}
}

// ServiceInput은 화면에서 받은 서비스 값이다. 모양 검사는 값 객체가 이미 했다.
type ServiceInput struct {
	Kind       KindName
	Name       ServiceName // 비면 NameChooser가 짓는다
	Repo       RepoURL
	Branch     Branch
	Image      ImageRef
	External   ExternalAddress
	Folder     FolderPath
	BuildPath  FolderPath
	Port       Port
	Domain     DomainName // 비어도 된다
	Token      string     // 비면 그대로 둔다
	ClearToken bool
	Env        EnvVars
	Volumes    Volumes
	AutoDeploy bool
}

// Source는 이 입력의 소스를 글자로 — 저장소 주소나 이미지 이름. 종류가 정한 칸만 채워져 있다.
func (in ServiceInput) Source() string {
	if !in.Repo.IsZero() {
		return in.Repo.String()
	}
	return in.Image.String()
}

// Domain은 서비스에 붙은 주소 하나다.
type Domain struct {
	ID     DomainID
	Domain DomainName
	HTTPS  bool
	HSTS   bool
}

// DomainInput은 화면에서 붙이는 주소다.
type DomainInput struct {
	Domain DomainName
	HTTPS  bool
}

// Service는 서비스 하나의 지금 모습이다. 비밀은 없다.
type Service struct {
	ID         ServiceID
	Name       ServiceName
	Kind       KindName
	Source     string // 저장소 주소 또는 이미지 이름 (저장할 때 이미 검사됨)
	Branch     string
	BuildPath  string
	Folder     string
	External   string
	Port       Port
	AutoDeploy bool
	Live       LiveState
	HookLog    HookLog
	Domains    []Domain
}

// PrimaryDomain은 먼저 붙인 주소다.
func (s Service) PrimaryDomain() string {
	if len(s.Domains) == 0 {
		return ""
	}
	return s.Domains[0].Domain.String()
}

// LiveState는 지금 도는 것이다.
type LiveState struct {
	Alias    string // 네트워크에서 서비스를 부르는 이름 — 웹서버가 여기로 보낸다
	Instance string // 지금 요청을 받는 실제 컨테이너
	// InstanceIP는 그 컨테이너의 앱 네트워크 IP — 배포 때 기록한다. 재시작하면 바뀔 수 있어
	// 사이트 지도를 그릴 때 지금 IP로 덮어쓴다 (설치형은 이 IP로 보낸다).
	InstanceIP string
	Release    string // 정적 사이트: 지금 서빙 중인 배포 번호
	Port       Port
	Stopped    bool // 직접 멈췄다 — 장애가 아니다
}

// CurrentContainer는 지금 요청을 받는 컨테이너다 (v1에서 넘겨받은 것은 Alias 그 자체).
func (l LiveState) CurrentContainer() string {
	if l.Instance != "" {
		return l.Instance
	}
	return l.Alias
}

// LiveDeployment는 지금 서빙 중인 배포 번호다 — 컨테이너 이름 끝의 번호나 정적 배포본.
func (l LiveState) LiveDeployment() DeploymentID {
	if l.Release != "" {
		n, _ := strconv.ParseInt(l.Release, 10, 64)
		return DeploymentID(n)
	}
	for i := len(l.Instance) - 1; i >= 0; i-- {
		if l.Instance[i] == '-' {
			n, _ := strconv.ParseInt(l.Instance[i+1:], 10, 64)
			return DeploymentID(n)
		}
	}
	return 0
}

// NewService는 저장할 새 서비스다.
type NewService struct {
	ID         ServiceID // 0이 아니면 그 번호 (v1 앱 번호를 지켜 웹훅 주소를 유지)
	Name       ServiceName
	Kind       KindName
	Source     string
	Branch     string
	BuildPath  string
	Folder     string
	External   string
	Port       Port
	AutoDeploy bool
	Alias      string
}

// ServiceSettings는 고칠 수 있는 값이다.
type ServiceSettings struct {
	Source     string
	Branch     string
	BuildPath  string
	Folder     string
	External   string
	Port       Port
	AutoDeploy bool
}

// ServiceSecrets는 배포에만 쓰는 비밀이다. 화면 계층은 이 타입을 쓰지 않는다.
type ServiceSecrets struct {
	Env           EnvVars
	Volumes       Volumes
	GitToken      string
	WebhookSecret string
}

// HookLog는 웹훅을 마지막으로 받은 때와 결과다.
type HookLog struct {
	At     time.Time
	Result string
}
