package caddy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// snippetHost는 고급 칸의 지시어를 감싸는 가짜 사이트 이름이다. .invalid는 실제로 있을 수 없는 이름이다.
const snippetHost = "naru.invalid"

// AdaptCompiler는 고급 칸의 Caddyfile 지시어를 Caddy 관리 API의 /adapt로 JSON으로 바꾼다 (SnippetCompiler).
// Caddyfile을 Naru가 직접 해석하지 않는다 — Caddy 버전이 바뀌어도 어긋나지 않게.
// 결과에서 우리 사이트 안쪽의 경로 처리만 꺼낸다. TLS·관리·로그·포트·다른 사이트는 버리고 무엇을 버렸는지 알린다 —
// 그래서 고급 칸으로 관리 소켓(불변식 2)이나 인증서 정책을 바꿀 수 없다.
type AdaptCompiler struct{ s *SocketSender }

func NewAdaptCompiler(socket string) AdaptCompiler { return AdaptCompiler{NewSocketSender(socket)} }

func (c AdaptCompiler) Compile(ctx context.Context, caddyfile string) ([]byte, []string, error) {
	wrapped := snippetHost + " {\n" + caddyfile + "\n}\n"
	raw, err := c.s.do(ctx, http.MethodPost, "/adapt", "text/caddyfile", []byte(wrapped))
	if err != nil {
		return nil, nil, caddySaid(err)
	}
	var out struct {
		Result struct {
			Apps map[string]json.RawMessage `json:"apps"`
		} `json:"result"`
		Warnings []struct {
			Message string `json:"message"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, fmt.Errorf("caddy: /adapt: %w", err)
	}
	var ignored []string
	for _, w := range out.Warnings {
		// 들여쓰기 경고는 Naru가 지시어를 사이트 블록으로 감싸서 생긴다 — 사용자에게는 쓸모없는 말이다
		if !strings.Contains(w.Message, "not formatted") {
			ignored = append(ignored, w.Message)
		}
	}
	for name := range out.Result.Apps {
		if name != "http" {
			ignored = append(ignored, name+" — 이 설정은 Naru가 정해요 / Naru manages "+name)
		}
	}
	var http struct {
		Servers map[string]struct {
			Listen []string          `json:"listen"`
			Routes []json.RawMessage `json:"routes"`
		} `json:"servers"`
	}
	json.Unmarshal(out.Result.Apps["http"], &http)
	var compiled []byte
	for _, server := range http.Servers {
		for _, r := range server.Routes {
			var site struct {
				Match  []struct{ Host []string } `json:"match"`
				Handle []struct {
					Handler string          `json:"handler"`
					Routes  json.RawMessage `json:"routes"`
				} `json:"handle"`
			}
			json.Unmarshal(r, &site)
			hosts := []string{}
			for _, m := range site.Match {
				hosts = append(hosts, m.Host...)
			}
			if len(hosts) == 1 && hosts[0] == snippetHost && len(site.Handle) == 1 && site.Handle[0].Handler == "subroute" {
				compiled = site.Handle[0].Routes
				continue
			}
			ignored = append(ignored, "사이트 블록 밖에 쓴 것 / outside the site block: "+strings.Join(hosts, ", "))
		}
	}
	if compiled == nil {
		compiled = []byte("[]")
	}
	sort.Strings(ignored)
	return compiled, ignored, nil
}

// caddySaid는 Caddy가 돌려준 {"error": "..."}에서 사람이 읽을 문장만 꺼낸다 (/adapt·/load 모두).
func caddySaid(err error) error {
	msg := err.Error()
	if i := strings.Index(msg, "{"); i >= 0 {
		var body struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(msg[i:]), &body) == nil && body.Error != "" {
			msg := strings.TrimPrefix(body.Error, "adapting config using caddyfile adapter: ")
			return errors.New("caddy: " + strings.TrimPrefix(msg, "loading config: "))
		}
	}
	return err
}
