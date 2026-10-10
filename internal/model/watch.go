package model

import (
	"net/url"
	"strings"
	"time"
)

// ── 지켜보기 (M6) ───────────────────────────

// WatchSnapshot은 지켜보기가 마지막으로 본 것이다.
type WatchSnapshot struct {
	CheckedAt   time.Time
	Down        []DownService // 멈춘 컨테이너 서비스 (두 번 연속 문제였던 것)
	WebServerUp bool
	DockerUp    bool
	Findings    []Finding // 서비스·인증서의 문제 (웹서버·Docker에 닿는지는 화면이 지금 상태로 본다)
}

// FindingLevel은 문제의 무게다 — 무거운 것부터 보인다.
type FindingLevel int

const (
	FindingUrgent  FindingLevel = iota // 지금 요청이 실패한다 — 멈춤, 닿지 않음, 틀린 포트
	FindingWarning                     // 곧 문제가 된다 — 인증서, DNS
	FindingHint                        // 확인해 보면 좋다 — 웹훅
)

// Finding은 지켜보기가 찾은 문제 하나와 고칠 방법이다. 화면은 Key로 문구를 고르고 Args를 끼워 넣는다.
type Finding struct {
	Service ServiceID // 0이면 서버 전체 (관리 주소의 인증서 등)
	Name    string    // 서비스 이름
	Key     string    // 문구 키 — "find.port" · "find.dns.elsewhere" …
	Level   FindingLevel
	Args    []string // 문구에 끼울 값 — 포트·주소·IP·날짜
	Detail  string   // 기술적인 이유 (그대로 보인다)
	FixPort Port     // 0이 아니면 [포트를 이것으로 바꾸기]
}

// DownService는 멈춘 서비스 하나와 그 이유다.
type DownService struct {
	Service ServiceID
	Name    string
	Why     string // "crashed" · "restarting" · "missing" · "noanswer"
	Since   time.Time
}

// ── 알리기 (M6) ─────────────────────────────

type AlertLevel string

const (
	AlertProblem  AlertLevel = "problem"
	AlertRecovery AlertLevel = "recovery"
	AlertInfo     AlertLevel = "info"
)

// Alert는 알림 주소로 보낼 것 하나다. 비밀(환경 변수·토큰)은 담지 않는다.
type Alert struct {
	Event   string // "service.down" · "service.up" · "deploy.failed" · "certificate.ending" · "webserver.down" …
	Level   AlertLevel
	Title   string
	Detail  string
	Service string // 서비스 이름 (없으면 빈 값)
}

type ChannelID int64

// AlertChannel은 알림 주소 하나다. 주소 자체가 비밀이다 (Discord 웹훅 주소는 그것만으로 보낼 수 있다).
// 화면 계층은 이 타입을 쓰지 않는다 (R10) — ChannelView를 쓴다.
type AlertChannel struct {
	ID     ChannelID
	Name   string
	URL    string
	Secret string // 일반 JSON으로 보낼 때 HMAC 서명에 쓴다
}

// AlertURL은 알림을 보낼 http(s) 주소다. 사용자 정보(user:pass@)는 받지 않는다.
type AlertURL struct{ v string }

func ParseAlertURL(s string) (AlertURL, error) {
	s = strings.TrimSpace(s)
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || len(s) > 2000 {
		return AlertURL{}, InputError{Field: "url", Code: "alerturl"}
	}
	return AlertURL{s}, nil
}

func (a AlertURL) String() string { return a.v }

// AlertFormatOf는 그 주소가 받는 알림 모양이다 — "discord" · "slack" · "json".
func AlertFormatOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "json"
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case (host == "discord.com" || host == "discordapp.com") && strings.HasPrefix(u.Path, "/api/webhooks/"):
		return "discord"
	case host == "hooks.slack.com":
		return "slack"
	}
	return "json"
}

// AlertHostOf는 화면·오류에 쓸 호스트 이름이다 — 주소 전체(비밀)는 보여주지 않는다.
func AlertHostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return "?"
}

// ChannelInput은 화면에서 받은 알림 주소다.
type ChannelInput struct {
	Name   string
	URL    AlertURL
	Secret string
}

// ChannelView는 화면에 보일 알림 주소다 — 주소는 가리고(호스트까지만) 시크릿은 있는지만.
type ChannelView struct {
	ID        ChannelID
	Name      string
	Target    string // "discord.com/…"
	Format    string // "discord" · "slack" · "json"
	HasSecret bool
}

// Delivery는 보낸 결과 하나다.
type Delivery struct {
	Channel     ChannelID
	ChannelName string
	Event       string
	Title       string
	OK          bool
	Detail      string // 실패 이유 (HTTP 상태나 연결 오류)
	At          time.Time
}

// ── 지켜보기가 내는 일 ─────────────────────────

// ServiceDown: 컨테이너 서비스가 두 번 연속 문제였다.
type ServiceDown struct {
	Service ServiceID
	Name    string
	Why     string
}

// ServiceUp: 멈췄던 서비스가 다시 응답한다.
type ServiceUp struct {
	Service ServiceID
	Name    string
}

// WebServerDown · WebServerUp: 웹서버에 두 번 연속 닿지 않았다 · 다시 닿는다.
type WebServerDown struct{ Why string }
type WebServerUp struct{}

// DockerDown · DockerUp: Docker에 두 번 연속 닿지 않았다 · 다시 닿는다.
type DockerDown struct{ Why string }
type DockerUp struct{}

// CertificateEndingSoon: 인증서가 14일 안에 끝난다 (주소마다 하루 한 번).
type CertificateEndingSoon struct {
	Domain   string
	NotAfter time.Time
}

func (ServiceDown) event()           {}
func (ServiceUp) event()             {}
func (WebServerDown) event()         {}
func (WebServerUp) event()           {}
func (DockerDown) event()            {}
func (DockerUp) event()              {}
func (CertificateEndingSoon) event() {}
