package web

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// ── 첫 설정: 계정 ─────────────────────────────

type setupScreen struct {
	Step     int
	Token    string
	Username string
	Domain   string
	Fixed    string
	Email    string
	Check    *model.DNSAnswer
}

func (s *Server) setupAccountPage(w http.ResponseWriter, r *http.Request) {
	if !s.needsSetup(r.Context()) {
		s.leaveSetup(w, r)
		return
	}
	token := r.URL.Query().Get("token")
	if !s.d.SetupKey.Matches(r.Context(), token) {
		v := view{}
		status := http.StatusOK
		if token != "" {
			v.Err, status = "err.token", http.StatusForbidden
		}
		s.render(w, r, status, "setup_token", v)
		return
	}
	s.render(w, r, http.StatusOK, "setup_account", view{Data: setupScreen{Step: 1, Token: token}})
}

func (s *Server) setupAccount(w http.ResponseWriter, r *http.Request) {
	token, username := r.FormValue("token"), strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	again := func(errKey string) {
		s.render(w, r, http.StatusBadRequest, "setup_account", view{Err: errKey, Data: setupScreen{Step: 1, Token: token, Username: username}})
	}
	if password != r.FormValue("password2") {
		again("err.mismatch")
		return
	}
	if !s.needsSetup(r.Context()) {
		redirect(w, r, "/login")
		return
	}
	if !s.d.SetupKey.Matches(r.Context(), token) {
		s.render(w, r, http.StatusForbidden, "setup_token", view{Err: "err.token"})
		return
	}
	user, err := model.ParseUsername(username)
	if err != nil {
		again("err.username")
		return
	}
	t, err := s.d.Accounts.CreateFirst(r.Context(), token, user, password)
	switch {
	case errors.Is(err, model.ErrSetupClosed):
		redirect(w, r, "/login")
		return
	case errors.Is(err, model.ErrBadSetupKey):
		s.render(w, r, http.StatusForbidden, "setup_token", view{Err: "err.token"})
		return
	case errors.Is(err, model.ErrWeakPassword):
		again("err.weakpassword")
		return
	case err != nil:
		s.d.Log.Error("create account", "err", err)
		again("err.internal")
		return
	}
	s.d.Log.Info("admin account created", "user", user.String(), "from", clientIP(r))
	s.setSessionCookie(w, r, t)
	redirect(w, r, "/setup/domain")
}

// leaveSetup은 계정이 이미 있을 때 /setup으로 온 사람을 알맞은 곳으로 보낸다.
func (s *Server) leaveSetup(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.currentUser(r); ok {
		if s.setupDone(r.Context()) {
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
	if s.needsSetup(r.Context()) {
		redirect(w, r, "/setup")
		return false
	}
	if _, _, ok := s.currentUser(r); !ok {
		redirect(w, r, "/login")
		return false
	}
	if s.setupDone(r.Context()) {
		redirect(w, r, "/")
		return false
	}
	return true
}

// adminDomainForm은 관리 주소 폼에 미리 채울 값이다. 환경 변수로 정했으면 fixed에 들어간다.
func (s *Server) adminDomainForm(r *http.Request) (domain, fixed string) {
	d, by := s.d.AdminDomain.Get(r.Context())
	if by == model.SetByEnv {
		return "", d.String()
	}
	return d.String(), ""
}

// ── 첫 설정: 관리 화면 주소 ───────────────────

func (s *Server) setupDomainPage(w http.ResponseWriter, r *http.Request) {
	if !s.setupUser(w, r) {
		return
	}
	domain, fixed := s.adminDomainForm(r)
	s.render(w, r, http.StatusOK, "setup_domain", view{Data: setupScreen{Step: 2, Domain: domain, Fixed: fixed}})
}

func (s *Server) setupDomain(w http.ResponseWriter, r *http.Request) {
	if !s.setupUser(w, r) {
		return
	}
	action := r.FormValue("action")
	if _, fixed := s.adminDomainForm(r); action == "skip" || fixed != "" {
		redirect(w, r, "/setup/https")
		return
	}
	domain, err := model.ParseDomainName(r.FormValue("domain"))
	if err != nil {
		s.render(w, r, http.StatusBadRequest, "setup_domain", view{Err: "err.domain", Data: setupScreen{Step: 2, Domain: r.FormValue("domain")}})
		return
	}
	if action != "save" {
		check := s.d.DNS.PointsHere(r.Context(), domain, hostIP(r))
		if !check.Matches {
			s.render(w, r, http.StatusOK, "setup_domain", view{Data: setupScreen{Step: 2, Domain: domain.String(), Check: &check}})
			return
		}
	}
	if err := s.d.AdminDomain.Set(r.Context(), domain); err != nil {
		s.render(w, r, http.StatusInternalServerError, "setup_domain", view{Err: "err.internal", Data: setupScreen{Step: 2, Domain: domain.String()}})
		return
	}
	redirect(w, r, "/setup/https")
}

// ── 첫 설정: HTTPS 연락처 ─────────────────────

func (s *Server) certEmail(r *http.Request) string {
	e, _ := s.d.CertEmail.Get(r.Context())
	return e.String()
}

// saveCertEmail은 이메일을 저장한다. 환경 변수로 정해 둔 값이 있으면 그것이 이기므로 저장하지 않는다.
func (s *Server) saveCertEmail(r *http.Request, e model.Email) error {
	if err := s.d.CertEmail.Set(r.Context(), e); err != nil && !errors.Is(err, model.ErrSetByEnv) {
		return err
	}
	return nil
}

func (s *Server) setupHTTPSPage(w http.ResponseWriter, r *http.Request) {
	if !s.setupUser(w, r) {
		return
	}
	s.render(w, r, http.StatusOK, "setup_https", view{Data: setupScreen{Step: 3, Domain: s.adminDomain(r.Context()), Email: s.certEmail(r)}})
}

func (s *Server) setupHTTPS(w http.ResponseWriter, r *http.Request) {
	if !s.setupUser(w, r) {
		return
	}
	domain := s.adminDomain(r.Context())
	if r.FormValue("action") != "skip" {
		raw := strings.TrimSpace(r.FormValue("email"))
		email, err := model.ParseEmail(raw)
		if err != nil {
			s.render(w, r, http.StatusBadRequest, "setup_https", view{Err: "err.email", Data: setupScreen{Step: 3, Domain: domain, Email: raw}})
			return
		}
		if !email.IsZero() {
			if err := s.saveCertEmail(r, email); err != nil {
				s.render(w, r, http.StatusInternalServerError, "setup_https", view{Err: "err.internal", Data: setupScreen{Step: 3, Domain: domain, Email: raw}})
				return
			}
		}
	}
	if err := s.d.Setup.MarkDone(r.Context()); err != nil {
		s.render(w, r, http.StatusInternalServerError, "setup_https", view{Err: "err.internal", Data: setupScreen{Step: 3, Domain: domain}})
		return
	}
	// 설정이 끝나면 IP로 들어오던 길은 닫힌다. 다른 주소로 들어와 있었다면 관리 주소로 안내한다.
	if domain != "" && !sameHost(r.Host, domain) {
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

// ── 로그인 ─────────────────────────────────

type loginScreen struct{ Username string }

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if s.needsSetup(r.Context()) {
		redirect(w, r, "/setup")
		return
	}
	if _, _, ok := s.currentUser(r); ok {
		redirect(w, r, "/")
		return
	}
	s.render(w, r, http.StatusOK, "login", view{Data: loginScreen{}})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	ip := clientIP(r)
	t, err := s.d.Login.LogIn(r.Context(), ip, username, r.FormValue("password"))
	if err != nil {
		key, status := "err.credentials", http.StatusUnauthorized
		switch {
		case errors.Is(err, model.ErrLocked):
			key, status = "err.locked", http.StatusTooManyRequests
		case !errors.Is(err, model.ErrBadLogin):
			key, status = "err.internal", http.StatusInternalServerError
		}
		s.d.Log.Warn("failed sign-in", "user", username, "from", ip)
		s.render(w, r, status, "login", view{Err: key, Data: loginScreen{Username: username}})
		return
	}
	s.setSessionCookie(w, r, t)
	redirect(w, r, "/")
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.d.Login.LogOut(r.Context(), model.SessionToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode})
	redirect(w, r, "/login")
}

// 리다이렉트 뒤에 보여줄 알림. 쿼리에서 받는 값은 이 목록으로만 바꾼다.
var okKeys = map[string]string{
	"setup": "ok.setup", "password": "ok.password", "domain": "ok.domain",
	"queued": "ok.queued", "saved": "ok.saved", "redeploy": "ok.redeploy", "stopped": "ok.stopped", "started": "ok.started",
	"domain-added": "ok.domainadded", "domain-removed": "ok.domainremoved", "deleted": "ok.deleted", "web": "ok.web",
}

// ── 설정 ───────────────────────────────────

type settingsScreen struct {
	Form        string // 오류가 난 폼
	Domain      string
	Email       string
	FixedDomain string
	DataDir     string
	Check       *model.DNSAnswer
	Cert        *certRow // 관리 주소의 인증서
}

func (s *Server) settingsScreen(r *http.Request) settingsScreen {
	domain, fixed := s.adminDomainForm(r)
	return settingsScreen{Domain: domain, Email: s.certEmail(r), FixedDomain: fixed, DataDir: s.d.DataDir, Cert: s.adminCert(r.Context())}
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	s.render(w, r, http.StatusOK, "settings", view{Nav: "settings", User: &u, OK: okKeys[r.URL.Query().Get("ok")], Data: s.settingsScreen(r)})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	u, token, ok := s.gate(w, r)
	if !ok {
		return
	}
	fail := func(status int, key string) {
		d := s.settingsScreen(r)
		d.Form = "password"
		s.render(w, r, status, "settings", view{Nav: "settings", User: &u, Err: key, Data: d})
	}
	next := r.FormValue("next")
	if next != r.FormValue("next2") {
		fail(http.StatusBadRequest, "err.mismatch")
		return
	}
	switch err := s.d.Accounts.ChangePassword(r.Context(), u, r.FormValue("current"), next, token); {
	case errors.Is(err, model.ErrWrongPassword):
		fail(http.StatusBadRequest, "err.currentpassword")
	case errors.Is(err, model.ErrWeakPassword):
		fail(http.StatusBadRequest, "err.weakpassword")
	case err != nil:
		s.d.Log.Error("change password", "err", err)
		fail(http.StatusInternalServerError, "err.internal")
	default:
		s.d.Log.Warn("password changed — other sessions revoked", "user", u.Username.String())
		redirect(w, r, "/settings?ok=password#password")
	}
}

func (s *Server) changeDomain(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	d := s.settingsScreen(r)
	if d.FixedDomain != "" {
		redirect(w, r, "/settings#domain")
		return
	}
	d.Form = "domain"
	d.Domain = r.FormValue("domain")
	d.Email = strings.TrimSpace(r.FormValue("email"))
	fail := func(status int, key string) {
		s.render(w, r, status, "settings", view{Nav: "settings", User: &u, Err: key, Data: d})
	}

	domain, err := model.ParseOptionalDomainName(d.Domain)
	if err != nil {
		fail(http.StatusBadRequest, "err.domain")
		return
	}
	email, err := model.ParseEmail(d.Email)
	if err != nil {
		fail(http.StatusBadRequest, "err.email")
		return
	}
	if !domain.IsZero() && r.FormValue("action") != "save" {
		check := s.d.DNS.PointsHere(r.Context(), domain, hostIP(r))
		if !check.Matches {
			d.Domain, d.Check = domain.String(), &check
			s.render(w, r, http.StatusOK, "settings", view{Nav: "settings", User: &u, Data: d})
			return
		}
	}
	if err := s.d.AdminDomain.Set(r.Context(), domain); err != nil {
		fail(http.StatusInternalServerError, "err.internal")
		return
	}
	if err := s.saveCertEmail(r, email); err != nil {
		fail(http.StatusInternalServerError, "err.internal")
		return
	}
	s.d.Log.Info("admin address changed", "domain", domain.String(), "by", u.Username.String())
	redirect(w, r, "/settings?ok=domain#domain")
}
