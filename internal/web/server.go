// Package web은 관리 화면이다. HTML은 서버가 만들고(html/template — 출력은 자동으로 이스케이프된다),
// 폼은 자바스크립트 없이 POST → 리다이렉트로 동작한다.
package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/auth"
	"github.com/KangminNa/naru/internal/dnscheck"
	"github.com/KangminNa/naru/internal/hostinfo"
	"github.com/KangminNa/naru/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

const (
	sessionCookie = "naru_session"
	langCookie    = "naru_lang"
	defaultLang   = "en"
)

var pageNames = []string{"setup_token", "setup_account", "setup_domain", "setup_https", "login", "home", "settings", "notfound"}

type Deps struct {
	Store   *store.Store
	Auth    *auth.Service
	Host    *hostinfo.Sampler
	Lookup  dnscheck.Resolver
	Log     *slog.Logger
	DataDir string
	Version string
	// 환경 변수로 정해진 값은 화면보다 우선한다 (파일로 설정하고 싶은 사람을 위해).
	EnvAdminDomain string
	EnvACMEEmail   string
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
	User                     *auth.User
	Err, OK                  string // 문구 키
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
		"bytes": humanBytes,
		"width": func(v float64) template.CSS { return template.CSS(fmt.Sprintf("%.0f%%", clamp(v))) },
		"ratio": func(part, whole uint64) template.CSS {
			if whole == 0 {
				return "0%"
			}
			return template.CSS(fmt.Sprintf("%.0f%%", clamp(float64(part)/float64(whole)*100)))
		},
	}
}

func clamp(v float64) float64 { return max(0, min(100, v)) }

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

func (s *Server) currentUser(r *http.Request) (auth.User, string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.User{}, "", false
	}
	u, ok := s.d.Auth.UserFor(c.Value)
	return u, c.Value, ok
}

func (s *Server) setupDone() bool {
	_, ok := s.d.Store.Setting(store.KeySetupDone)
	return ok
}

func (s *Server) adminDomain() string {
	if s.d.EnvAdminDomain != "" {
		return s.d.EnvAdminDomain
	}
	v, _ := s.d.Store.Setting(store.KeyAdminDomain)
	return v
}

func (s *Server) acmeEmail() string {
	if s.d.EnvACMEEmail != "" {
		return s.d.EnvACMEEmail
	}
	v, _ := s.d.Store.Setting(store.KeyACMEEmail)
	return v
}

// gate는 관리 화면에 들어올 자격을 본다. 안 되면 갈 곳으로 보내고 false를 돌려준다.
func (s *Server) gate(w http.ResponseWriter, r *http.Request) (auth.User, string, bool) {
	if s.d.Auth.NeedsSetup() {
		redirect(w, r, "/setup")
		return auth.User{}, "", false
	}
	u, token, ok := s.currentUser(r)
	if !ok {
		redirect(w, r, "/login")
		return auth.User{}, "", false
	}
	if !s.setupDone() {
		redirect(w, r, "/setup/domain")
		return auth.User{}, "", false
	}
	return u, token, true
}

func redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, err := s.d.Auth.NewSession(userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		MaxAge: int(auth.SessionTTL / time.Second), HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	return nil
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
