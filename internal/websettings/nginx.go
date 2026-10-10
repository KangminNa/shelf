package websettings

import (
	"net/url"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// NginxReader는 nginx server 블록(또는 NPM "Advanced" 칸처럼 지시어만 있는 글)을 웹서버 설정 칸으로 옮긴다.
// 아는 지시어만 옮기고, 나머지는 줄 번호와 이유를 붙여 "옮기지 못한 줄"로 돌려준다 — 조용히 버리지 않는다.
// 엔진이 Caddy라 nginx 설정은 실행할 수 없다. 옮기는 건 한 번이고, 옮긴 결과는 일반 칸이 된다.
type NginxReader struct{}

const (
	whyAddress   = "주소와 포트는 Naru가 정해요 — 주소는 서비스에 붙이세요"
	whyHTTPS     = "HTTPS는 Naru가 알아서 해요"
	whyGzip      = "압축은 늘 켜져 있어요"
	whyAuth      = "비밀번호 보호는 아이디·비밀번호를 새로 정하세요"
	whyAuthFile  = "비밀번호 파일은 옮길 수 없어요 — 아이디·비밀번호를 새로 정하세요"
	whyProxy     = "Naru가 알아서 해요 (원래 Host·주소를 그대로 넘겨요)"
	whyRoot      = "기본 목적지는 서비스가 정해요 — 이 location은 옮기지 않았어요"
	whyNoProxy   = "proxy_pass가 없는 location은 옮기지 못했어요"
	whyRegex     = "정규식·정확히 일치 location은 옮기지 못했어요 — 필요하면 고급 칸에 Caddy 문법으로"
	whyRewrite   = "경로를 바꿔 보내는 proxy_pass는 옮기지 못했어요 — 필요하면 고급 칸에 Caddy 문법으로"
	whyAllowOnly = "deny all이 없으면 nginx에서도 모두 들어와요 — 옮길 것이 없어요"
	whyDeny      = "특정 주소만 막는 설정은 옮기지 않아요 — 허용할 곳을 적는 방식만 있어요"
	whyStatic    = "파일 서빙은 정적 사이트로 올리세요"
	whyUnknown   = "옮기지 못했어요 — 필요하면 고급 칸에 Caddy 문법으로 적어 주세요"
)

func (NginxReader) Translate(text string) model.NginxImport {
	var out model.NginxImport
	body := serverBody(parseNginx(text))
	var allows []directive
	denyAll := false
	skip := func(d directive, why string) {
		out.Skipped = append(out.Skipped, model.SkippedLine{Line: d.line, Text: d.text(), Why: why})
	}
	for _, d := range body {
		switch {
		case d.name == "add_header" && len(d.args) >= 2:
			if h, err := model.ParseHeaderLines(d.args[0] + ": " + d.args[1]); err == nil {
				out.Input.Headers = append(out.Input.Headers, h...)
			} else {
				skip(d, whyUnknown)
			}
		case d.name == "allow" && len(d.args) == 1 && d.args[0] != "all":
			allows = append(allows, d)
		case d.name == "deny" && len(d.args) == 1 && d.args[0] == "all":
			denyAll = true
		case d.name == "deny":
			skip(d, whyDeny)
		case d.name == "return" && len(d.args) >= 1 && d.args[0] == "503":
			out.Input.Maintenance = true
		case d.name == "location":
			if r, why := locationRoute(d); why != "" {
				skip(d, why)
			} else {
				out.Input.Paths = append(out.Input.Paths, r)
			}
		case d.name == "listen" || d.name == "server_name":
			skip(d, whyAddress)
		case strings.HasPrefix(d.name, "ssl") || d.name == "http2":
			skip(d, whyHTTPS)
		case strings.HasPrefix(d.name, "gzip"):
			skip(d, whyGzip)
		case d.name == "auth_basic":
			skip(d, whyAuth)
		case d.name == "auth_basic_user_file":
			skip(d, whyAuthFile)
		case strings.HasPrefix(d.name, "proxy_"):
			skip(d, whyProxy)
		case d.name == "root" || d.name == "index" || d.name == "try_files":
			skip(d, whyStatic)
		default:
			skip(d, whyUnknown)
		}
	}
	for _, d := range allows {
		if !denyAll {
			skip(d, whyAllowOnly)
			continue
		}
		if ips, err := model.ParseIPLines(d.args[0]); err == nil {
			out.Input.AllowFrom = append(out.Input.AllowFrom, ips...)
		} else {
			skip(d, whyUnknown)
		}
	}
	return out
}

// locationRoute는 "location /경로 { proxy_pass … }"를 경로 규칙으로 옮긴다. 못 옮기면 이유를 돌려준다.
func locationRoute(d directive) (model.PathRouteInput, string) {
	args := d.args
	if len(args) == 2 && args[0] == "^~" {
		args = args[1:]
	}
	if len(args) != 1 || !strings.HasPrefix(args[0], "/") {
		return model.PathRouteInput{}, whyRegex
	}
	if strings.Trim(args[0], "/") == "" {
		return model.PathRouteInput{}, whyRoot
	}
	prefix, err := model.ParsePathPrefix(args[0])
	if err != nil {
		return model.PathRouteInput{}, whyRegex
	}
	for _, inner := range d.block {
		if inner.name != "proxy_pass" || len(inner.args) != 1 {
			continue
		}
		target, strip, ok := proxyTarget(inner.args[0])
		if !ok {
			return model.PathRouteInput{}, whyRewrite
		}
		return model.PathRouteInput{Prefix: prefix, Target: target, StripPrefix: strip}, ""
	}
	return model.PathRouteInput{}, whyNoProxy
}

// proxyTarget은 proxy_pass 주소를 "호스트:포트"로 바꾼다. 경로가 "/"이면 nginx처럼 앞부분을 떼고 보낸다.
func proxyTarget(raw string) (target string, strip, ok bool) {
	if strings.Contains(raw, "$") {
		return "", false, false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false, false
	}
	switch u.Path {
	case "":
	case "/":
		strip = true
	default:
		return "", false, false
	}
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	target = host + ":" + port
	if u.Scheme == "https" {
		target = "https://" + target
	}
	return target, strip, true
}

// ── nginx 글 읽기 ─────────────────────────────

// directive는 nginx 지시어 하나다 — "이름 인자…;" 또는 "이름 인자… { … }".
type directive struct {
	name  string
	args  []string
	line  int
	block []directive
}

func (d directive) text() string {
	t := strings.TrimSpace(d.name + " " + strings.Join(d.args, " "))
	if d.block != nil {
		return t + " { … }"
	}
	return t + ";"
}

// serverBody는 server 블록 안쪽이다. server가 없으면(지시어만 붙여 넣었으면) 전체를 그대로 쓴다.
func serverBody(all []directive) []directive {
	for _, d := range all {
		if d.name == "server" {
			return d.block
		}
		if d.name == "http" {
			if inner := serverBody(d.block); inner != nil {
				return inner
			}
		}
	}
	return all
}

type token struct {
	text   string
	line   int
	quoted bool
}

// tokens는 글을 낱말·따옴표 글·{ } ;로 나눈다. # 뒤는 주석이다. 닫히지 않은 따옴표는 글 끝까지다.
func tokens(text string) []token {
	var out []token
	line := 1
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case c == '{' || c == '}' || c == ';':
			out = append(out, token{text: string(c), line: line})
			i++
		case c == '"' || c == '\'':
			start, j := line, i+1
			var b strings.Builder
			for j < len(text) && text[j] != c {
				if text[j] == '\\' && j+1 < len(text) {
					j++
				}
				if text[j] == '\n' {
					line++
				}
				b.WriteByte(text[j])
				j++
			}
			out = append(out, token{text: b.String(), line: start, quoted: true})
			i = j + 1
		default:
			j := i
			for j < len(text) && !strings.ContainsRune(" \t\r\n{};#\"'", rune(text[j])) {
				j++
			}
			out = append(out, token{text: text[i:j], line: line})
			i = j
		}
	}
	return out
}

// parseNginx는 낱말들을 지시어 나무로 묶는다. 틀린 글도 끝까지 읽는다 — 짝이 안 맞는 }는 버리고, 열린 블록은 글 끝에서 닫는다.
func parseNginx(text string) []directive {
	toks := tokens(text)
	pos := 0
	var block func(depth int) []directive
	block = func(depth int) []directive {
		var out []directive
		for pos < len(toks) {
			t := toks[pos]
			pos++
			if !t.quoted && t.text == "}" {
				if depth > 0 {
					return out
				}
				continue
			}
			if !t.quoted && (t.text == ";" || t.text == "{") {
				continue
			}
			d := directive{name: t.text, line: t.line}
			for pos < len(toks) {
				a := toks[pos]
				if !a.quoted && (a.text == ";" || a.text == "}") {
					if a.text == ";" {
						pos++
					}
					break
				}
				pos++
				if !a.quoted && a.text == "{" {
					d.block = block(depth + 1)
					if d.block == nil {
						d.block = []directive{}
					}
					break
				}
				d.args = append(d.args, a.text)
			}
			out = append(out, d)
		}
		return out
	}
	return block(0)
}
