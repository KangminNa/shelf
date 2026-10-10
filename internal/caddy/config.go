// Package caddy는 웹서버 엔진(Caddy)에 닿는 도구다 — 설정 쓰기, 보내기, 보내기 전 확인.
// 설정의 원본은 Naru DB 하나다. Caddy 설정은 매번 통째로 그려서 교체한다 — 부분 수정은 하지 않는다.
package caddy

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

const (
	deniedBody      = "Naru: 이 주소는 허용된 곳에서만 열 수 있어요. / Access is limited to allowed addresses.\n"
	maintenanceBody = "Naru: 점검 중이에요. 잠시 뒤에 다시 와 주세요. / Down for maintenance — please come back soon.\n"
)

const notFoundBody = "Naru: 이 주소에 연결된 서비스가 없어요. / No service is configured for this address.\n"

// HSTS 기간 — 2년 (v1과 같다)
const hstsValue = "max-age=63072000"

type caddyConfig struct {
	Admin struct {
		Listen string `json:"listen"`
	} `json:"admin"`
	Storage *module        `json:"storage,omitempty"`
	Logging *loggingConfig `json:"logging,omitempty"`
	Apps    struct {
		HTTP struct {
			Servers map[string]server `json:"servers"`
		} `json:"http"`
		TLS *tlsApp `json:"tls,omitempty"`
	} `json:"apps"`
}

// loggingConfig는 접근 로그를 따로 파일로 남기는 설정이다 (기본 로그에는 섞지 않는다).
type loggingConfig struct {
	Logs map[string]map[string]any `json:"logs"`
}

// serverLogs는 서버의 접근 로그 설정이다 — 이름을 붙인 주소만 남기고, 나머지(관리 주소·주소 없는 요청)는 남기지 않는다.
type serverLogs struct {
	LoggerNames       map[string][]string `json:"logger_names"`
	SkipUnmappedHosts bool                `json:"skip_unmapped_hosts"`
}

type server struct {
	Logs      *serverLogs `json:"logs,omitempty"`
	Listen    []string    `json:"listen"`
	Routes    []route     `json:"routes"`
	AutoHTTPS struct {
		// 리다이렉트는 Caddy가 아니라 Naru가 그린다 — 인증서가 있을 때만 넘겨야 한다 (불변식 3·4)
		DisableRedirects bool `json:"disable_redirects"`
	} `json:"automatic_https"`
}

type route struct {
	Match    []match  `json:"match,omitempty"`
	Handle   []module `json:"handle"`
	Terminal bool     `json:"terminal,omitempty"`
}

// innerRoute는 사이트 안쪽(subroute)의 경로다 — 매처 모양이 여러 가지(not·path·client_ip)라 그대로 쓴다.
type innerRoute struct {
	Match    []map[string]any `json:"match,omitempty"`
	Handle   []module         `json:"handle"`
	Terminal bool             `json:"terminal,omitempty"`
}

type match struct {
	Host []string    `json:"host"`
	Not  []pathMatch `json:"not,omitempty"`
}

type pathMatch struct {
	Path []string `json:"path"`
}

// acmeChallenge는 인증서를 받고 갱신할 때 확인 기관이 :80으로 묻는 경로다. 이것은 넘기지 않는다.
const acmeChallenge = "/.well-known/acme-challenge/*"

type module map[string]any

type tlsApp struct {
	Automation struct {
		Policies []tlsPolicy `json:"policies"`
	} `json:"automation"`
}

type tlsPolicy struct {
	Issuers []module `json:"issuers"`
}

// SocketListen은 Caddy가 쓰는 관리 주소 표기다.
func SocketListen(socket string) string { return "unix/" + socket }

// JSONWriter는 사이트 지도를 Caddy JSON으로 쓴다. 같은 지도는 언제나 같은 바이트가 된다.
type JSONWriter struct{}

func (JSONWriter) Write(p model.SiteMap) ([]byte, error) {
	if p.AdminSocket == "" {
		return nil, errors.New("caddy: admin socket is required")
	}
	sites := append([]model.Site{}, p.Sites...)
	sort.Slice(sites, func(i, j int) bool { return first(sites[i].Hosts) < first(sites[j].Hosts) })

	seen := map[string]bool{}
	for _, h := range p.AdminHosts {
		seen[strings.ToLower(h)] = true
	}
	for _, s := range sites {
		if len(s.Hosts) == 0 {
			return nil, errors.New("caddy: a site needs at least one host")
		}
		for _, h := range s.Hosts {
			if seen[strings.ToLower(h)] {
				return nil, fmt.Errorf("caddy: %s is used twice", h)
			}
			seen[strings.ToLower(h)] = true
		}
	}

	var c caddyConfig
	c.Admin.Listen = SocketListen(p.AdminSocket)
	if p.Storage != "" {
		c.Storage = &module{"module": "file_system", "root": p.Storage}
	}
	var logs *serverLogs
	if p.AccessLog != "" {
		// 접근 로그는 서비스 주소만 남긴다. 관리 주소와 주소 없이 들어온 요청은 남기지 않는다 —
		// 웹훅의 ?secret=, 첫 설정의 ?token= 이 파일에 남지 않게 (실제 Caddy로 확인).
		logs = &serverLogs{LoggerNames: map[string][]string{}, SkipUnmappedHosts: true}
		for _, s := range sites {
			for _, h := range s.Hosts {
				logs.LoggerNames[strings.ToLower(h)] = []string{"access"}
			}
		}
		c.Logging = &loggingConfig{Logs: map[string]map[string]any{
			"default": {"exclude": []string{"http.log.access"}},
			"access": {
				"writer":  map[string]any{"output": "file", "filename": p.AccessLog, "roll_size_mb": 10, "roll_keep": 5},
				"encoder": map[string]any{"format": "json"},
				"include": []string{"http.log.access"},
			},
		}}
	}
	c.Apps.HTTP.Servers = map[string]server{}

	// :80 — 모든 주소. HTTPS로 넘기는 것은 지도가 그러라고 한 주소뿐이다 (인증서가 있을 때만 — 불변식 4).
	// 넘기는 주소도 HTTP 경로를 그대로 둔다 — 인증서 확인 요청은 넘기지 않고, 그 밖은 넘긴다.
	plain := server{Listen: []string{":80"}, Logs: logs}
	plain.AutoHTTPS.DisableRedirects = true
	if len(p.AdminHosts) > 0 {
		if p.AdminHTTPS && p.AdminRedirectHTTP {
			plain.Routes = append(plain.Routes, redirectRoute(p.AdminHosts, p.HTTPSPort))
		}
		plain.Routes = append(plain.Routes, hostRoute(p.AdminHosts, proxyTo(p.AdminUpstream)))
	}
	for _, s := range sites {
		if s.HTTPS && s.RedirectHTTP {
			plain.Routes = append(plain.Routes, redirectRoute(s.Hosts, p.HTTPSPort))
		}
		plain.Routes = append(plain.Routes, hostRoute(s.Hosts, site(s, false)))
	}
	if p.OpenFallback {
		plain.Routes = append(plain.Routes, route{Handle: []module{proxyTo(p.AdminUpstream)}})
	} else {
		plain.Routes = append(plain.Routes, route{Handle: []module{notFound()}})
	}
	c.Apps.HTTP.Servers["http"] = plain

	// :443 — HTTPS를 켠 주소만. 여기 있는 주소에 Caddy가 인증서를 받는다.
	secure := server{Listen: []string{":443"}, Logs: logs}
	secure.AutoHTTPS.DisableRedirects = true
	if p.AdminHTTPS && len(p.AdminHosts) > 0 {
		secure.Routes = append(secure.Routes, hostRoute(p.AdminHosts, proxyTo(p.AdminUpstream)))
	}
	for _, s := range sites {
		if s.HTTPS {
			secure.Routes = append(secure.Routes, hostRoute(s.Hosts, site(s, true)))
		}
	}
	if len(secure.Routes) > 0 {
		secure.Routes = append(secure.Routes, route{Handle: []module{notFound()}})
		c.Apps.HTTP.Servers["https"] = secure
	}

	if p.InternalTLS || p.ACMEEmail != "" {
		issuer := module{"module": "acme", "email": p.ACMEEmail}
		if p.InternalTLS {
			issuer = module{"module": "internal"}
		}
		c.Apps.TLS = &tlsApp{}
		c.Apps.TLS.Automation.Policies = []tlsPolicy{{Issuers: []module{issuer}}}
	}
	return json.MarshalIndent(c, "", "  ")
}

func hostRoute(hosts []string, handlers ...module) route {
	lower := make([]string, len(hosts))
	for i, h := range hosts {
		lower[i] = strings.ToLower(h)
	}
	return route{Match: []match{{Host: lower}}, Handle: handlers, Terminal: true}
}

// site는 사이트 하나의 처리 순서다 (v2-objects §4 I):
// IP 제한 → 점검 중 → 비밀번호 → 헤더·압축 → 고급 → 경로별 연결 → 서비스 기본 목적지.
// IP 제한·점검 중·비밀번호는 인증서 확인 경로를 빼고 건다 — 막으면 발급·갱신이 실패한다.
func site(s model.Site, secure bool) module {
	w := s.Settings
	var routes []innerRoute
	notChallenge := func(more ...map[string]any) []map[string]any {
		return []map[string]any{{"not": append(more, map[string]any{"path": []string{acmeChallenge}})}}
	}
	if len(w.AllowFrom) > 0 {
		ranges := make([]string, len(w.AllowFrom))
		for i, p := range w.AllowFrom {
			ranges[i] = p.String()
		}
		routes = append(routes, innerRoute{Match: notChallenge(map[string]any{"client_ip": map[string]any{"ranges": ranges}}),
			Handle: []module{plainText(403, deniedBody)}, Terminal: true})
	}
	if w.Maintenance {
		m := plainText(503, maintenanceBody)
		m["headers"].(map[string][]string)["Retry-After"] = []string{"300"}
		routes = append(routes, innerRoute{Match: notChallenge(), Handle: []module{m}, Terminal: true})
	}
	if w.Login.User != "" {
		routes = append(routes, innerRoute{Match: notChallenge(), Handle: []module{{
			"handler": "authentication",
			"providers": map[string]any{"http_basic": map[string]any{
				"accounts": []map[string]string{{"username": w.Login.User, "password": w.Login.Hash}},
				"hash":     map[string]string{"algorithm": "bcrypt"},
			}},
		}}})
	}
	set, del := map[string][]string{}, []string{}
	if secure && s.HSTS {
		set["Strict-Transport-Security"] = []string{hstsValue}
	}
	for _, h := range w.Headers {
		if h.Value == "" {
			del = append(del, h.Name)
		} else {
			set[h.Name] = append(set[h.Name], h.Value)
		}
	}
	early := []module{}
	if len(set) > 0 || len(del) > 0 {
		response := map[string]any{}
		if len(set) > 0 {
			response["set"] = set
		}
		if len(del) > 0 {
			response["delete"] = del
		}
		early = append(early, module{"handler": "headers", "response": response})
	}
	early = append(early, module{"handler": "encode", "encodings": map[string]any{"zstd": map[string]any{}, "gzip": map[string]any{}}, "prefer": []string{"zstd", "gzip"}})
	routes = append(routes, innerRoute{Handle: early})
	if len(w.Compiled) > 0 {
		routes = append(routes, innerRoute{Handle: []module{{"handler": "subroute", "routes": json.RawMessage(w.Compiled)}}})
	}
	for _, p := range w.Paths {
		hs := []module{}
		if p.StripPrefix {
			hs = append(hs, module{"handler": "rewrite", "strip_path_prefix": p.Prefix.String()})
		}
		hs = append(hs, serve(model.Site{Destination: p.Destination}))
		routes = append(routes, innerRoute{Match: []map[string]any{{"path": []string{p.Prefix.String(), p.Prefix.String() + "/*"}}}, Handle: hs, Terminal: true})
	}
	routes = append(routes, innerRoute{Handle: []module{serve(s)}})
	return module{"handler": "subroute", "routes": routes}
}

func plainText(status int, body string) module {
	return module{"handler": "static_response", "status_code": status,
		"headers": map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}}, "body": body}
}

// redirectRoute는 그 주소들의 :80 요청을 HTTPS로 넘긴다. 307 — 브라우저가 영구히 기억하지 않아
// HTTPS를 끄거나 인증서를 잃으면 바로 되돌릴 수 있다 (영구 고정은 HSTS로 따로 켠다).
func redirectRoute(hosts []string, port int) route {
	r := hostRoute(hosts, module{"handler": "static_response", "status_code": 307,
		"headers": map[string][]string{"Location": {httpsLocation(port)}}})
	r.Match[0].Not = []pathMatch{{Path: []string{acmeChallenge}}}
	return r
}

func httpsLocation(port int) string {
	if port == 0 || port == 443 {
		return "https://{http.request.host}{http.request.uri}"
	}
	return "https://{http.request.host}:" + strconv.Itoa(port) + "{http.request.uri}"
}

// serve는 사이트 하나를 어떻게 응답할지다 — 파일을 직접, 아니면 연결 대상으로.
func serve(s model.Site) module {
	if s.Destination.Folder != "" {
		return module{"handler": "file_server", "root": s.Destination.Folder}
	}
	return proxyTo(s.Destination.Address)
}

// proxyTo는 upstream으로 보낸다. Host 헤더는 Caddy가 원래 값을 그대로 넘긴다.
// 배포 중 새 컨테이너와 옛 컨테이너가 같은 이름으로 잠깐 함께 있다 — 연결이 거절되면 몇 초 동안 다시 시도해
// 응답하는 쪽으로 간다 (그래서 배포가 끊기지 않는다).
func proxyTo(upstream string) module {
	if upstream == "" {
		return module{"handler": "static_response", "status_code": 502,
			"headers": map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}},
			"body":    "Naru: 이 서비스는 연결할 곳이 없어요. / This service has nowhere to send requests.\n"}
	}
	h := module{"handler": "reverse_proxy"}
	if rest, ok := strings.CutPrefix(upstream, "https://"); ok {
		upstream = rest
		h["transport"] = map[string]any{"protocol": "http", "tls": map[string]any{}}
	}
	h["upstreams"] = []map[string]string{{"dial": upstream}}
	h["load_balancing"] = map[string]any{"try_duration": "5s", "try_interval": "250ms"}
	return h
}

func notFound() module {
	return module{"handler": "static_response", "status_code": 404,
		"headers": map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}},
		"body":    notFoundBody}
}

func first(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

// checkAdmin은 보내기 직전의 마지막 확인이다. 관리 소켓이 빠지거나 바뀐 설정을 보내면
// Caddy는 200으로 받아들이고 Naru는 제어를 영영 잃는다 (재시작해도 autosave에 남는다).
func checkAdmin(cfg []byte, socket string) error {
	var c struct {
		Admin struct {
			Listen string `json:"listen"`
		} `json:"admin"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		return fmt.Errorf("caddy: config is not JSON: %w", err)
	}
	if c.Admin.Listen != SocketListen(socket) {
		return fmt.Errorf("caddy: refusing to load a config whose admin listener is %q, not %q", c.Admin.Listen, SocketListen(socket))
	}
	return nil
}
