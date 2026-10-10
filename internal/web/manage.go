package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// ── 서비스 추가 ─────────────────────────────

type newServiceScreen struct {
	Kinds []string
	Form  serviceForm
}

func kindNames() []string {
	var out []string
	for _, k := range model.AllKinds() {
		out = append(out, string(k))
	}
	return out
}

func (s *Server) newServicePage(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	f := serviceForm{Branch: "main"}
	if k, err := model.ParseKindName(r.URL.Query().Get("kind")); err == nil {
		f.Kind = string(k)
	}
	s.render(w, r, http.StatusOK, "new_service", view{Nav: "home", User: &u, Data: newServiceScreen{Kinds: kindNames(), Form: f}})
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	f := readServiceForm(r)
	fail := func(key string) {
		f.Token = "" // 토큰은 다시 그리지 않는다
		s.render(w, r, http.StatusBadRequest, "new_service", view{Nav: "home", User: &u, Err: key, Data: newServiceScreen{Kinds: kindNames(), Form: f}})
	}
	kind, err := model.ParseKindName(f.Kind)
	if err != nil {
		fail(errKeyOf(err))
		return
	}
	in, err := f.input(kind)
	if err != nil {
		fail(errKeyOf(err))
		return
	}
	id, did, err := s.d.Launcher.Launch(r.Context(), in)
	if id == 0 {
		if _, isInput := model.AsInputError(err); !isInput {
			s.d.Log.Error("create service", "err", err)
		}
		fail(errKeyOf(err))
		return
	}
	s.d.Log.Info("service created", "service", id, "kind", f.Kind, "by", u.Username.String())
	if err != nil || did == 0 {
		redirect(w, r, fmt.Sprintf("/services/%d", id))
		return
	}
	redirect(w, r, fmt.Sprintf("/services/%d/deploys/%d", id, did))
}

// ── 서비스 하나: 동작 ───────────────────────

// detailFor는 경로의 {id} 서비스 화면 값을 읽는다. 없으면 404를 그리고 false.
func (s *Server) detailFor(w http.ResponseWriter, r *http.Request) (model.ServiceView, bool) {
	id := pathID(r, "id")
	if id > 0 {
		v, err := s.d.Viewer.Detail(r.Context(), model.ServiceID(id))
		if err == nil {
			return v, true
		}
		if !errors.Is(err, model.ErrNotFound) {
			s.d.Log.Error("get service", "id", id, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return v, false
		}
	}
	s.render(w, r, http.StatusNotFound, "notfound", view{})
	return model.ServiceView{}, false
}

func (s *Server) deployNow(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.gate(w, r); !ok {
		return
	}
	v, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	id := v.Service.ID
	did, err := s.d.Deployer.Deploy(r.Context(), id, model.ReasonManual)
	switch {
	case errors.Is(err, model.ErrQueued):
		redirect(w, r, fmt.Sprintf("/services/%d?ok=queued", id))
	case err != nil:
		redirect(w, r, fmt.Sprintf("/services/%d", id))
	default:
		redirect(w, r, fmt.Sprintf("/services/%d/deploys/%d", id, did))
	}
}

func (s *Server) rollback(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.gate(w, r); !ok {
		return
	}
	v, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	id := v.Service.ID
	did, err := s.d.Deployer.RollBack(r.Context(), id, model.DeploymentID(pathID(r, "did")))
	switch {
	case errors.Is(err, model.ErrBusy):
		redirect(w, r, fmt.Sprintf("/services/%d?ok=queued", id))
	case err != nil:
		redirect(w, r, fmt.Sprintf("/services/%d?err=rollback#deploys", id))
	default:
		redirect(w, r, fmt.Sprintf("/services/%d/deploys/%d", id, did))
	}
}

func (s *Server) stopService(w http.ResponseWriter, r *http.Request)  { s.runtimeAction(w, r, true) }
func (s *Server) startService(w http.ResponseWriter, r *http.Request) { s.runtimeAction(w, r, false) }

func (s *Server) runtimeAction(w http.ResponseWriter, r *http.Request, stop bool) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	v, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	sv := v.Service
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var err error
	flash := "started"
	if stop {
		err, flash = s.d.Control.Stop(ctx, sv.ID), "stopped"
	} else {
		err = s.d.Control.Start(ctx, sv.ID)
	}
	if err != nil {
		s.d.Log.Warn("start/stop", "service", sv.Name.String(), "err", err)
		redirect(w, r, fmt.Sprintf("/services/%d?err=docker", sv.ID))
		return
	}
	s.d.Log.Info("service "+flash, "service", sv.Name.String(), "by", u.Username.String())
	redirect(w, r, fmt.Sprintf("/services/%d?ok=%s", sv.ID, flash))
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	v, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	f := readServiceForm(r)
	in, err := f.input(v.Service.Kind)
	if err == nil {
		in.Domain = model.DomainName{} // 설정 폼에는 주소가 없다 — 주소는 따로 붙이고 뗀다
		var redeploy bool
		if redeploy, err = s.d.Editor.Update(r.Context(), v.Service.ID, in); err == nil {
			flash := "saved"
			if redeploy {
				flash = "redeploy"
			}
			redirect(w, r, fmt.Sprintf("/services/%d?ok=%s#settings", v.Service.ID, flash))
			return
		}
	}
	status := http.StatusBadRequest
	if _, isInput := model.AsInputError(err); !isInput {
		s.d.Log.Error("save settings", "service", v.Service.Name.String(), "err", err)
		status = http.StatusInternalServerError
	}
	s.renderService(w, r, u, v, status, "settings", errKeyOf(err), &f)
}

func (s *Server) addDomain(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	v, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	domain, err := model.ParseDomainName(r.FormValue("domain"))
	if err == nil {
		err = s.d.Editor.AddDomain(r.Context(), v.Service.ID, model.DomainInput{Domain: domain, HTTPS: r.FormValue("https") == "1"})
	}
	if err != nil {
		status := http.StatusBadRequest
		if _, isInput := model.AsInputError(err); !isInput {
			status = http.StatusInternalServerError
		}
		s.renderService(w, r, u, v, status, "domain", errKeyOf(err), nil)
		return
	}
	redirect(w, r, fmt.Sprintf("/services/%d?ok=domain-added#domains", v.Service.ID))
}

func (s *Server) removeDomain(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.gate(w, r); !ok {
		return
	}
	v, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	s.d.Editor.RemoveDomain(r.Context(), v.Service.ID, model.DomainID(pathID(r, "did")))
	redirect(w, r, fmt.Sprintf("/services/%d?ok=domain-removed#domains", v.Service.ID))
}

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	v, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	sv := v.Service
	if strings.TrimSpace(r.FormValue("confirm")) != sv.Name.String() {
		s.renderService(w, r, u, v, http.StatusBadRequest, "delete", "err.confirm", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.d.Control.Remove(ctx, sv.ID); err != nil {
		key := "err.internal"
		if errors.Is(err, model.ErrBusy) {
			key = "err.busy"
		}
		s.renderService(w, r, u, v, http.StatusConflict, "delete", key, nil)
		return
	}
	s.d.Log.Warn("service deleted", "service", sv.Name.String(), "by", u.Username.String())
	redirect(w, r, "/?ok=deleted")
}

// ── 배포 하나 ─────────────────────────────

type deployScreen struct {
	Service    model.Service
	Deployment model.Deployment
	Steps      []stepView
}

type stepView struct {
	Label string
	State string // done | doing | failed
}

func (s *Server) deployPage(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	dv, err := s.d.Viewer.Deployment(r.Context(), model.ServiceID(pathID(r, "id")), model.DeploymentID(pathID(r, "did")))
	if err != nil {
		s.render(w, r, http.StatusNotFound, "notfound", view{})
		return
	}
	lang := langOf(r)
	d := deployScreen{Service: dv.Service, Deployment: dv.Deployment}
	for _, st := range dv.Steps {
		label := st.Ko
		if lang == "en" && st.En != "" {
			label = st.En
		}
		d.Steps = append(d.Steps, stepView{Label: label, State: st.State})
	}
	v := view{Nav: "home", User: &u, Data: d}
	if dv.Deployment.Status == model.DeployRunning {
		v.Refresh = 2 // 진행 중에는 2초마다 새로 그린다
	}
	s.render(w, r, http.StatusOK, "deploy", v)
}

// ── 로그 ──────────────────────────────────

func (s *Server) logsPage(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	lv, err := s.d.Viewer.Logs(r.Context(), model.ServiceID(pathID(r, "id")), model.ParseLogFilter(r.URL.Query().Get("show")))
	if err != nil {
		s.render(w, r, http.StatusNotFound, "notfound", view{})
		return
	}
	s.render(w, r, http.StatusOK, "logs", view{Nav: "home", User: &u, Data: lv})
}

// ── 서비스 화면 그리기 ──────────────────────

type serviceScreen struct {
	Service       model.Service
	Card          card
	Target        string
	Container     bool // 멈추기·켜기가 된다
	Deployable    bool
	Deploys       []model.Deployment
	Latest        *model.Deployment
	LiveID        model.DeploymentID
	Running       bool
	WebhookURL    string
	WebhookSecret string
	HasToken      bool
	Domains       []domainRow
	Settings      serviceForm
	Web           webForm
	WebWarnings   []string            // 적용됐지만 알릴 것 (고급 칸에서 버린 지시어)
	NginxSkipped  []model.SkippedLine // nginx에서 옮기지 못한 줄
	WebRefused    string              // 웹서버가 거절한 이유 (그 말 그대로)
	Form          string              // 오류가 난 폼
	Findings      []finding           // 지켜보기가 찾은 이 서비스의 문제
}

func (s *Server) servicePage(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	sv, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	v := s.serviceView(r, u, sv, "", nil)
	v.OK = okKeys[r.URL.Query().Get("ok")]
	if e := r.URL.Query().Get("err"); e != "" {
		v.Err = errKeys[e]
	}
	s.render(w, r, http.StatusOK, "service", v)
}

func (s *Server) renderService(w http.ResponseWriter, r *http.Request, u model.Account, sv model.ServiceView, status int, form, errKey string, f *serviceForm) {
	v := s.serviceView(r, u, sv, form, f)
	v.Err = errKey
	s.render(w, r, status, "service", v)
}

func (s *Server) serviceView(r *http.Request, u model.Account, sv model.ServiceView, form string, f *serviceForm) view {
	sc := model.ServiceCard{ID: sv.Service.ID, Name: sv.Service.Name.String(), Kind: sv.Service.Kind, Domain: sv.Service.PrimaryDomain(), Status: sv.Status}
	d := serviceScreen{
		Service: sv.Service, Card: cardOf(sc), Target: sv.Target, Container: sv.Container, Deployable: sv.Deployable,
		Deploys: sv.Deploys, LiveID: sv.LiveID, Running: sv.Deploying, Form: form,
		WebhookURL: sv.Webhook.URL, WebhookSecret: sv.Webhook.Secret, HasToken: sv.Form.HasToken,
		Domains: s.domainRows(r, sv.Domains), Web: webFormOf(sv.Web),
	}
	for _, f := range sv.Findings {
		row := findingOf(f)
		row.Name = "" // 이 서비스 화면 안이다
		d.Findings = append(d.Findings, row)
	}
	if len(d.Deploys) > 0 {
		d.Latest = &d.Deploys[0]
	}
	if f != nil {
		d.Settings = *f
	} else {
		d.Settings = formFrom(sv.Form)
	}
	return view{Nav: "home", User: &u, Data: d}
}

// 리다이렉트 뒤 ?err= 로 받는 값도 이 목록으로만 바꾼다.
var errKeys = map[string]string{"rollback": "err.rollback", "docker": "err.docker"}

// setPort는 진단의 [포트를 N으로 바꾸기]다 — 다시 배포하지 않고 웹서버만 새 포트로 보낸다.
func (s *Server) setPort(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.gate(w, r); !ok {
		return
	}
	v, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	id := v.Service.ID
	p, err := model.ParsePort(r.FormValue("port"))
	if err == nil {
		err = s.d.Editor.SetPort(r.Context(), id, p)
	}
	if err != nil {
		s.d.Log.Warn("set port", "service", v.Service.Name.String(), "err", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	redirect(w, r, fmt.Sprintf("/services/%d?ok=port", id))
}
