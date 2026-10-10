package model

import (
	"net/netip"
	"regexp"
	"strings"
)

// ── 웹서버 설정 (서비스마다) ─────────────────────

// WebSettings는 서비스 하나의 웹서버 설정이다. 그 서비스의 모든 주소에 똑같이 적용된다.
// 한 요청이 지나가는 순서: IP 제한 → 점검 중 → 비밀번호 → 헤더·압축 → 고급 → 경로별 연결 → 서비스 기본 목적지.
type WebSettings struct {
	Headers     []HeaderRule
	AllowFrom   []netip.Prefix // 비면 누구나
	Login       BasicLogin     // User가 비면 끔
	Maintenance bool
	Paths       []PathRoute
	Advanced    string // 사용자가 쓴 Caddyfile 지시어 — 화면에 그대로 보여준다
	Compiled    []byte // Advanced를 웹서버 형식으로 바꾼 것 — 쓰기만 하고 열어보지 않는다
}

// HeaderRule은 응답 헤더 하나다. Value가 비면 그 헤더를 지운다.
type HeaderRule struct {
	Name  string
	Value string
}

// BasicLogin은 기본 인증 계정이다. Hash는 웹서버가 아는 bcrypt — 화면 계층은 이 타입을 쓰지 않는다 (R10).
type BasicLogin struct {
	User string
	Hash string
}

// PathRoute는 경로 하나를 다른 곳으로 보내는 규칙이다. Service와 External 중 하나만 채워진다.
type PathRoute struct {
	Prefix      PathPrefix
	Service     ServiceID
	External    ExternalAddress
	StripPrefix bool // 보낼 때 경로 앞부분을 뗀다 (/api/users → /users)
}

// WebSettingsInput은 화면에서 받은 웹서버 설정이다. 비밀번호는 원문이고, 저장 전에 해시한다.
type WebSettingsInput struct {
	Headers     []HeaderRule
	AllowFrom   []netip.Prefix
	LoginUser   string
	Password    string // 비면 지금 비밀번호 그대로
	RemoveLogin bool
	Maintenance bool
	Paths       []PathRouteInput
	Advanced    string
}

// PathRouteInput은 화면에서 받은 경로 규칙이다. Target은 서비스 이름이나 "호스트:포트".
type PathRouteInput struct {
	Prefix      PathPrefix
	Target      string
	StripPrefix bool
}

// WebSettingsView는 화면에 보일 웹서버 설정이다. 비밀번호 해시는 없고, 있는지만.
type WebSettingsView struct {
	Headers     []HeaderRule
	AllowFrom   []netip.Prefix
	LoginUser   string
	HasPassword bool
	Maintenance bool
	Paths       []PathRouteInput
	Advanced    string
	SentHereBy  []string // 경로별 연결로 이 서비스에 요청을 보내는 다른 서비스들
}

// NginxImport는 nginx 설정을 옮긴 결과 — 화면에 채울 값과 옮기지 못한 줄.
type NginxImport struct {
	Input   WebSettingsInput
	Skipped []SkippedLine
}

// SkippedLine은 옮기지 못한(또는 옮길 필요가 없는) nginx 설정 한 줄과 그 이유다.
type SkippedLine struct {
	Line int
	Text string
	Why  string
}

// SiteSettings는 사이트 지도에 실리는 웹서버 설정이다 — 경로의 목적지는 이미 실제 주소로 바뀌어 있다.
type SiteSettings struct {
	Headers     []HeaderRule
	AllowFrom   []netip.Prefix
	Login       BasicLogin
	Maintenance bool
	Paths       []SitePath
	Compiled    []byte
}

// SitePath는 사이트 지도의 경로 규칙 하나다.
type SitePath struct {
	Prefix      PathPrefix
	Destination Destination
	StripPrefix bool
}

// RefusedError는 웹서버가 설정을 받아들이지 않았다는 뜻이다. Reason은 웹서버가 한 말 그대로.
type RefusedError struct{ Reason string }

func (e RefusedError) Error() string { return "the web server refused the settings: " + e.Reason }

// ── 값 ──────────────────────────────────

// PathPrefix는 "/api" 같은 경로 앞부분이다. "/"로 시작하고, 끝의 "/"는 떼고, ".."·"*"·빈칸이 없다. "/" 하나는 안 된다(그건 기본 목적지다).
type PathPrefix struct{ v string }

var pathPrefixPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~@%+-]+)+$`)

func ParsePathPrefix(s string) (PathPrefix, error) {
	p := strings.TrimRight(strings.TrimSpace(s), "/")
	if len(p) > 200 || !pathPrefixPattern.MatchString(p) || strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") || strings.Contains(p, "/./") {
		return PathPrefix{}, InputError{Field: "paths", Code: "route"}
	}
	return PathPrefix{p}, nil
}

func (p PathPrefix) String() string               { return p.v }
func (p PathPrefix) MarshalText() ([]byte, error) { return []byte(p.v), nil }
func (p *PathPrefix) UnmarshalText(b []byte) error {
	v, err := ParsePathPrefix(string(b))
	*p = v
	return err
}

// ExternalAddress를 저장할 때 — 다시 읽을 때도 모양을 확인한다.
func (a ExternalAddress) MarshalText() ([]byte, error) { return []byte(a.v), nil }
func (a *ExternalAddress) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*a = ExternalAddress{}
		return nil
	}
	v, err := ParseExternalAddress(string(b))
	*a = v
	return err
}

// ── 화면 글자에서 읽기 (한 줄에 하나) ─────────────

var headerNamePattern = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]{1,100}$")

// ParseHeaderLines는 "이름: 값" 줄들을 읽는다. 값이 비면 그 헤더를 지운다는 뜻이다.
func ParseHeaderLines(text string) ([]HeaderRule, error) {
	var out []HeaderRule
	for _, line := range lines(text) {
		name, value, ok := strings.Cut(line, ":")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || !headerNamePattern.MatchString(name) || strings.ContainsAny(value, "\r\n\x00") || len(value) > 2000 {
			return nil, InputError{Field: "headers", Code: "header"}
		}
		out = append(out, HeaderRule{Name: name, Value: value})
	}
	return out, nil
}

// ParseIPLines는 IP나 대역(192.168.0.0/24) 줄들을 읽는다. IP 하나는 그 IP만의 대역이 된다.
func ParseIPLines(text string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, line := range lines(text) {
		if p, err := netip.ParsePrefix(line); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(line)
		if err != nil {
			return nil, InputError{Field: "allow", Code: "ip"}
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// ParsePathLines는 "/경로 대상 [떼기]" 줄들을 읽는다. 대상은 서비스 이름이나 "호스트:포트".
func ParsePathLines(text string) ([]PathRouteInput, error) {
	var out []PathRouteInput
	seen := map[string]bool{}
	for _, line := range lines(text) {
		f := strings.Fields(line)
		if len(f) < 2 || len(f) > 3 {
			return nil, InputError{Field: "paths", Code: "route"}
		}
		prefix, err := ParsePathPrefix(f[0])
		if err != nil || seen[prefix.v] {
			return nil, InputError{Field: "paths", Code: "route"}
		}
		seen[prefix.v] = true
		r := PathRouteInput{Prefix: prefix, Target: f[1]}
		if len(f) == 3 {
			if f[2] != "떼기" && f[2] != "strip" {
				return nil, InputError{Field: "paths", Code: "route"}
			}
			r.StripPrefix = true
		}
		out = append(out, r)
	}
	return out, nil
}

// lines는 빈 줄과 # 주석을 뺀 줄들이다.
func lines(text string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}
