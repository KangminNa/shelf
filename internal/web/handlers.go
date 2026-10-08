package web

import (
	"errors"
	"net"
	"net/http"
	"net/mail"
	"strings"

	"github.com/KangminNa/naru/internal/auth"
	"github.com/KangminNa/naru/internal/dnscheck"
	"github.com/KangminNa/naru/internal/store"
)

// ── 첫 설정: 계정 ─────────────────────────────

type setupData struct {
	Step     int
	Token    string
	Username string
	Domain   string
	Fixed    string
	Email    string
	Check    *dnscheck.Result
}

func (s *Server) setupAccountPage(w http.ResponseWriter, r *http.Request) {
	if !s.d.Auth.NeedsSetup() {
		s.leaveSetup(w, r)
		return
	}
	token := r.URL.Query().Get("token")
	if !s.d.Auth.CheckSetupToken(token) {
		v := view{}
		status := http.StatusOK
		if token != "" {
			v.Err, status = "err.token", http.StatusForbidden
		}
		s.render(w, r, status, "setup_token", v)
		return
	}
	s.render(w, r, http.StatusOK, "setup_account", view{Data: setupData{Step: 1, Token: token}})
}

func (s *Server) setupAccount(w http.ResponseWriter, r *http.Request) {
	token, username := r.FormValue("token"), strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	again := func(errKey string) {
		s.render(w, r, http.StatusBadRequest, "setup_account", view{Err: errKey, Data: setupData{Step: 1, Token: token, Username: username}})
	}
	if password != r.FormValue("password2") {
		again("err.mismatch")
		return
	}
	u, err := s.d.Auth.CreateFirstAccount(token, username, password)
	switch {
	case errors.Is(err, auth.ErrSetupClosed):
		redirect(w, r, "/login")
		return
	case errors.Is(err, auth.ErrBadSetupToken):
		s.render(w, r, http.StatusForbidden, "setup_token", view{Err: "err.token"})
		return
	case errors.Is(err, auth.ErrInvalidUsername):
		again("err.username")
		return
	case errors.Is(err, auth.ErrWeakPassword):
		again("err.weakpassword")
		return
	case err != nil:
		s.d.Log.Error("create account", "err", err)
		again("err.internal")
		return
	}
	s.d.Log.Info("admin account created", "user", u.Username, "from", clientIP(r))
	if err := s.startSession(w, r, u.ID); err != nil {
		redirect(w, r, "/login")
		return
	}
	redirect(w, r, "/setup/domain")
}

// leaveSetup은 계정이 이미 있을 때 /setup으로 온 사람을 알맞은 곳으로 보낸다.
func (s *Server) leaveSetup(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.currentUser(r); ok {
		if s.setupDone() {
			redirect(w, r, "/")
		} else {
			redirect(w, r, "/setup/domain")
		}
		return
	}
	redirect(w, r, "/login")
}

// setupUser는 마법사 2·3단계의 문지기 — 계정을 만든 그 사람만, 아직 끝나지 않았을 때만.
func (s *Server) setupUser(w http.ResponseWriter, r *http.Request) bool {
	if s.d.Auth.NeedsSetup() {
		redirect(w, r, "/setup")
		return false
	}
	if _, _, ok := s.currentUser(r); !ok {
		redirect(w, r, "/login")
		return false
	}
	if s.setupDone() {
		redirect(w, r, "/")
		return false
	}
	return true
}

// ── 첫 설정: 관리 화면 주소 ───────────────────

func (s *Server) setupDomainPage(w http.ResponseWriter, r *http.Request) {
	if !s.setupUser(w, r) {
		return
	}
	domain, _ := s.d.Store.Setting(store.KeyAdminDomain)
	s.render(w, r, http.StatusOK, "setup_domain", view{Data: setupData{Step: 2, Domain: domain, Fixed: s.d.EnvAdminDomain}})
}

func (s *Server) setupDomain(w http.ResponseWriter, r *http.Request) {
	if !s.setupUser(w, r) {
		return
	}
	action := r.FormValue("action")
	if action == "skip" || s.d.EnvAdminDomain != "" {
		redirect(w, r, "/setup/https")
		return
	}
	domain := dnscheck.Normalize(r.FormValue("domain"))
	if !dnscheck.Valid(domain) {
		s.render(w, r, http.StatusBadRequest, "setup_domain", view{Err: "err.domain", Data: setupData{Step: 2, Domain: r.FormValue("domain")}})
		return
	}
	if action != "save" {
		check := dnscheck.Check(r.Context(), s.d.Lookup, domain, hostIP(r))
		if !check.Matches {
			s.render(w, r, http.StatusOK, "setup_domain", view{Data: setupData{Step: 2, Domain: domain, Check: &check}})
			return
		}
	}
	if err := s.d.Store.SetSetting(store.KeyAdminDomain, domain); err != nil {
		s.render(w, r, http.StatusInternalServerError, "setup_domain", view{Err: "err.internal", Data: setupData{Step: 2, Domain: domain}})
		return
	}
	s.changed()
	redirect(w, r, "/setup/https")
}

// ── 첫 설정: HTTPS 연락처 ─────────────────────

func (s *Server) setupHTTPSPage(w http.ResponseWriter, r *http.Request) {
	if !s.setupUser(w, r) {
		return
	}
	s.render(w, r, http.StatusOK, "setup_https", view{Data: setupData{Step: 3, Domain: s.adminDomain(), Email: s.acmeEmail()}})
}

func (s *Server) setupHTTPS(w http.ResponseWriter, r *http.Request) {
	if !s.setupUser(w, r) {
		return
	}
	if r.FormValue("action") != "skip" {
		email := strings.TrimSpace(r.FormValue("email"))
		if email != "" && !validEmail(email) {
			s.render(w, r, http.StatusBadRequest, "setup_https", view{Err: "err.email", Data: setupData{Step: 3, Domain: s.adminDomain(), Email: email}})
			return
		}
		if email != "" {
			if err := s.d.Store.SetSetting(store.KeyACMEEmail, email); err != nil {
				s.render(w, r, http.StatusInternalServerError, "setup_https", view{Err: "err.internal", Data: setupData{Step: 3, Domain: s.adminDomain(), Email: email}})
				return
			}
		}
	}
	if err := s.d.Store.SetSetting(store.KeySetupDone, "1"); err != nil {
		s.render(w, r, http.StatusInternalServerError, "setup_https", view{Err: "err.internal", Data: setupData{Step: 3, Domain: s.adminDomain()}})
		return
	}
	s.changed()
	// 설정이 끝나면 IP로 들어오던 길은 닫힌다. 다른 주소로 들어와 있었다면 관리 주소로 안내한다.
	if domain := s.adminDomain(); domain != "" && !sameHost(r.Host, domain) {
		s.render(w, r, http.StatusOK, "setup_done", view{Data: domain})
		return
	}
	redirect(w, r, "/?ok=setup")
}

// sameHost는 요청 Host(포트 포함일 수 있음)가 domain인가.
func sameHost(host, domain string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.EqualFold(host, domain)
}

func validEmail(v string) bool {
	a, err := mail.ParseAddress(v)
	return err == nil && a.Address == v && strings.Contains(v, ".")
}

// ── 로그인 ─────────────────────────────────

type loginData struct{ Username string }

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if s.d.Auth.NeedsSetup() {
		redirect(w, r, "/setup")
		return
	}
	if _, _, ok := s.currentUser(r); ok {
		redirect(w, r, "/")
		return
	}
	s.render(w, r, http.StatusOK, "login", view{Data: loginData{}})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	ip := clientIP(r)
	u, err := s.d.Auth.Login(ip, username, r.FormValue("password"))
	if err != nil {
		key, status := "err.credentials", http.StatusUnauthorized
		if errors.Is(err, auth.ErrLocked) {
			key, status = "err.locked", http.StatusTooManyRequests
		}
		s.d.Log.Warn("failed sign-in", "user", username, "from", ip)
		s.render(w, r, status, "login", view{Err: key, Data: loginData{Username: username}})
		return
	}
	if err := s.startSession(w, r, u.ID); err != nil {
		s.render(w, r, http.StatusInternalServerError, "login", view{Err: "err.internal", Data: loginData{Username: username}})
		return
	}
	redirect(w, r, "/")
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.d.Auth.EndSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode})
	redirect(w, r, "/login")
}

// 리다이렉트 뒤에 보여줄 알림. 쿼리에서 받는 값은 이 목록으로만 바꾼다.
var okKeys = map[string]string{
	"setup": "ok.setup", "password": "ok.password", "domain": "ok.domain",
	"queued": "ok.queued", "saved": "ok.saved", "redeploy": "ok.redeploy", "stopped": "ok.stopped", "started": "ok.started",
	"domain-added": "ok.domainadded", "domain-removed": "ok.domainremoved", "deleted": "ok.deleted",
}

// ── 설정 ───────────────────────────────────

type settingsData struct {
	Form        string // 오류가 난 폼
	Domain      string
	Email       string
	FixedDomain string
	DataDir     string
	Check       *dnscheck.Result
}

func (s *Server) settingsData() settingsData {
	domain, _ := s.d.Store.Setting(store.KeyAdminDomain)
	return settingsData{Domain: domain, Email: s.acmeEmail(), FixedDomain: s.d.EnvAdminDomain, DataDir: s.d.DataDir}
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	s.render(w, r, http.StatusOK, "settings", view{Nav: "settings", User: &u, OK: okKeys[r.URL.Query().Get("ok")], Data: s.settingsData()})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	u, token, ok := s.gate(w, r)
	if !ok {
		return
	}
	fail := func(status int, key string) {
		d := s.settingsData()
		d.Form = "password"
		s.render(w, r, status, "settings", view{Nav: "settings", User: &u, Err: key, Data: d})
	}
	next := r.FormValue("next")
	if next != r.FormValue("next2") {
		fail(http.StatusBadRequest, "err.mismatch")
		return
	}
	switch err := s.d.Auth.ChangePassword(u.ID, r.FormValue("current"), next, token); {
	case errors.Is(err, auth.ErrBadCredentials):
		fail(http.StatusBadRequest, "err.currentpassword")
	case errors.Is(err, auth.ErrWeakPassword):
		fail(http.StatusBadRequest, "err.weakpassword")
	case err != nil:
		s.d.Log.Error("change password", "err", err)
		fail(http.StatusInternalServerError, "err.internal")
	default:
		s.d.Log.Warn("password changed — other sessions revoked", "user", u.Username)
		redirect(w, r, "/settings?ok=password#password")
	}
}

func (s *Server) changeDomain(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	if s.d.EnvAdminDomain != "" {
		redirect(w, r, "/settings#domain")
		return
	}
	d := s.settingsData()
	d.Form = "domain"
	d.Domain = r.FormValue("domain")
	d.Email = strings.TrimSpace(r.FormValue("email"))
	fail := func(status int, key string) {
		s.render(w, r, status, "settings", view{Nav: "settings", User: &u, Err: key, Data: d})
	}

	domain := dnscheck.Normalize(d.Domain)
	if domain != "" && !dnscheck.Valid(domain) {
		fail(http.StatusBadRequest, "err.domain")
		return
	}
	if d.Email != "" && !validEmail(d.Email) {
		fail(http.StatusBadRequest, "err.email")
		return
	}
	if domain != "" && r.FormValue("action") != "save" {
		check := dnscheck.Check(r.Context(), s.d.Lookup, domain, hostIP(r))
		if !check.Matches {
			d.Domain, d.Check = domain, &check
			s.render(w, r, http.StatusOK, "settings", view{Nav: "settings", User: &u, Data: d})
			return
		}
	}

	var err error
	if domain == "" {
		err = s.d.Store.DeleteSetting(store.KeyAdminDomain)
	} else {
		err = s.d.Store.SetSetting(store.KeyAdminDomain, domain)
	}
	if err == nil {
		if d.Email == "" {
			err = s.d.Store.DeleteSetting(store.KeyACMEEmail)
		} else {
			err = s.d.Store.SetSetting(store.KeyACMEEmail, d.Email)
		}
	}
	if err != nil {
		fail(http.StatusInternalServerError, "err.internal")
		return
	}
	s.changed()
	s.d.Log.Info("admin address changed", "domain", domain, "by", u.Username)
	redirect(w, r, "/settings?ok=domain#domain")
}
