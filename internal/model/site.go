package model

import (
	"strings"
	"time"
)

// Destination은 요청을 보낼 곳이다. 셋 중 하나만 채워진다.
type Destination struct {
	Address   string // "컨테이너:포트" 또는 외부 "호스트:포트" (https://면 TLS)
	Folder    string // 정적 사이트: 웹서버가 이 폴더를 직접 서빙한다
	Container bool   // 컨테이너로 도는 서비스인가 (멈추기·켜기가 된다)
}

// Site는 사이트 지도의 한 줄이다.
type Site struct {
	Hosts       []string
	Destination Destination
	HTTPS       bool
	HSTS        bool
	// RedirectHTTP는 :80으로 온 요청을 HTTPS로 넘기는가 — 쓸 수 있는 인증서가 있을 때만 참이다 (불변식 4).
	RedirectHTTP bool
	Settings     SiteSettings // 그 서비스의 웹서버 설정 (M5)
}

// SiteMap은 웹서버에 줄 지도다 — 주소마다 어디로 보내고, HTTPS를 쓰는지.
type SiteMap struct {
	AdminSocket   string   // 웹서버 관리 소켓 — 그린 설정에 항상 들어간다
	AdminHosts    []string // 관리 화면 주소
	AdminUpstream string   // 관리 화면이 듣는 곳
	AdminHTTPS    bool
	// AdminRedirectHTTP는 관리 주소의 :80 요청을 HTTPS로 넘기는가 — 인증서가 있을 때만 (없는데 넘기면 내 서버에서 잠긴다).
	AdminRedirectHTTP bool
	OpenFallback      bool   // 모르는 주소로 :80에 온 요청을 관리 화면으로 — 첫 설정 전이나 관리 주소가 없을 때
	HTTPSPort         int    // 바깥에서 본 HTTPS 포트. 0이면 443 (로컬 개발처럼 다른 포트로 열었을 때만 적는다)
	Storage           string // 웹서버가 인증서를 둘 폴더. 비면 웹서버 기본값
	AccessLog         string // 접근 로그 파일 (웹서버가 본 경로). 비면 남기지 않는다. 관리 주소의 요청은 남기지 않는다
	ACMEEmail         string
	InternalTLS       bool // 개발용 내부 인증서
	Sites             []Site
}

// WebServerStatus는 웹서버에 닿는지다.
type WebServerStatus struct {
	Connected bool
	LastError string
	AppliedAt time.Time
}

// CertificateState는 도메인 하나의 인증서 상태다.
type CertificateState struct {
	Issued    bool
	NotBefore time.Time
	NotAfter  time.Time
	Issuer    string
}

// Usable은 지금 쓸 수 있는 인증서가 있는가 — 있고, 아직 끝나지 않았다.
func (s CertificateState) Usable(now time.Time) bool { return s.Issued && now.Before(s.NotAfter) }

// EndsSoon은 남은 기간이 전체 수명의 1/6보다 적은가 (90일짜리면 15일, 개발용 내부 인증서 12시간짜리면 2시간).
// 웹서버는 1/3이 남으면 갱신하니, 이 안으로 들어왔다면 갱신이 막힌 것이다. 수명을 모르면 14일로 본다.
func (s CertificateState) EndsSoon(now time.Time) bool {
	window := 14 * 24 * time.Hour
	if life := s.NotAfter.Sub(s.NotBefore); !s.NotBefore.IsZero() && life > 0 {
		window = life / 6
	}
	return s.Usable(now) && s.NotAfter.Sub(now) < window
}

// Certificate는 웹서버가 받아 둔 인증서 하나다 — 공개 정보만 담는다.
type Certificate struct {
	Names     []string // 인증서가 덮는 이름 (SAN). "*.example.com" 포함
	Issuer    string
	NotBefore time.Time
	NotAfter  time.Time
}

// Certificates는 읽어 온 인증서 전부다.
type Certificates []Certificate

// For는 그 도메인을 지금 덮는 인증서 중 가장 오래 가는 것의 상태다.
// 와일드카드(*.example.com)는 한 단계만 덮는다 — a.example.com은 되고 a.b.example.com과 example.com은 안 된다.
func (c Certificates) For(d DomainName, now time.Time) CertificateState {
	var best CertificateState
	for _, cert := range c {
		if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) || !cert.covers(d.String()) {
			continue
		}
		if !best.Issued || cert.NotAfter.After(best.NotAfter) {
			best = CertificateState{Issued: true, NotBefore: cert.NotBefore, NotAfter: cert.NotAfter, Issuer: cert.Issuer}
		}
	}
	return best
}

func (c Certificate) covers(host string) bool {
	host = strings.ToLower(host)
	for _, n := range c.Names {
		n = strings.ToLower(n)
		if n == host {
			return true
		}
		if rest, ok := strings.CutPrefix(n, "*."); ok {
			if label, parent, found := strings.Cut(host, "."); found && label != "" && parent == rest {
				return true
			}
		}
	}
	return false
}

// DNSAnswer는 도메인이 이 서버를 가리키는지의 답이다.
type DNSAnswer struct {
	Domain   string
	IPs      []string
	Resolves bool
	Expected string // 이 서버의 IP. 모르면 비어 있다
	Matches  bool
}

// IPList는 화면에 보여줄 IP 목록이다.
func (a DNSAnswer) IPList() string {
	out := ""
	for i, ip := range a.IPs {
		if i > 0 {
			out += ", "
		}
		out += ip
	}
	return out
}

// SetBy는 설정 값을 누가 정했는가다.
type SetBy string

const (
	SetByEnv    SetBy = "env"
	SetByScreen SetBy = "screen"
	SetByNobody SetBy = ""
)

// 서버 설정의 이름. 값은 SettingStore에 글자로 저장된다.
const (
	SettingAdminDomain = "admin_domain"
	SettingACMEEmail   = "acme_email"
	SettingSetupDone   = "setup_done"
)
