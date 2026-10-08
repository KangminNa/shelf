package web

import (
	"context"
	"fmt"
	"net/http"
	"time"
	"unicode"

	"github.com/KangminNa/naru/internal/docker"
	"github.com/KangminNa/naru/internal/engine"
	"github.com/KangminNa/naru/internal/hostinfo"
	"github.com/KangminNa/naru/internal/service"
)

// card는 서비스 하나를 화면에 보여줄 모양이다.
type card struct {
	ID         int64
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

// cardFor는 서비스와 (있다면) 컨테이너 상태로 카드를 만든다. containers가 nil이면 Docker를 읽지 못한 것이다.
func cardForService(s service.Service, containers map[string]docker.Container) card {
	c := card{ID: s.ID, Name: s.Name, Initial: initialOf(s.Name), Color: colorFor(s.Name), KindKey: "kind." + string(s.Kind), Domain: s.PrimaryDomain()}
	switch {
	case s.Kind == service.KindExternal:
		c.StateKey, c.StateClass = "state.routed", "muted"
	case s.Kind == service.KindStatic && s.Release == "":
		c.StateKey, c.StateClass = "state.notdeployed", "muted"
	case s.Kind == service.KindStatic:
		c.StateKey, c.StateClass = "state.static", "ok"
	case containers == nil:
		c.StateKey, c.StateClass = "state.unknown", "muted"
	default:
		ct, ok := containers[s.CurrentContainer()]
		if !ok {
			c.StateKey, c.StateClass = "state.missing", "bad"
			if s.Instance == "" && s.Container == "" {
				c.StateKey, c.StateClass = "state.notdeployed", "muted"
			}
			break
		}
		c.Detail = ct.Status
		switch ct.State {
		case docker.Running:
			c.StateKey, c.StateClass = "state.running", "ok"
		case docker.Restarting, docker.Dead:
			c.StateKey, c.StateClass = "state.restarting", "bad"
		case docker.Exited:
			c.StateKey, c.StateClass = "state.crashed", "bad"
			if s.Stopped {
				c.StateKey, c.StateClass = "state.stopped", "muted"
			}
		case docker.Paused:
			c.StateKey, c.StateClass = "state.paused", "warn"
		default:
			c.StateKey, c.StateClass = "state.created", "muted"
		}
	}
	return c
}

func (s *Server) containers(ctx context.Context) map[string]docker.Container {
	if s.d.Containers == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	all, err := s.d.Containers.Containers(ctx)
	if err != nil {
		s.d.Log.Warn("docker unreachable", "err", err)
		return nil
	}
	return all
}

func (s *Server) engineStatus() engine.Status {
	if s.d.Engine == nil {
		return engine.Status{}
	}
	return s.d.Engine.Status()
}

// changed는 웹서버 설정에 영향을 주는 것이 바뀌었음을 엔진에 알린다.
func (s *Server) changed() {
	if s.d.Engine != nil {
		s.d.Engine.Kick()
	}
}

// ── 홈 ────────────────────────────────────

type homeData struct {
	AdminDomain string
	Host        hostinfo.Snapshot
	Services    []card
	Engine      engine.Status
	DockerDown  bool
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	d := homeData{AdminDomain: s.adminDomain(), Host: s.d.Host.Snapshot(), Engine: s.engineStatus()}
	services, err := s.d.Services.List()
	if err != nil {
		s.d.Log.Error("list services", "err", err)
	}
	var containers map[string]docker.Container
	if hasContainers(services) {
		containers = s.containers(r.Context())
		d.DockerDown = containers == nil
	}
	for _, sv := range services {
		d.Services = append(d.Services, cardForService(sv, containers))
	}
	s.render(w, r, http.StatusOK, "home", view{Nav: "home", User: &u, OK: okKeys[r.URL.Query().Get("ok")], Data: d})
}

func hasContainers(all []service.Service) bool {
	for _, s := range all {
		if s.HasContainer() {
			return true
		}
	}
	return false
}
