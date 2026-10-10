package model

import "time"

// ── 계정 ─────────────────────────────────

// Account는 관리자 한 명이다.
type Account struct {
	ID       AccountID
	Username Username
}

// PasswordHash는 "salt:hash" (v1과 같은 모양). 화면 계층은 이 타입을 쓰지 않는다.
type PasswordHash string

// SessionToken은 쿠키에만 있는 원문이다. 저장할 때는 SessionDigest로만.
type SessionToken string

// SessionDigest는 세션 토큰의 SHA-256이다.
type SessionDigest string

// ── 웹훅 ─────────────────────────────────

// HookRequest는 받은 웹훅이다.
type HookRequest struct {
	Headers map[string]string // 소문자 키
	Query   map[string]string
	Body    []byte
	Ref     string // push가 가리키는 ref ("refs/heads/main"), 없으면 빈 값
	Ping    bool
}

// HookResult는 웹훅을 처리한 결과다.
type HookResult struct {
	Status     int // HTTP 상태
	Message    string
	Deployment DeploymentID
}

// ── 화면에 보여줄 모습 ───────────────────────

// ServiceStatus는 화면 상태다. Key는 문구 키, Tone은 ok·warn·bad·muted.
type ServiceStatus struct {
	Key    string
	Tone   string
	Detail string // Docker가 말하는 상태 ("Up 3 hours")
}

// ServiceCard는 홈의 카드 하나다.
type ServiceCard struct {
	ID     ServiceID
	Name   string
	Kind   KindName
	Domain string
	Status ServiceStatus
}

// HomeView는 홈 화면이다.
type HomeView struct {
	Cards      []ServiceCard
	DockerDown bool
}

// WebhookView는 웹훅 설정에 필요한 값이다 (시크릿은 GitHub에 넣어야 하므로 보여준다).
type WebhookView struct {
	URL    string // 관리 주소가 없으면 빈 값
	Secret string
}

// ServiceForm은 설정 폼에 미리 채울 값이다. 토큰 원문은 없다.
type ServiceForm struct {
	Source     string
	Branch     string
	BuildPath  string
	Folder     string
	External   string
	Port       Port
	AutoDeploy bool
	EnvText    string
	Volumes    string
	HasToken   bool
}

// ServiceView는 서비스 상세 화면이다.
type ServiceView struct {
	Service    Service
	Status     ServiceStatus
	Target     string
	Container  bool // 멈추기·켜기가 된다
	Deployable bool // 배포가 있다
	Deploys    []Deployment
	LiveID     DeploymentID
	Deploying  bool
	Webhook    WebhookView
	Form       ServiceForm
}

// StepView는 배포 단계 하나다. State: done · doing · failed
type StepView struct {
	Ko, En string
	State  string
}

// DeploymentView는 배포 화면이다.
type DeploymentView struct {
	Service    Service
	Deployment Deployment
	Steps      []StepView
}

// ServerSnapshot은 서버의 CPU·메모리·디스크다. 읽지 못한 값은 Has…가 거짓.
type ServerSnapshot struct {
	Cores      int
	CPUPercent float64
	HasCPU     bool
	Load1      float64
	HasLoad    bool
	MemTotal   uint64
	MemUsed    uint64
	HasMem     bool
	DiskTotal  uint64
	DiskFree   uint64
	HasDisk    bool
	Uptime     time.Duration
}

// ── 일어난 일 ─────────────────────────────

// Event는 일어난 일이다. 아래 타입들만 Event다.
type Event interface{ event() }

// SiteMapChanged: 주소·포트·서빙 폴더·관리 주소·첫 설정 상태가 바뀌었다.
type SiteMapChanged struct{ Reason string }

// DeployFinished: 배포가 끝났다.
type DeployFinished struct {
	Service    ServiceID
	Deployment DeploymentID
	OK         bool
}

// ServiceDeleted: 서비스를 지웠다.
type ServiceDeleted struct{ Service ServiceID }

func (SiteMapChanged) event() {}
func (DeployFinished) event() {}
func (ServiceDeleted) event() {}
