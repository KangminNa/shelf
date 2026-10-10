// Package contract는 객체 사이의 모든 관계다. 인터페이스만 있다.
// 어떤 객체도 다른 객체의 구체 타입을 모른다 — 여기 있는 이름으로만 서로를 안다.
// 인터페이스마다 메서드는 다섯 개 이하다 (archtest가 강제한다).
package contract

import (
	"context"
	"io"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// ── A. 관리자로 들어오기 ─────────────────────

// PasswordHasher는 비밀번호를 해시하고, 맞는지 본다.
type PasswordHasher interface {
	Hash(password string) (model.PasswordHash, error)
	Matches(password string, h model.PasswordHash) bool
}

// LoginLimiter는 같은 IP에서 계속 틀리면 잠근다.
type LoginLimiter interface {
	Allowed(ip string) bool
	Failed(ip string)
	Succeeded(ip string)
}

// SetupKey는 첫 설정 열쇠를 만들고, 계정이 없을 때만 맞는지 본다.
type SetupKey interface {
	Value() string
	Matches(ctx context.Context, key string) bool
}

// LoginManager는 로그인·로그아웃하고, 세션의 주인이 누구인지 알려준다.
type LoginManager interface {
	LogIn(ctx context.Context, ip, user, password string) (model.SessionToken, error)
	WhoIs(ctx context.Context, t model.SessionToken) (model.Account, bool)
	LogOut(ctx context.Context, t model.SessionToken)
}

// AccountManager는 첫 계정을 만들고, 비밀번호를 바꾸고, 셸에서 되찾거나 초기화한다.
type AccountManager interface {
	CreateFirst(ctx context.Context, setupKey string, user model.Username, password string) (model.SessionToken, error)
	ChangePassword(ctx context.Context, who model.Account, current, next string, keep model.SessionToken) error
	Recover(ctx context.Context, user, password string) error
	ResetAll(ctx context.Context) error
	Names(ctx context.Context) ([]string, error)
}

// AccountReader는 계정을 읽는다.
type AccountReader interface {
	Count(ctx context.Context) (int, error)
	FindByName(ctx context.Context, user string) (model.Account, model.PasswordHash, error)
	Names(ctx context.Context) ([]string, error)
}

// AccountStore는 계정을 저장한다.
type AccountStore interface {
	CreateFirst(ctx context.Context, u model.Username, h model.PasswordHash) (model.AccountID, error)
	SetPassword(ctx context.Context, id model.AccountID, h model.PasswordHash) error
	DeleteAll(ctx context.Context) error
}

// SessionStore는 로그인 세션을 저장하고 찾는다.
type SessionStore interface {
	Save(ctx context.Context, d model.SessionDigest, who model.AccountID, until time.Time) error
	FindOwner(ctx context.Context, d model.SessionDigest, now time.Time) (model.Account, bool)
	Delete(ctx context.Context, d model.SessionDigest) error
	DeleteOthers(ctx context.Context, who model.AccountID, keep model.SessionDigest) error
}

// ── B. 서버 설정 정하기 ──────────────────────

// AdminDomainSetting은 관리 주소를 정하고 알려준다. 환경 변수가 화면보다 우선한다.
type AdminDomainSetting interface {
	Get(ctx context.Context) (model.DomainName, model.SetBy)
	Set(ctx context.Context, d model.DomainName) error // 빈 값이면 지운다
}

// CertEmailSetting은 인증서 연락처 이메일을 정하고 알려준다.
type CertEmailSetting interface {
	Get(ctx context.Context) (model.Email, model.SetBy)
	Set(ctx context.Context, e model.Email) error
}

// SetupProgress는 첫 설정을 마쳤는지 기록하고 알려준다.
type SetupProgress interface {
	Done(ctx context.Context) bool
	MarkDone(ctx context.Context) error
}

// SettingStore는 서버 설정 값을 저장한다.
type SettingStore interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
}

// ── C. 서비스 관리하기 ───────────────────────

// ServiceEditor는 서비스를 만들고·고치고, 주소를 붙이고 뗀다.
type ServiceEditor interface {
	Create(ctx context.Context, in model.ServiceInput) (model.ServiceID, error)
	Update(ctx context.Context, id model.ServiceID, in model.ServiceInput) (needsRedeploy bool, err error)
	AddDomain(ctx context.Context, id model.ServiceID, d model.DomainInput) error
	RemoveDomain(ctx context.Context, id model.ServiceID, d model.DomainID) error
	SetPort(ctx context.Context, id model.ServiceID, p model.Port) error // 앱 포트만 바꾼다 — 다시 배포하지 않고 웹서버만 따라간다
}

// ServiceLauncher는 서비스를 만들고 첫 배포까지 한 번에 한다.
type ServiceLauncher interface {
	Launch(ctx context.Context, in model.ServiceInput) (model.ServiceID, model.DeploymentID, error)
}

// NameChooser는 이름을 비워 두면 지어 주고, 겹치는지 본다.
type NameChooser interface {
	Choose(ctx context.Context, in model.ServiceInput) (model.ServiceName, error)
}

// DomainChecker는 주소가 다른 서비스나 관리 화면과 겹치는지 본다.
type DomainChecker interface {
	Check(ctx context.Context, d model.DomainName) error
}

// ServiceReader는 서비스를 (주소와 함께) 읽는다.
type ServiceReader interface {
	List(ctx context.Context) ([]model.Service, error)
	Get(ctx context.Context, id model.ServiceID) (model.Service, error)
	NameTaken(ctx context.Context, n model.ServiceName) bool
}

// ServiceStore는 서비스를 저장한다.
type ServiceStore interface {
	Create(ctx context.Context, s model.NewService) (model.ServiceID, error)
	Update(ctx context.Context, id model.ServiceID, s model.ServiceSettings) error
	Delete(ctx context.Context, id model.ServiceID) error
}

// DomainStore는 서비스에 붙은 주소를 저장한다.
type DomainStore interface {
	FindOwner(ctx context.Context, d model.DomainName) (model.ServiceID, bool)
	Add(ctx context.Context, id model.ServiceID, d model.DomainInput) error
	Remove(ctx context.Context, id model.ServiceID, d model.DomainID) error
}

// SecretStore는 서비스의 비밀을 저장한다.
type SecretStore interface {
	Get(ctx context.Context, id model.ServiceID) (model.ServiceSecrets, error)
	Set(ctx context.Context, id model.ServiceID, s model.ServiceSecrets) error
}

// LiveStateStore는 지금 도는 것을 저장한다.
type LiveStateStore interface {
	Save(ctx context.Context, id model.ServiceID, s model.LiveState) error
}

// ── D. 종류마다 다르게 다루기 ─────────────────

// InputChecker는 이 종류에 필요한 값이 다 있는지 보고, 이 종류에 맞게 정리한다 (안 쓰는 칸 비우기, 기본 브랜치).
type InputChecker interface {
	Check(in model.ServiceInput) (model.ServiceInput, error)
}

// VersionBuilder는 새 버전을 만든다.
type VersionBuilder interface {
	Build(ctx context.Context, req model.BuildRequest, log DeployLogWriter) (model.Version, error)
}

// VersionSwapper는 새 버전으로 바꿔 끼운다. Swap은 새 것을 띄우고 응답까지만 확인한다 — 실패하면 지금 도는 것을 그대로 둔다.
// Retire는 웹서버가 새 것을 가리킨 뒤에 옛 것을 내린다 (그 전에 내리면 설치형에서 요청이 끊긴다).
type VersionSwapper interface {
	Swap(ctx context.Context, req model.SwapRequest, v model.Version, log DeployLogWriter) (model.LiveState, error)
	Retire(ctx context.Context, s model.Service, keep model.LiveState, log DeployLogWriter)
}

// DestinationFinder는 웹서버가 요청을 보낼 곳을 알려준다.
type DestinationFinder interface {
	Find(s model.Service) model.Destination
}

// StatusReader는 화면에 보일 상태를 읽는다.
type StatusReader interface {
	Read(s model.Service, containers model.ContainerStates) model.ServiceStatus
}

// KindTools는 종류 하나의 도구 묶음이다. 배포가 없는 종류는 Builder·Swapper가 nil.
type KindTools struct {
	Input       InputChecker
	Builder     VersionBuilder
	Swapper     VersionSwapper
	Destination DestinationFinder
	Status      StatusReader
}

// KindLookup은 종류 이름으로 그 종류의 도구 묶음을 찾아 준다.
type KindLookup interface {
	Find(k model.KindName) (KindTools, bool)
}

// ── E. 배포하기 ────────────────────────────

// Deployer는 배포를 정해진 순서로 하고, 예전 버전으로 되돌린다.
type Deployer interface {
	Deploy(ctx context.Context, id model.ServiceID, why model.DeployReason) (model.DeploymentID, error)
	RollBack(ctx context.Context, id model.ServiceID, to model.DeploymentID) (model.DeploymentID, error)
	IsDeploying(id model.ServiceID) bool
}

// DeployLock은 서비스마다 배포를 한 번에 하나만 하게 하고, 그 사이에 온 요청은 한 번으로 합친다.
type DeployLock interface {
	TryLock(id model.ServiceID, why model.DeployReason) (unlock func() (again model.DeployReason, ok bool), locked bool)
	IsLocked(id model.ServiceID) bool
}

// DeployLogWriter는 배포 기록을 쓴다. Step은 "▶ 한국어 / English" 한 줄이다.
type DeployLogWriter interface {
	io.Writer
	Step(ko, en string)
}

// DeployLog는 배포 기록을 쓰고 1초마다 저장한다.
type DeployLog interface {
	Open(d model.DeploymentID) (w DeployLogWriter, close func() (whole string))
}

// OldVersionCleaner는 되돌리기용으로 최근 버전만 남기고 오래된 것을 지운다.
type OldVersionCleaner interface {
	Clean(ctx context.Context, s model.Service, live model.DeploymentID)
}

// ServiceControl은 서비스를 멈추고, 켜고, (컨테이너·이미지·파일째) 지운다.
type ServiceControl interface {
	Stop(ctx context.Context, id model.ServiceID) error
	Start(ctx context.Context, id model.ServiceID) error
	Remove(ctx context.Context, id model.ServiceID) error
}

// DeployHistoryReader는 배포 기록을 읽는다.
type DeployHistoryReader interface {
	Get(ctx context.Context, d model.DeploymentID) (model.Deployment, error)
	Recent(ctx context.Context, id model.ServiceID, n int) ([]model.Deployment, error)
	Succeeded(ctx context.Context, id model.ServiceID) ([]model.DeploymentID, error)
}

// DeployHistoryStore는 배포 기록을 저장한다.
type DeployHistoryStore interface {
	Start(ctx context.Context, id model.ServiceID, why model.DeployReason) (model.DeploymentID, error)
	SaveLog(ctx context.Context, d model.DeploymentID, log string) error
	Finish(ctx context.Context, d model.Deployment) error
	CloseInterrupted(ctx context.Context, note string) (int, error)
}

// ── F. push 받아 배포하기 ─────────────────────

// HookReceiver는 웹훅을 확인하고 배포를 맡기거나 거절한다.
type HookReceiver interface {
	Receive(ctx context.Context, id model.ServiceID, r model.HookRequest) model.HookResult
}

// SignatureChecker는 웹훅이 진짜 보낸 곳에서 왔는지 본다. 자기 방식이 아니면 CanCheck가 거짓 — 다음 것이 본다.
type SignatureChecker interface {
	CanCheck(r model.HookRequest) bool
	IsGenuine(r model.HookRequest, secret string) bool
}

// BranchFilter는 등록한 브랜치의 push인지 본다.
type BranchFilter interface {
	Wanted(r model.HookRequest, s model.Service) (ok bool, why string)
}

// HookLogStore는 웹훅을 받은 기록을 저장한다.
type HookLogStore interface {
	Save(ctx context.Context, id model.ServiceID, l model.HookLog) error
}

// ── G. 웹서버 맞추기 ─────────────────────────

// SiteMapBuilder는 서비스·관리 주소로 "주소마다 어디로 보낼지" 사이트 지도를 만든다.
type SiteMapBuilder interface {
	Build(ctx context.Context) (model.SiteMap, error)
}

// WebServerSync는 사이트 지도를 웹서버에 맞춘다.
type WebServerSync interface {
	SyncNow(ctx context.Context) error
	Status() model.WebServerStatus
}

// ConfigWriter는 사이트 지도를 웹서버 설정으로 쓴다.
type ConfigWriter interface {
	Write(m model.SiteMap) ([]byte, error)
}

// ConfigSender는 설정을 웹서버에 보낸다.
type ConfigSender interface {
	Send(ctx context.Context, config []byte) error
	Ping(ctx context.Context) error
}

// CertificateReader는 웹서버가 받아 둔 인증서를 읽는다. 공개 인증서만 열고 키 파일은 열지 않는다.
// 읽지 못하면 오류와 함께 빈 목록 — 쓰는 쪽은 "인증서 없음"으로 본다 (HTTP로라도 열리는 쪽으로 무너진다).
type CertificateReader interface {
	Read(ctx context.Context) (model.Certificates, error)
}

// ── I. 웹서버 설정 정하기 ─────────────────────

// WebSettingsEditor는 서비스의 웹서버 설정을 적용한다. 저장한 뒤 바로 웹서버에 맞춰 보고,
// 웹서버가 거절하면 저장을 되돌리고 model.RefusedError로 그 이유를 돌려준다.
// warnings는 적용은 됐지만 알려야 할 것 — 고급 칸에서 사이트 밖이라 버린 지시어 같은 것.
type WebSettingsEditor interface {
	Apply(ctx context.Context, id model.ServiceID, in model.WebSettingsInput) (warnings []string, err error)
}

// WebSettingsStore는 서비스의 웹서버 설정을 저장한다. 없으면 빈 설정이다.
type WebSettingsStore interface {
	Get(ctx context.Context, id model.ServiceID) (model.WebSettings, error)
	Set(ctx context.Context, id model.ServiceID, s model.WebSettings) error
}

// NginxTranslator는 nginx server 블록을 웹서버 설정 칸으로 옮긴다. 저장하지 않는다 — 화면이 미리 보여주고 사람이 적용한다.
type NginxTranslator interface {
	Translate(text string) model.NginxImport
}

// LoginHasher는 기본 인증 비밀번호를 웹서버가 아는 해시(bcrypt)로 만든다.
type LoginHasher interface {
	Hash(password string) (string, error)
}

// ── J. 지켜보기 ────────────────────────────

// HealthWatcher는 지켜보기가 마지막으로 본 것을 알려준다. 도는 고리(30초마다)는 만들 때 함께 돌려준다.
// 바뀔 때만 이벤트(ServiceDown·ServiceUp·WebServerDown …)를 낸다 — 보내는 일은 알리기가 듣고 한다.
type HealthWatcher interface {
	Snapshot() model.WatchSnapshot
}

// Diagnoser는 서비스 하나의 문제와 고칠 방법을 찾는다. 지켜보기 고리가 부르고, 결과는 Snapshot에 담긴다.
// 비싼 확인(다른 포트 찔러 보기, DNS 조회)은 여기에서만 한다 — 화면을 열 때는 하지 않는다.
type Diagnoser interface {
	Diagnose(ctx context.Context, s model.Service, states model.ContainerStates) []model.Finding
}

// ── K. 알리기 ─────────────────────────────

// AlertSettings는 알림 주소를 등록·삭제·시험하고, 보낸 결과를 보여준다. 화면에는 주소를 가린 모습만 준다.
type AlertSettings interface {
	Channels(ctx context.Context) ([]model.ChannelView, error)
	Add(ctx context.Context, in model.ChannelInput) error
	Remove(ctx context.Context, id model.ChannelID) error
	Test(ctx context.Context, id model.ChannelID) error
	Deliveries(ctx context.Context, n int) ([]model.Delivery, error)
}

// ChannelStore는 알림 주소를 저장한다. 주소와 시크릿은 비밀이다.
type ChannelStore interface {
	List(ctx context.Context) ([]model.AlertChannel, error)
	Add(ctx context.Context, c model.AlertChannel) (model.ChannelID, error)
	Remove(ctx context.Context, id model.ChannelID) error
}

// DeliveryLog는 보낸 결과를 남긴다 (최근 것만).
type DeliveryLog interface {
	Save(ctx context.Context, d model.Delivery) error
	Recent(ctx context.Context, n int) ([]model.Delivery, error)
}

// ── H. 보여주기 ────────────────────────────

// ServiceViewer는 화면에 보여줄 서비스 모습을 모은다. 비밀(토큰·비밀번호)은 담지 않는다.
type ServiceViewer interface {
	Home(ctx context.Context) (model.HomeView, error)
	Detail(ctx context.Context, id model.ServiceID) (model.ServiceView, error)
	Deployment(ctx context.Context, id model.ServiceID, d model.DeploymentID) (model.DeploymentView, error)
	Logs(ctx context.Context, id model.ServiceID, f model.LogFilter) (model.LogsView, error) // 앱 출력과 요청을 시간순으로
}

// ServerStats는 서버의 CPU·메모리·디스크를 알려준다.
type ServerStats interface {
	Now() model.ServerSnapshot
}

// ── 도구 ─────────────────────────────────

// ImageBuilder는 빌드할 폴더(묶은 것)로 이미지를 만든다.
type ImageBuilder interface {
	Build(ctx context.Context, buildFolder io.Reader, tag string, log io.Writer) (model.ImageID, error)
}

// ImagePuller는 이미지를 받고 그 ID·열어둔 포트를 알려준다.
type ImagePuller interface {
	Pull(ctx context.Context, ref model.ImageRef, log io.Writer) (model.ImageDetails, error)
}

// ImageCleaner는 이미지가 있는지 보고, 지운다.
type ImageCleaner interface {
	Exists(ctx context.Context, id model.ImageID) bool
	Remove(ctx context.Context, ref string) error
}

// ContainerStarter는 컨테이너를 만들어 띄우고, 앱 네트워크에서의 IP를 알려준다 (네트워크가 없으면 만든다).
type ContainerStarter interface {
	Start(ctx context.Context, spec model.ContainerSpec) (ip string, err error)
}

// ContainerRemover는 컨테이너를 없앤다.
type ContainerRemover interface {
	Remove(ctx context.Context, name string) error
}

// ContainerSwitch는 컨테이너를 멈추고 켠다.
type ContainerSwitch interface {
	TurnOff(ctx context.Context, name string) error
	TurnOn(ctx context.Context, name string) error
}

// ContainerWatcher는 컨테이너 상태·로그, 이 서비스의 컨테이너 목록을 본다.
type ContainerWatcher interface {
	All(ctx context.Context) (model.ContainerStates, error)
	One(ctx context.Context, name string) (model.ContainerState, error)
	Logs(ctx context.Context, name string, lines int) (string, error)
	BelongingTo(ctx context.Context, id model.ServiceID) ([]string, error)
}

// CodeDownloader는 저장소 코드를 내려받는다.
type CodeDownloader interface {
	Download(ctx context.Context, from model.CodeSource, into string, log io.Writer) (model.Commit, error)
}

// WorkFolder는 잠깐 쓸 작업 폴더를 빌려주고, 그 안의 경로를 안전하게 찾아 준다.
type WorkFolder interface {
	Borrow(name string) (path string, giveBack func(), err error)
	Inside(root string, p model.FolderPath) (string, error) // 심볼릭 링크로 밖에 나가면 실패
}

// BuildContextPacker는 빌드할 폴더를 묶는다 (.git·.dockerignore 제외).
type BuildContextPacker interface {
	Pack(folder string) io.ReadCloser
}

// DockerfilePortReader는 Dockerfile의 EXPOSE에서 앱 포트를 찾는다. Dockerfile이 없으면 found가 거짓.
type DockerfilePortReader interface {
	Ports(folder string) (ports []model.Port, found bool)
}

// SiteFiles는 정적 사이트 파일을 올리고·복사하고·지운다 (숨김 파일 제외).
type SiteFiles interface {
	Publish(from string, to model.SiteFolder) (count int, err error)
	Copy(from, to model.SiteFolder) error
	Exists(f model.SiteFolder) bool
	Prune(site model.ServiceName, keep int, live model.DeploymentID)
	RemoveSite(site model.ServiceName) error
}

// ContainerLogReader는 컨테이너가 찍은 최근 n줄을 시각과 함께 읽는다.
type ContainerLogReader interface {
	Recent(ctx context.Context, container string, n int) ([]model.LogLine, error)
}

// AccessLogReader는 웹서버가 남긴 요청 기록에서 그 주소들의 최근 n개를 읽는다. 아직 기록이 없으면 빈 목록.
type AccessLogReader interface {
	Recent(ctx context.Context, hosts []string, n int) ([]model.LogLine, error)
}

// AlertSender는 알림 하나를 그 주소의 형식(Discord·Slack·일반 JSON)으로 보낸다. 오래 걸리면 끊는다.
type AlertSender interface {
	Send(ctx context.Context, ch model.AlertChannel, a model.Alert) error
}

// SnippetCompiler는 고급 칸의 Caddyfile 지시어(사이트 블록 안쪽)를 웹서버 형식으로 바꾼다.
// 그 사이트의 경로 처리만 꺼내고, 그 밖(TLS·관리·포트·다른 사이트)은 ignored로 알려준다.
type SnippetCompiler interface {
	Compile(ctx context.Context, caddyfile string) (compiled []byte, ignored []string, err error)
}

// PortChecker는 그 주소의 포트가 응답하는지 본다.
type PortChecker interface {
	Answers(ctx context.Context, host string, port model.Port) error
}

// DNSChecker는 도메인이 이 서버를 가리키는지 본다.
type DNSChecker interface {
	PointsHere(ctx context.Context, d model.DomainName, thisServer string) model.DNSAnswer
}

// EventPublisher는 일어난 일을 알린다.
type EventPublisher interface {
	Publish(e model.Event)
}

// EventSubscriber는 일어난 일을 듣는다.
type EventSubscriber interface {
	Subscribe(listen func(model.Event))
}

// Clock은 지금 시각이다.
type Clock interface {
	Now() time.Time
}

// RandomTokens는 난수 문자열(16진수)이다.
type RandomTokens interface {
	New(bytes int) string
}
