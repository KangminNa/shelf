// Package engine은 웹서버 엔진(Caddy)을 다룬다.
// 설정의 원본은 Naru DB 하나다. Caddy 설정은 매번 통째로 그려서 교체한다 — 부분 수정은 하지 않는다.
package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Site는 주소 묶음 하나와 그 요청을 보낼 곳이다.
type Site struct {
	Hosts    []string
	Upstream string // "host:port". "https://host:port"면 TLS로 붙는다. 비어 있으면 502
	HTTPS    bool   // 인증서를 받아 443에서도 서빙
	HSTS     bool
}

// Plan은 Caddy에 그릴 모든 것이다. Naru DB에서 만든다.
type Plan struct {
	AdminSocket   string   // 관리 소켓 경로. 렌더 결과에 항상 들어간다 (불변식 2)
	AdminHosts    []string // 관리 화면 주소
	AdminUpstream string   // 관리 화면이 듣는 곳 (예: naru:8080)
	AdminHTTPS    bool
	OpenFallback  bool   // 모르는 주소(IP 등)로 :80에 온 요청을 관리 화면으로 — 첫 설정과 관리 주소가 없을 때
	ACMEEmail     string // 인증서 연락처
	InternalTLS   bool   // 개발용: 공개 CA 대신 Caddy 내부 CA
	Sites         []Site
}

const notFoundBody = "Naru: 이 주소에 연결된 서비스가 없어요. / No service is configured for this address.\n"

// HSTS 기간 — 2년 (v1과 같다)
const hstsValue = "max-age=63072000"

type caddyConfig struct {
	Admin struct {
		Listen string `json:"listen"`
	} `json:"admin"`
	Apps struct {
		HTTP struct {
			Servers map[string]server `json:"servers"`
		} `json:"http"`
		TLS *tlsApp `json:"tls,omitempty"`
	} `json:"apps"`
}

type server struct {
	Listen    []string `json:"listen"`
	Routes    []route  `json:"routes"`
	AutoHTTPS struct {
		// 리다이렉트는 Caddy가 아니라 Naru가 그린다 — 인증서가 있을 때만 넘겨야 한다 (불변식 3·4)
		DisableRedirects bool `json:"disable_redirects"`
	} `json:"automatic_https"`
}

type route struct {
	Match    []match   `json:"match,omitempty"`
	Handle   []handler `json:"handle"`
	Terminal bool      `json:"terminal,omitempty"`
}

type match struct {
	Host []string `json:"host"`
}

type handler map[string]any

type tlsApp struct {
	Automation struct {
		Policies []tlsPolicy `json:"policies"`
	} `json:"automation"`
}

type tlsPolicy struct {
	Issuers []handler `json:"issuers"`
}

// SocketListen은 Caddy가 쓰는 관리 주소 표기다.
func SocketListen(socket string) string { return "unix/" + socket }

// Render는 Plan을 Caddy JSON으로 그린다. 같은 Plan은 언제나 같은 바이트가 된다.
func Render(p Plan) ([]byte, error) {
	if p.AdminSocket == "" {
		return nil, errors.New("engine: admin socket is required")
	}
	sites := append([]Site(nil), p.Sites...)
	sort.Slice(sites, func(i, j int) bool { return first(sites[i].Hosts) < first(sites[j].Hosts) })

	seen := map[string]bool{}
	for _, h := range p.AdminHosts {
		seen[strings.ToLower(h)] = true
	}
	for _, s := range sites {
		if len(s.Hosts) == 0 {
			return nil, errors.New("engine: a site needs at least one host")
		}
		for _, h := range s.Hosts {
			if seen[strings.ToLower(h)] {
				return nil, fmt.Errorf("engine: %s is used twice", h)
			}
			seen[strings.ToLower(h)] = true
		}
	}

	var c caddyConfig
	c.Admin.Listen = SocketListen(p.AdminSocket)
	c.Apps.HTTP.Servers = map[string]server{}

	// :80 — 모든 주소. 아직 HTTPS로 넘기지 않는다 (인증서가 확인되면 M4에서).
	plain := server{Listen: []string{":80"}}
	plain.AutoHTTPS.DisableRedirects = true
	if len(p.AdminHosts) > 0 {
		plain.Routes = append(plain.Routes, hostRoute(p.AdminHosts, proxyTo(p.AdminUpstream)))
	}
	for _, s := range sites {
		plain.Routes = append(plain.Routes, hostRoute(s.Hosts, proxyTo(s.Upstream)))
	}
	if p.OpenFallback {
		plain.Routes = append(plain.Routes, route{Handle: []handler{proxyTo(p.AdminUpstream)}})
	} else {
		plain.Routes = append(plain.Routes, route{Handle: []handler{notFound()}})
	}
	c.Apps.HTTP.Servers["http"] = plain

	// :443 — HTTPS를 켠 주소만. 여기 있는 주소에 Caddy가 인증서를 받는다.
	secure := server{Listen: []string{":443"}}
	secure.AutoHTTPS.DisableRedirects = true
	if p.AdminHTTPS && len(p.AdminHosts) > 0 {
		secure.Routes = append(secure.Routes, hostRoute(p.AdminHosts, proxyTo(p.AdminUpstream)))
	}
	for _, s := range sites {
		if !s.HTTPS {
			continue
		}
		hs := []handler{}
		if s.HSTS {
			hs = append(hs, handler{"handler": "headers", "response": map[string]any{"set": map[string][]string{"Strict-Transport-Security": {hstsValue}}}})
		}
		secure.Routes = append(secure.Routes, hostRoute(s.Hosts, append(hs, proxyTo(s.Upstream))...))
	}
	if len(secure.Routes) > 0 {
		secure.Routes = append(secure.Routes, route{Handle: []handler{notFound()}})
		c.Apps.HTTP.Servers["https"] = secure
	}

	if p.InternalTLS || p.ACMEEmail != "" {
		issuer := handler{"module": "acme", "email": p.ACMEEmail}
		if p.InternalTLS {
			issuer = handler{"module": "internal"}
		}
		c.Apps.TLS = &tlsApp{}
		c.Apps.TLS.Automation.Policies = []tlsPolicy{{Issuers: []handler{issuer}}}
	}
	return json.MarshalIndent(c, "", "  ")
}

func hostRoute(hosts []string, handlers ...handler) route {
	lower := make([]string, len(hosts))
	for i, h := range hosts {
		lower[i] = strings.ToLower(h)
	}
	return route{Match: []match{{Host: lower}}, Handle: handlers, Terminal: true}
}

// proxyTo는 upstream으로 보낸다. Host 헤더는 Caddy가 원래 값을 그대로 넘긴다.
func proxyTo(upstream string) handler {
	if upstream == "" {
		return handler{"handler": "static_response", "status_code": 502,
			"headers": map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}},
			"body":    "Naru: 이 서비스는 연결할 곳이 없어요. / This service has nowhere to send requests.\n"}
	}
	h := handler{"handler": "reverse_proxy"}
	if rest, ok := strings.CutPrefix(upstream, "https://"); ok {
		upstream = rest
		h["transport"] = map[string]any{"protocol": "http", "tls": map[string]any{}}
	}
	h["upstreams"] = []map[string]string{{"dial": upstream}}
	return h
}

func notFound() handler {
	return handler{"handler": "static_response", "status_code": 404,
		"headers": map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}},
		"body":    notFoundBody}
}

func first(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

// CheckAdmin은 보내기 직전의 마지막 확인이다. 관리 소켓이 빠지거나 바뀐 설정을 보내면
// Caddy는 200으로 받아들이고 Naru는 제어를 영영 잃는다 (재시작해도 autosave에 남는다).
func CheckAdmin(cfg []byte, socket string) error {
	var c struct {
		Admin struct {
			Listen string `json:"listen"`
		} `json:"admin"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		return fmt.Errorf("engine: config is not JSON: %w", err)
	}
	if c.Admin.Listen != SocketListen(socket) {
		return fmt.Errorf("engine: refusing to load a config whose admin listener is %q, not %q", c.Admin.Listen, SocketListen(socket))
	}
	return nil
}
