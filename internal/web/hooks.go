package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/deploy"
	"github.com/KangminNa/naru/internal/service"
)

const maxHookBody = 1 << 20

// webhook은 GitHub·GitLab·레지스트리가 보내는 "새 버전이 있어요"를 받는다. 로그인이 아니라 서명으로 확인한다.
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	reply := func(status int, msg string, extra map[string]any) {
		body := map[string]any{"ok": status < 300, "message": msg}
		for k, v := range extra {
			body[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		reply(http.StatusNotFound, "unknown service", nil)
		return
	}
	sv, err := s.d.Services.Get(id)
	if err != nil {
		reply(http.StatusNotFound, "unknown service", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxHookBody+1))
	if err != nil || len(body) > maxHookBody {
		reply(http.StatusRequestEntityTooLarge, "payload too large", nil)
		return
	}
	sec, err := s.d.Services.Secrets(id)
	if err != nil || sec.WebhookSecret == "" || !signed(r, body, sec.WebhookSecret) {
		s.d.Log.Warn("webhook with a bad signature", "service", sv.Name, "from", clientIP(r))
		reply(http.StatusUnauthorized, "signature does not match", nil)
		return
	}
	record := func(result string) { s.d.Services.RecordHook(id, time.Now().Unix(), result) }

	if r.Header.Get("X-GitHub-Event") == "ping" {
		record("ping")
		reply(http.StatusOK, "pong", nil)
		return
	}
	if !sv.Deployable() {
		record("ignored: nothing to deploy")
		reply(http.StatusOK, "this service has nothing to deploy", nil)
		return
	}
	// 저장소는 등록한 브랜치의 push만 받는다. 이미지는 레지스트리가 보내므로 브랜치가 없다.
	if sv.Kind != service.KindImage {
		var push struct {
			Ref string `json:"ref"`
		}
		json.Unmarshal(body, &push)
		if push.Ref != "" && push.Ref != "refs/heads/"+sv.Branch {
			record("ignored: " + strings.TrimPrefix(push.Ref, "refs/heads/"))
			reply(http.StatusOK, "ignored: not the "+sv.Branch+" branch", nil)
			return
		}
	}
	if !sv.AutoDeploy {
		record("ignored: auto deploy is off")
		reply(http.StatusAccepted, "received; auto deploy is off", nil)
		return
	}
	did, err := s.d.Deployer.Deploy(id, "webhook")
	switch {
	case errors.Is(err, deploy.ErrBusy):
		record("queued")
		reply(http.StatusAccepted, "a deploy is running; another will follow", nil)
	case err != nil:
		record("error: " + err.Error())
		reply(http.StatusInternalServerError, "could not start a deploy", nil)
	default:
		record("deploying #" + strconv.FormatInt(did, 10))
		reply(http.StatusAccepted, "deploying", map[string]any{"deployment": did})
	}
}

// signed는 GitHub(HMAC-SHA256), GitLab(토큰 헤더), 그 밖의 레지스트리(?secret=) 중 하나로 맞는지 본다.
func signed(r *http.Request, body []byte, secret string) bool {
	if sig := r.Header.Get("X-Hub-Signature-256"); sig != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(sig), []byte(want))
	}
	if tok := r.Header.Get("X-Gitlab-Token"); tok != "" {
		return subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) == 1
	}
	if q := r.URL.Query().Get("secret"); q != "" {
		return subtle.ConstantTimeCompare([]byte(q), []byte(secret)) == 1
	}
	return false
}
