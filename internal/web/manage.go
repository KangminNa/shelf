package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/deploy"
	"github.com/KangminNa/naru/internal/docker"
	"github.com/KangminNa/naru/internal/service"
)

// Deployer는 화면이 쓰는 배포 기능이다.
type Deployer interface {
	Deploy(serviceID int64, trigger string) (int64, error)
	Rollback(serviceID, from int64) (int64, error)
	Stop(ctx context.Context, serviceID int64) error
	Start(ctx context.Context, serviceID int64) error
	Delete(ctx context.Context, serviceID int64) error
	Running(serviceID int64) bool
}

var serviceKinds = []string{"repo", "image", "static", "external"}

// ── 서비스 추가 ─────────────────────────────

type newServiceData struct {
	Kinds []string
	Form  serviceForm
}

func (s *Server) newServicePage(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	f := serviceForm{Kind: r.URL.Query().Get("kind"), Branch: "main"}
	if !service.Kind(f.Kind).Valid() {
		f.Kind = ""
	}
	s.render(w, r, http.StatusOK, "new_service", view{Nav: "home", User: &u, Data: newServiceData{Kinds: serviceKinds, Form: f}})
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	f := readServiceForm(r)
	fail := func(key string) {
		f.Token = "" // 토큰은 다시 그리지 않는다
		s.render(w, r, http.StatusBadRequest, "new_service", view{Nav: "home", User: &u, Err: key, Data: newServiceData{Kinds: serviceKinds, Form: f}})
	}
	if key := f.check(); key != "" {
		fail(key)
		return
	}
	if f.Name == "" {
		f.Name = s.freeName(suggestName(f))
	}
	switch {
	case !namePattern.MatchString(f.Name):
		fail("err.name")
		return
	case s.d.Services.NameTaken(f.Name):
		fail("err.nametaken")
		return
	case f.Domain != "" && strings.EqualFold(f.Domain, s.adminDomain()):
		fail("err.admindomain")
		return
	case f.Domain != "" && s.d.Services.DomainTaken(f.Domain):
		fail("err.domaintaken")
		return
	}

	secret := make([]byte, 24)
	rand.Read(secret)
	sv := service.Service{
		Name: f.Name, Kind: service.Kind(f.Kind), Source: f.Source, Branch: f.Branch, BuildPath: f.BuildPath,
		Folder: f.Folder, Upstream: f.Upstream, Port: f.Port, AutoDeploy: true,
	}
	id, err := s.d.Services.Create(sv, service.Secrets{Env: f.Env, Volumes: f.Volumes, GitToken: f.Token, WebhookSecret: hex.EncodeToString(secret)})
	if err != nil {
		s.d.Log.Error("create service", "err", err)
		fail("err.internal")
		return
	}
	if f.Domain != "" {
		if err := s.d.Services.AddDomain(id, service.Domain{Domain: f.Domain, HTTPS: true}); err != nil {
			s.d.Log.Warn("add domain", "err", err)
		}
	}
	s.d.Log.Info("service created", "service", f.Name, "kind", f.Kind, "by", u.Username)
	s.changed()

	if !sv.Deployable() {
		redirect(w, r, fmt.Sprintf("/services/%d", id))
		return
	}
	did, err := s.d.Deployer.Deploy(id, "create")
	if err != nil {
		redirect(w, r, fmt.Sprintf("/services/%d", id))
		return
	}
	redirect(w, r, fmt.Sprintf("/services/%d/deploys/%d", id, did))
}

// freeName은 겹치면 -2, -3을 붙인다.
func (s *Server) freeName(base string) string {
	name := base
	for i := 2; s.d.Services.NameTaken(name); i++ {
		suffix := "-" + strconv.Itoa(i)
		if len(base)+len(suffix) > 31 {
			base = base[:31-len(suffix)]
		}
		name = base + suffix
	}
	return name
}

// ── 서비스 하나: 동작 ───────────────────────

// serviceFor는 경로의 {id} 서비스를 찾는다. 없으면 404를 그리고 false.
func (s *Server) serviceFor(w http.ResponseWriter, r *http.Request) (service.Service, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		if sv, err := s.d.Services.Get(id); err == nil {
			return sv, true
		} else if !errors.Is(err, service.ErrNotFound) {
			s.d.Log.Error("get service", "id", id, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return service.Service{}, false
		}
	}
	s.render(w, r, http.StatusNotFound, "notfound", view{})
	return service.Service{}, false
}

func (s *Server) deployNow(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.gate(w, r); !ok {
		return
	}
	sv, ok := s.serviceFor(w, r)
	if !ok {
		return
	}
	did, err := s.d.Deployer.Deploy(sv.ID, "manual")
	switch {
	case errors.Is(err, deploy.ErrBusy):
		redirect(w, r, fmt.Sprintf("/services/%d?ok=queued", sv.ID))
	case err != nil:
		redirect(w, r, fmt.Sprintf("/services/%d", sv.ID))
	default:
		redirect(w, r, fmt.Sprintf("/services/%d/deploys/%d", sv.ID, did))
	}
}

func (s *Server) rollback(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.gate(w, r); !ok {
		return
	}
	sv, ok := s.serviceFor(w, r)
	if !ok {
		return
	}
	from, _ := strconv.ParseInt(r.PathValue("did"), 10, 64)
	did, err := s.d.Deployer.Rollback(sv.ID, from)
	switch {
	case errors.Is(err, deploy.ErrBusy):
		redirect(w, r, fmt.Sprintf("/services/%d?ok=queued", sv.ID))
	case err != nil:
		redirect(w, r, fmt.Sprintf("/services/%d?err=rollback#deploys", sv.ID))
	default:
		redirect(w, r, fmt.Sprintf("/services/%d/deploys/%d", sv.ID, did))
	}
}

func (s *Server) stopService(w http.ResponseWriter, r *http.Request)  { s.runtimeAction(w, r, true) }
func (s *Server) startService(w http.ResponseWriter, r *http.Request) { s.runtimeAction(w, r, false) }

func (s *Server) runtimeAction(w http.ResponseWriter, r *http.Request, stop bool) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	sv, ok := s.serviceFor(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var err error
	flash := "started"
	if stop {
		err, flash = s.d.Deployer.Stop(ctx, sv.ID), "stopped"
	} else {
		err = s.d.Deployer.Start(ctx, sv.ID)
	}
	if err != nil {
		s.d.Log.Warn("start/stop", "service", sv.Name, "err", err)
		redirect(w, r, fmt.Sprintf("/services/%d?err=docker", sv.ID))
		return
	}
	s.d.Log.Info("service "+flash, "service", sv.Name, "by", u.Username)
	redirect(w, r, fmt.Sprintf("/services/%d?ok=%s", sv.ID, flash))
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	sv, ok := s.serviceFor(w, r)
	if !ok {
		return
	}
	f := readServiceForm(r)
	f.Kind = string(sv.Kind)
	if key := f.check(); key != "" {
		s.renderService(w, r, u, sv, http.StatusBadRequest, "settings", key, &f)
		return
	}
	st := service.Settings{Source: f.Source, Branch: f.Branch, BuildPath: f.BuildPath, Folder: f.Folder, Port: f.Port,
		Upstream: f.Upstream, AutoDeploy: f.AutoDeploy, Env: f.Env, Volumes: f.Volumes}
	if !sv.Deployable() {
		st.AutoDeploy = sv.AutoDeploy
	}
	if f.Token != "" {
		st.GitToken = &f.Token
	} else if f.ClearToken {
		empty := ""
		st.GitToken = &empty
	}
	// 무엇이 바뀌었는지는 저장하기 전에 본다
	old, _ := s.d.Services.Secrets(sv.ID)
	flash := "saved"
	if sv.Deployable() && (f.Source != sv.Source || f.Branch != sv.Branch || f.BuildPath != sv.BuildPath || f.Folder != sv.Folder || f.Token != "") {
		flash = "redeploy"
	}
	if sv.HasContainer() && (old.Env != f.Env || old.Volumes != f.Volumes) {
		flash = "redeploy" // 컨테이너는 만들 때의 환경으로 돈다
	}
	if err := s.d.Services.Update(sv.ID, st); err != nil {
		s.renderService(w, r, u, sv, http.StatusInternalServerError, "settings", "err.internal", &f)
		return
	}
	s.changed() // 포트·연결 대상이 바뀌었을 수 있다
	redirect(w, r, fmt.Sprintf("/services/%d?ok=%s#settings", sv.ID, flash))
}

func (s *Server) addDomain(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	sv, ok := s.serviceFor(w, r)
	if !ok {
		return
	}
	d := strings.TrimSpace(r.FormValue("domain"))
	domain := normalizeDomain(d)
	key := ""
	switch {
	case !validDomain(domain):
		key = "err.domain"
	case strings.EqualFold(domain, s.adminDomain()):
		key = "err.admindomain"
	case s.d.Services.DomainTaken(domain):
		key = "err.domaintaken"
	}
	if key != "" {
		s.renderService(w, r, u, sv, http.StatusBadRequest, "domain", key, nil)
		return
	}
	if err := s.d.Services.AddDomain(sv.ID, service.Domain{Domain: domain, HTTPS: r.FormValue("https") == "1"}); err != nil {
		s.renderService(w, r, u, sv, http.StatusInternalServerError, "domain", "err.internal", nil)
		return
	}
	s.changed()
	redirect(w, r, fmt.Sprintf("/services/%d?ok=domain-added#domains", sv.ID))
}

func (s *Server) removeDomain(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.gate(w, r); !ok {
		return
	}
	sv, ok := s.serviceFor(w, r)
	if !ok {
		return
	}
	did, _ := strconv.ParseInt(r.PathValue("did"), 10, 64)
	if err := s.d.Services.RemoveDomain(sv.ID, did); err == nil {
		s.changed()
	}
	redirect(w, r, fmt.Sprintf("/services/%d?ok=domain-removed#domains", sv.ID))
}

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	sv, ok := s.serviceFor(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(r.FormValue("confirm")) != sv.Name {
		s.renderService(w, r, u, sv, http.StatusBadRequest, "delete", "err.confirm", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.d.Deployer.Delete(ctx, sv.ID); err != nil {
		key := "err.internal"
		if errors.Is(err, deploy.ErrBusy) {
			key = "err.busy"
		}
		s.renderService(w, r, u, sv, http.StatusConflict, "delete", key, nil)
		return
	}
	s.d.Log.Warn("service deleted", "service", sv.Name, "by", u.Username)
	redirect(w, r, "/?ok=deleted")
}

// ── 배포 하나 ─────────────────────────────

type deployData struct {
	Service    service.Service
	Deployment deploy.Deployment
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
	sv, ok := s.serviceFor(w, r)
	if !ok {
		return
	}
	did, _ := strconv.ParseInt(r.PathValue("did"), 10, 64)
	dep, err := s.d.Deployments.Get(did)
	if err != nil || dep.ServiceID != sv.ID {
		s.render(w, r, http.StatusNotFound, "notfound", view{})
		return
	}
	v := view{Nav: "home", User: &u, Data: deployData{Service: sv, Deployment: dep, Steps: stepsOf(dep, langOf(r))}}
	if dep.Status == deploy.Running {
		v.Refresh = 2 // 진행 중에는 2초마다 새로 그린다
	}
	s.render(w, r, http.StatusOK, "deploy", v)
}

// stepsOf는 로그의 "▶ 한국어 / English" 줄에서 단계를 읽는다.
func stepsOf(d deploy.Deployment, lang string) []stepView {
	var steps []stepView
	for _, line := range strings.Split(d.Log, "\n") {
		label, ok := strings.CutPrefix(line, "▶ ")
		if !ok {
			continue
		}
		ko, en, _ := strings.Cut(label, " / ")
		if lang == "en" && en != "" {
			label = en
		} else {
			label = ko
		}
		steps = append(steps, stepView{Label: label, State: "done"})
	}
	if n := len(steps); n > 0 {
		switch d.Status {
		case deploy.Running:
			steps[n-1].State = "doing"
		case deploy.Failed:
			steps[n-1].State = "failed"
		}
	}
	return steps
}

// ── 서비스 화면 그리기 ──────────────────────

type serviceData struct {
	Service       service.Service
	Card          card
	Target        string
	Deploys       []deploy.Deployment
	Latest        *deploy.Deployment
	LiveID        int64
	Running       bool
	WebhookURL    string
	WebhookSecret string
	HookTime      time.Time
	HasToken      bool
	Settings      serviceForm
	Form          string // 오류가 난 폼
}

func (s *Server) servicePage(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	sv, ok := s.serviceFor(w, r)
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

func (s *Server) renderService(w http.ResponseWriter, r *http.Request, u authUser, sv service.Service, status int, form, errKey string, f *serviceForm) {
	v := s.serviceView(r, u, sv, form, f)
	v.Err = errKey
	s.render(w, r, status, "service", v)
}

func (s *Server) serviceView(r *http.Request, u authUser, sv service.Service, form string, f *serviceForm) view {
	var containers map[string]docker.Container
	if sv.HasContainer() {
		containers = s.containers(r.Context())
	}
	sec, _ := s.d.Services.Secrets(sv.ID)
	d := serviceData{
		Service: sv, Card: cardForService(sv, containers), Target: sv.Target(), Form: form,
		Running: s.d.Deployer != nil && s.d.Deployer.Running(sv.ID), HasToken: sec.GitToken != "",
		WebhookSecret: sec.WebhookSecret, HookTime: time.Unix(sv.HookAt, 0),
	}
	if sv.Deployable() {
		d.Deploys, _ = s.d.Deployments.Recent(sv.ID, 10)
		if len(d.Deploys) > 0 {
			d.Latest = &d.Deploys[0]
		}
		d.LiveID = liveDeployment(sv)
		if sv.Kind == service.KindStatic && sv.Release != "" {
			d.Target = "data/sites/" + sv.Name + "/" + sv.Release
		}
		if domain := s.adminDomain(); domain != "" {
			d.WebhookURL = fmt.Sprintf("https://%s/hooks/%d", domain, sv.ID)
		}
	}
	if f != nil {
		d.Settings = *f
	} else {
		d.Settings = serviceForm{Source: sv.Source, Branch: sv.Branch, BuildPath: sv.BuildPath, Folder: sv.Folder, Upstream: sv.Upstream,
			Port: sv.Port, AutoDeploy: sv.AutoDeploy, Env: sec.Env, Volumes: sec.Volumes}
	}
	return view{Nav: "home", User: &u, Data: d}
}

// liveDeployment는 지금 서빙 중인 배포 번호다 — 컨테이너 이름 끝의 번호, 정적 사이트는 release.
func liveDeployment(sv service.Service) int64 {
	if sv.Kind == service.KindStatic {
		n, _ := strconv.ParseInt(sv.Release, 10, 64)
		return n
	}
	if i := strings.LastIndex(sv.Instance, "-"); i >= 0 {
		n, _ := strconv.ParseInt(sv.Instance[i+1:], 10, 64)
		return n
	}
	return 0
}

// 리다이렉트 뒤 ?err= 로 받는 값도 이 목록으로만 바꾼다.
var errKeys = map[string]string{"rollback": "err.rollback", "docker": "err.docker"}
