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
	Usage      *model.ResourceUsage
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
		StateKey: "state." + c.Status.Key, StateClass: c.Status.Tone, Detail: c.Status.Detail, Usage: c.Usage,
	}
}

// finding은 "주의가 필요한 것" 한 줄이다. 문구는 Key+".title"·".body"에 Args를 끼운다.
type finding struct {
	Service model.ServiceID
	Name    string // 비면 서비스 이름을 앞에 붙이지 않는다 (서버 전체의 것, 또는 서비스 화면 안)
	Key     string
	Args    []string
	Detail  string
	Tone    string // bad · warn · muted
	FixPort model.Port
	Link    string
	LinkKey string
}

func findingOf(f model.Finding) finding {
	out := finding{Service: f.Service, Name: f.Name, Key: f.Key, Args: f.Args, Detail: f.Detail, FixPort: f.FixPort,
		Tone: map[model.FindingLevel]string{model.FindingUrgent: "bad", model.FindingWarning: "warn", model.FindingHint: "muted"}[f.Level]}
	switch f.Key {
	case "find.crashed", "find.restarting", "find.noanswer":
		out.Link, out.LinkKey = fmt.Sprintf("/services/%d/logs", f.Service), "find.logs"
	case "find.webhook":
		out.Link, out.LinkKey = fmt.Sprintf("/services/%d#webhook", f.Service), "find.webhook.open"
	}
	return out
}

// ── 홈 ────────────────────────────────────

type homeScreen struct {
	AdminDomain string
	Host        model.ServerSnapshot
	Services    []card
	Engine      model.WebServerStatus
	Attention   []finding // 웹서버·Docker(지금 상태) 먼저, 그다음 지켜보기가 찾은 것
	Checked     bool
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
	if !d.Engine.Connected {
		d.Attention = append(d.Attention, finding{Key: "engine.down", Tone: "bad", Detail: d.Engine.LastError})
	}
	if h.DockerDown {
		d.Attention = append(d.Attention, finding{Key: "docker.down", Tone: "bad"})
	}
	for _, f := range h.Findings {
		d.Attention = append(d.Attention, findingOf(f))
	}
	d.Checked = h.Checked
	for _, c := range h.Cards {
		d.Services = append(d.Services, cardOf(c))
	}
	s.render(w, r, http.StatusOK, "home", view{Nav: "home", User: &u, OK: okKeys[r.URL.Query().Get("ok")], Data: d})
}
