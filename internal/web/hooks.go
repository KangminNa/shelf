package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

const maxHookBody = 1 << 20

// webhook은 GitHub·GitLab·레지스트리가 보내는 "새 버전이 있어요"를 받는다. 확인과 배포는 HookReceiver가 한다.
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	reply := func(res model.HookResult) {
		body := map[string]any{"ok": res.Status < 300, "message": res.Message}
		if res.Deployment != 0 {
			body["deployment"] = res.Deployment
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.Status)
		json.NewEncoder(w).Encode(body)
	}
	id := pathID(r, "id")
	if id <= 0 {
		reply(model.HookResult{Status: http.StatusNotFound, Message: "unknown service"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxHookBody+1))
	if err != nil || len(body) > maxHookBody {
		reply(model.HookResult{Status: http.StatusRequestEntityTooLarge, Message: "payload too large"})
		return
	}
	req := model.HookRequest{Headers: map[string]string{}, Query: map[string]string{}, Body: body}
	for k := range r.Header {
		req.Headers[strings.ToLower(k)] = r.Header.Get(k)
	}
	for k := range r.URL.Query() {
		req.Query[k] = r.URL.Query().Get(k)
	}
	res := s.d.Hooks.Receive(r.Context(), model.ServiceID(id), req)
	if res.Status == http.StatusUnauthorized {
		s.d.Log.Warn("webhook with a bad signature", "service", id, "from", clientIP(r))
	}
	reply(res)
}
