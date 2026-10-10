package web

import (
	"fmt"
	"net/http"
	"unicode"

	"github.com/KangminNa/naru/internal/model"
)

// card는 서비스 하나를 화면에 보여줄 모양이다.
type card struct {
	ID         model.ServiceID
	Name       string
	Initial    string
	Color      string
	KindKey    string
	Domain     string
	StateKey   string
	StateClass string // ok | warn | bad | muted
	Detail     string // Docker가 말하는 상태 ("Up 3 hours")
}

// colorFor는 서비스 이름마다 늘 같은 색 클래스(c0~c6)를 준다.
// 인라인 style은 쓰지 않는다 — CSP가 막고, 막는 게 맞다.
func colorFor(name string) string {
	var h uint32
	for _, r := range name {
		h = h*31 + uint32(r)
	}
	return fmt.Sprintf("c%d", h%7)
}

func initialOf(name string) string {
	for _, r := range name {
		return string(unicode.ToUpper(r))
	}
	return "?"
}

func cardOf(c model.ServiceCard) card {
	return card{
		ID: c.ID, Name: c.Name, Initial: initialOf(c.Name), Color: colorFor(c.Name), KindKey: "kind." + string(c.Kind), Domain: c.Domain,
		StateKey: "state." + c.Status.Key, StateClass: c.Status.Tone, Detail: c.Status.Detail,
	}
}

// ── 홈 ────────────────────────────────────

type homeScreen struct {
	AdminDomain string
	Host        model.ServerSnapshot
	Services    []card
	Engine      model.WebServerStatus
	DockerDown  bool
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	d := homeScreen{AdminDomain: s.adminDomain(r.Context()), Host: s.d.Stats.Now(), Engine: s.d.WebServer.Status()}
	h, err := s.d.Viewer.Home(r.Context())
	if err != nil {
		s.d.Log.Error("list services", "err", err)
	}
	d.DockerDown = h.DockerDown
	for _, c := range h.Cards {
		d.Services = append(d.Services, cardOf(c))
	}
	s.render(w, r, http.StatusOK, "home", view{Nav: "home", User: &u, OK: okKeys[r.URL.Query().Get("ok")], Data: d})
}
