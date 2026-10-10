package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// webForm은 서비스 화면 "웹서버" 칸에 채울 글자들이다. 비밀번호는 다시 그리지 않는다 — 있는지만.
type webForm struct {
	Headers     string
	Allow       string
	LoginUser   string
	HasPassword bool
	Maintenance bool
	Paths       string
	Advanced    string
	SentHereBy  []string
}

func webFormOf(v model.WebSettingsView) webForm {
	return webForm{
		Headers: headersText(v.Headers), Allow: allowText(v.AllowFrom), LoginUser: v.LoginUser, HasPassword: v.HasPassword,
		Maintenance: v.Maintenance, Paths: pathsText(v.Paths), Advanced: v.Advanced, SentHereBy: v.SentHereBy,
	}
}

func readWebForm(r *http.Request) webForm {
	return webForm{
		Headers: r.FormValue("headers"), Allow: r.FormValue("allow"), LoginUser: strings.TrimSpace(r.FormValue("login_user")),
		Maintenance: r.FormValue("maintenance") == "1", Paths: r.FormValue("paths"),
		Advanced: strings.ReplaceAll(r.FormValue("advanced"), "\r\n", "\n"),
	}
}

// input은 글자들을 값으로 바꾼다. 모양이 틀린 칸은 그 InputError.
func (f webForm) input(password string, removeLogin bool) (model.WebSettingsInput, error) {
	in := model.WebSettingsInput{LoginUser: f.LoginUser, Password: password, RemoveLogin: removeLogin, Maintenance: f.Maintenance, Advanced: strings.TrimSpace(f.Advanced)}
	var err error
	if in.Headers, err = model.ParseHeaderLines(f.Headers); err != nil {
		return in, err
	}
	if in.AllowFrom, err = model.ParseIPLines(f.Allow); err != nil {
		return in, err
	}
	if in.Paths, err = model.ParsePathLines(f.Paths); err != nil {
		return in, err
	}
	if len(in.Advanced) > 32<<10 {
		return in, model.InputError{Field: "advanced", Code: "advanced"}
	}
	return in, nil
}

func headersText(hs []model.HeaderRule) string {
	var b strings.Builder
	for _, h := range hs {
		fmt.Fprintf(&b, "%s: %s\n", h.Name, h.Value)
	}
	return b.String()
}

func allowText[T fmt.Stringer](ps []T) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.String() + "\n")
	}
	return b.String()
}

func pathsText(ps []model.PathRouteInput) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.Prefix.String() + " " + p.Target)
		if p.StripPrefix {
			b.WriteString(" 떼기")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// webExtras는 웹서버 칸을 다시 그릴 때 덧붙이는 것들이다.
type webExtras struct {
	form     *webForm
	warnings []string
	skipped  []model.SkippedLine
	refused  string
	ok       string
}

func (s *Server) renderWeb(w http.ResponseWriter, r *http.Request, u model.Account, sv model.ServiceView, status int, errKey string, x webExtras) {
	v := s.serviceView(r, u, sv, "web", nil)
	d := v.Data.(serviceScreen)
	if x.form != nil {
		x.form.HasPassword, x.form.SentHereBy = d.Web.HasPassword, d.Web.SentHereBy
		d.Web = *x.form
	}
	d.WebWarnings, d.NginxSkipped, d.WebRefused = x.warnings, x.skipped, x.refused
	v.Data, v.Err, v.OK = d, errKey, x.ok
	s.render(w, r, status, "service", v)
}

func (s *Server) saveWebSettings(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	sv, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	f := readWebForm(r)
	in, err := f.input(r.FormValue("login_password"), r.FormValue("remove_login") == "1")
	var warnings []string
	if err == nil {
		warnings, err = s.d.WebSettings.Apply(r.Context(), sv.Service.ID, in)
	}
	var refused model.RefusedError
	switch {
	case errors.As(err, &refused):
		s.d.Log.Warn("web settings refused", "service", sv.Service.Name.String(), "reason", refused.Reason)
		s.renderWeb(w, r, u, sv, http.StatusBadRequest, "err.refused", webExtras{form: &f, refused: refused.Reason})
	case err != nil:
		status := http.StatusBadRequest
		if _, isInput := model.AsInputError(err); !isInput {
			s.d.Log.Error("web settings", "service", sv.Service.Name.String(), "err", err)
			status = http.StatusInternalServerError
		}
		s.renderWeb(w, r, u, sv, status, errKeyOf(err), webExtras{form: &f})
	case len(warnings) > 0:
		s.d.Log.Info("web settings applied", "service", sv.Service.Name.String(), "by", u.Username.String())
		fresh, _ := s.d.Viewer.Detail(r.Context(), sv.Service.ID)
		s.renderWeb(w, r, u, fresh, http.StatusOK, "", webExtras{warnings: warnings, ok: "ok.web"})
	default:
		s.d.Log.Info("web settings applied", "service", sv.Service.Name.String(), "by", u.Username.String())
		redirect(w, r, fmt.Sprintf("/services/%d?ok=web#web", sv.Service.ID))
	}
}

// importNginx는 붙여 넣은 nginx 설정을 칸에 채워 보여준다. 저장하지 않는다 — 사람이 보고 적용한다.
func (s *Server) importNginx(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	sv, ok := s.detailFor(w, r)
	if !ok {
		return
	}
	got := s.d.Nginx.Translate(r.FormValue("nginx"))
	f := webFormOf(model.WebSettingsView{
		Headers: got.Input.Headers, AllowFrom: got.Input.AllowFrom, Maintenance: got.Input.Maintenance, Paths: got.Input.Paths,
	})
	f.LoginUser = sv.Web.LoginUser
	f.Advanced = sv.Web.Advanced
	s.renderWeb(w, r, u, sv, http.StatusOK, "", webExtras{form: &f, skipped: got.Skipped, ok: "ok.nginx"})
}
