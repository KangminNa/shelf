package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// addAlert는 알림 주소를 등록한다. 주소는 비밀이라 다시 그리지 않는다 (틀렸을 때만 적은 그대로 보여준다).
func (s *Server) addAlert(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	f := alertForm{Name: strings.TrimSpace(r.FormValue("name")), URL: strings.TrimSpace(r.FormValue("url"))}
	addr, err := model.ParseAlertURL(f.URL)
	if err == nil {
		err = s.d.Alerts.Add(r.Context(), model.ChannelInput{Name: f.Name, URL: addr, Secret: r.FormValue("secret")})
	}
	if err != nil {
		d := s.settingsScreen(r)
		d.Form, d.AlertForm = "alerts", f
		status := http.StatusBadRequest
		if _, isInput := model.AsInputError(err); !isInput {
			s.d.Log.Error("add alert channel", "err", err)
			status = http.StatusInternalServerError
		}
		s.render(w, r, status, "settings", view{Nav: "settings", User: &u, Err: errKeyOf(err), Data: d})
		return
	}
	s.d.Log.Info("alert channel added", "host", model.AlertHostOf(addr.String()), "by", u.Username.String())
	redirect(w, r, "/settings?ok=alert-added#alerts")
}

func (s *Server) removeAlert(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.gate(w, r); !ok {
		return
	}
	s.d.Alerts.Remove(r.Context(), model.ChannelID(pathID(r, "cid")))
	redirect(w, r, "/settings?ok=alert-removed#alerts")
}

// testAlert는 시험 알림을 지금 보낸다. 안 되면 그 이유를 그대로 보여준다.
func (s *Server) testAlert(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.gate(w, r)
	if !ok {
		return
	}
	err := s.d.Alerts.Test(r.Context(), model.ChannelID(pathID(r, "cid")))
	switch {
	case errors.Is(err, model.ErrNotFound):
		redirect(w, r, "/settings#alerts")
	case err != nil:
		d := s.settingsScreen(r)
		d.Form, d.AlertError = "alerts", err.Error()
		s.render(w, r, http.StatusBadGateway, "settings", view{Nav: "settings", User: &u, Err: "err.alerttest", Data: d})
	default:
		redirect(w, r, "/settings?ok=alert-tested#alerts")
	}
}
