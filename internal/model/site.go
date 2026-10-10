package model

import "time"

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
}

// SiteMap은 웹서버에 줄 지도다 — 주소마다 어디로 보내고, HTTPS를 쓰는지.
type SiteMap struct {
	AdminSocket   string   // 웹서버 관리 소켓 — 그린 설정에 항상 들어간다
	AdminHosts    []string // 관리 화면 주소
	AdminUpstream string   // 관리 화면이 듣는 곳
	AdminHTTPS    bool
	OpenFallback  bool // 모르는 주소로 :80에 온 요청을 관리 화면으로 — 첫 설정 전이나 관리 주소가 없을 때
	ACMEEmail     string
	InternalTLS   bool // 개발용 내부 인증서
	Sites         []Site
}

// WebServerStatus는 웹서버에 닿는지다.
type WebServerStatus struct {
	Connected bool
	LastError string
	AppliedAt time.Time
}

// CertificateState는 도메인 하나의 인증서 상태다 (M4).
type CertificateState struct {
	Issued   bool
	NotAfter time.Time
	Issuer   string
	Failed   string // 실패했다면 사람 말 이유
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
