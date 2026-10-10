// Package web은 관리 화면이다. HTML은 서버가 만들고(html/template — 출력은 자동으로 이스케이프된다),
// 폼은 자바스크립트 없이 POST → 리다이렉트로 동작한다.
package web

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

const (
	sessionCookie = "naru_session"
	langCookie    = "naru_lang"
	defaultLang   = "en"
	sessionMaxAge = 7 * 24 * time.Hour
)

var pageNames = []string{"setup_token", "setup_account", "setup_domain", "setup_https", "setup_done", "login", "home", "service", "new_service", "deploy", "settings", "notfound"}

// Deps는 화면이 쓰는 것들이다. 모두 인터페이스다 — 화면은 저장소도, Docker도, 웹서버도 직접 모른다.
type Deps struct {
	Login       contract.LoginManager
	Accounts    contract.AccountManager
	SetupKey    contract.SetupKey
	AdminDomain contract.AdminDomainSetting
	CertEmail   contract.CertEmailSetting
	Setup       contract.SetupProgress
	Launcher    contract.ServiceLauncher
	Editor      contract.ServiceEditor
	Viewer      contract.ServiceViewer
	Deployer    contract.Deployer
	Control     contract.ServiceControl
	Hooks       contract.HookReceiver
	Stats       contract.ServerStats
	WebServer   contract.WebServerSync
	DNS         contract.DNSChecker
	WebSettings contract.WebSettingsEditor
	Nginx       contract.NginxTranslator
	Certs       contract.CertificateReader
	Clock       contract.Clock
	Log         *slog.Logger
	DataDir     string
	Version     string
}

type Server struct {
	d     Deps
	pages map[string]map[string]*template.Template // 언어 → 페이지
	mux   *http.ServeMux
}

func New(d Deps) (*Server, error) {
	s := &Server{d: d, pages: map[string]map[string]*template.Template{}, mux: http.NewServeMux()}
	for lang := range messages {
		s.pages[lang] = map[string]*template.Template{}
		for _, page := range pageNames {
			t, err := template.New(page).Funcs(funcsFor(lang)).ParseFS(templateFS,
				"templates/base.html", "templates/dnscheck.html", "templates/"+page+".html")
			if err != nil {
				return nil, fmt.Errorf("template %s: %w", page, err)
			}
			s.pages[lang][page] = t
		}
	}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler { return securityHeaders(sameOrigin(s.mux)) }

func (s *Server) routes() {
	static, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})
	s.mux.HandleFunc("GET /lang/{lang}", s.setLang)

	s.mux.HandleFunc("GET /setup", s.setupAccountPage)
	s.mux.HandleFunc("POST /setup", s.setupAccount)
	s.mux.HandleFunc("GET /setup/domain", s.setupDomainPage)
	s.mux.HandleFunc("POST /setup/domain", s.setupDomain)
	s.mux.HandleFunc("GET /setup/https", s.setupHTTPSPage)
	s.mux.HandleFunc("POST /setup/https", s.setupHTTPS)

	s.mux.HandleFunc("GET /login", s.loginPage)
	s.mux.HandleFunc("POST /login", s.login)
	s.mux.HandleFunc("POST /logout", s.logout)

	s.mux.HandleFunc("GET /{$}", s.home)
	s.mux.HandleFunc("GET /services/new", s.newServicePage)
	s.mux.HandleFunc("POST /services/new", s.createService)
	s.mux.HandleFunc("GET /services/{id}", s.servicePage)
	s.mux.HandleFunc("POST /services/{id}/deploy", s.deployNow)
	s.mux.HandleFunc("POST /services/{id}/rollback/{did}", s.rollback)
	s.mux.HandleFunc("POST /services/{id}/stop", s.stopService)
	s.mux.HandleFunc("POST /services/{id}/start", s.startService)
	s.mux.HandleFunc("POST /services/{id}/settings", s.saveSettings)
	s.mux.HandleFunc("POST /services/{id}/domains", s.addDomain)
	s.mux.HandleFunc("POST /services/{id}/domains/{did}/delete", s.removeDomain)
	s.mux.HandleFunc("POST /services/{id}/delete", s.deleteService)
	s.mux.HandleFunc("POST /services/{id}/web", s.saveWebSettings)
	s.mux.HandleFunc("POST /services/{id}/web/nginx", s.importNginx)
	s.mux.HandleFunc("GET /services/{id}/deploys/{did}", s.deployPage)
	s.mux.HandleFunc("POST /hooks/{id}", s.webhook)
	s.mux.HandleFunc("GET /settings", s.settingsPage)
	s.mux.HandleFunc("POST /settings/password", s.changePassword)
	s.mux.HandleFunc("POST /settings/domain", s.changeDomain)

	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.render(w, r, http.StatusNotFound, "notfound", view{})
	})
}

// ── 화면 그리기 ─────────────────────────────

type view struct {
	Lang, Path, Nav, Version string
	User                     *model.Account
	Err, OK                  string // 문구 키
	Refresh                  int    // 0이 아니면 그 초마다 다시 그린다 (진행 중인 배포)
	Data                     any
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page string, v view) {
	v.Lang = langOf(r)
	v.Path = r.URL.Path
	v.Version = s.d.Version
	var buf bytes.Buffer
	if err := s.pages[v.Lang][page].ExecuteTemplate(&buf, "root", v); err != nil {
		s.d.Log.Error("render", "page", page, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func funcsFor(lang string) template.FuncMap {
	catalog := messages[lang]
	return template.FuncMap{
		"t": func(key string, args ...any) string {
			msg, ok := catalog[key]
			if !ok {
				return key
			}
			if len(args) > 0 {
				return fmt.Sprintf(msg, args...)
			}
			return msg
		},
		"pct": func(v float64) string { return fmt.Sprintf("%.0f%%", v) },
		"ago": func(t time.Time) string { return ago(lang, t) },
		"dur": func(d time.Duration) string { return d.String() },
		"trigger": func(t string) string {
			if strings.HasPrefix(t, "v1") {
				return "v1"
			}
			switch t {
			case "manual", "webhook", "create", "rollback":
				return t
			}
			return "manual"
		},
		"bytes": humanBytes,
		"date": func(t time.Time) string {
			if lang == "ko" {
				return fmt.Sprintf("%d월 %d일", t.Month(), t.Day())
			}
			return t.Format("Jan 2")
		},
	}
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}

// ── 요청에서 읽는 것들 ───────────────────────

func langOf(r *http.Request) string {
	if c, err := r.Cookie(langCookie); err == nil {
		if _, ok := messages[c.Value]; ok {
			return c.Value
		}
	}
	// 소개 페이지와 같은 규칙 — 브라우저가 한국어면 한국어, 아니면 영어.
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "ko") {
		return "ko"
	}
	return defaultLang
}

// fromTrustedProxy는 요청이 이 서버 안(루프백·사설망 — 같은 Docker 네트워크의 웹서버)에서 왔는가.
// 그럴 때만 X-Forwarded-* 헤더를 믿는다. 관리 화면 포트는 밖으로 열지 않는다.
func fromTrustedProxy(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func clientIP(r *http.Request) string {
	if fromTrustedProxy(r) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if ip := strings.TrimSpace(first); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || (fromTrustedProxy(r) && r.Header.Get("X-Forwarded-Proto") == "https")
}

// hostIP는 사용자가 주소창에 IP를 쳐서 들어왔다면 그 IP다 — 이 서버의 IP로 쓴다.
func hostIP(r *http.Request) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() {
		return ip.String()
	}
	return ""
}

func (s *Server) currentUser(r *http.Request) (model.Account, model.SessionToken, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return model.Account{}, "", false
	}
	t := model.SessionToken(c.Value)
	u, ok := s.d.Login.WhoIs(r.Context(), t)
	return u, t, ok
}

// needsSetup은 아직 계정이 하나도 없는가. 읽지 못하면 열어주지 않는다.
func (s *Server) needsSetup(ctx context.Context) bool {
	names, err := s.d.Accounts.Names(ctx)
	return err == nil && len(names) == 0
}

func (s *Server) setupDone(ctx context.Context) bool { return s.d.Setup.Done(ctx) }

func (s *Server) adminDomain(ctx context.Context) string {
	d, _ := s.d.AdminDomain.Get(ctx)
	return d.String()
}

// gate는 관리 화면에 들어올 자격을 본다. 안 되면 갈 곳으로 보내고 false를 돌려준다.
func (s *Server) gate(w http.ResponseWriter, r *http.Request) (model.Account, model.SessionToken, bool) {
	if s.needsSetup(r.Context()) {
		redirect(w, r, "/setup")
		return model.Account{}, "", false
	}
	u, token, ok := s.currentUser(r)
	if !ok {
		redirect(w, r, "/login")
		return model.Account{}, "", false
	}
	if !s.setupDone(r.Context()) {
		redirect(w, r, "/setup/domain")
		return model.Account{}, "", false
	}
	return u, token, true
}

func redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, t model.SessionToken) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: string(t), Path: "/",
		MaxAge: int(sessionMaxAge / time.Second), HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

// pathID는 경로의 {name} 숫자다.
func pathID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
}

func (s *Server) setLang(w http.ResponseWriter, r *http.Request) {
	lang := r.PathValue("lang")
	if _, ok := messages[lang]; ok {
		http.SetCookie(w, &http.Cookie{Name: langCookie, Value: lang, Path: "/", MaxAge: 365 * 24 * 3600, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r)})
	}
	redirect(w, r, safeBack(r.URL.Query().Get("back")))
}

// safeBack은 이 사이트 안의 경로만 돌려준다 — 다른 사이트로 튀는 리다이렉트를 막는다.
func safeBack(back string) string {
	if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") || strings.HasPrefix(back, "/\\") {
		return "/"
	}
	return back
}

// ── 공통 보호 ──────────────────────────────

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin은 다른 사이트에서 보낸 폼 제출을 막는다 (쿠키의 SameSite=Lax와 이중으로).
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" {
				if origin != "http://"+r.Host && origin != "https://"+r.Host {
					http.Error(w, "cross-site request refused", http.StatusForbidden)
					return
				}
			} else if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ago는 "3분 전" 같은 상대 시간이다.
func ago(lang string, t time.Time) string {
	d := time.Since(t)
	n := 0
	unit := ""
	switch {
	case d < time.Minute:
		return map[string]string{"ko": "방금", "en": "just now"}[lang]
	case d < time.Hour:
		n, unit = int(d.Minutes()), "m"
	case d < 24*time.Hour:
		n, unit = int(d.Hours()), "h"
	default:
		n, unit = int(d.Hours()/24), "d"
	}
	if lang == "ko" {
		return fmt.Sprintf("%d%s 전", n, map[string]string{"m": "분", "h": "시간", "d": "일"}[unit])
	}
	return fmt.Sprintf("%d%s ago", n, unit)
}
