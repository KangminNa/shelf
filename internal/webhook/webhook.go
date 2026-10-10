// Package webhook은 GitHub·GitLab·레지스트리가 보내는 "새 버전이 있어요"를 받아 배포를 맡긴다.
// 로그인이 아니라 서명으로 확인한다. 서명 방식은 차례로 묻는다 (Chain of Responsibility).
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// ── 서명 확인 ─────────────────────────────

// GitHubSignature는 X-Hub-Signature-256 (본문의 HMAC-SHA256)이다.
type GitHubSignature struct{}

func (GitHubSignature) CanCheck(r model.HookRequest) bool {
	return r.Headers["x-hub-signature-256"] != ""
}

func (GitHubSignature) IsGenuine(r model.HookRequest, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(r.Body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(r.Headers["x-hub-signature-256"]), []byte(want))
}

// GitLabToken은 X-Gitlab-Token 헤더다.
type GitLabToken struct{}

func (GitLabToken) CanCheck(r model.HookRequest) bool { return r.Headers["x-gitlab-token"] != "" }

func (GitLabToken) IsGenuine(r model.HookRequest, secret string) bool {
	return subtle.ConstantTimeCompare([]byte(r.Headers["x-gitlab-token"]), []byte(secret)) == 1
}

// QuerySecret은 주소 끝의 ?secret= 이다 — 헤더를 못 붙이는 레지스트리용.
type QuerySecret struct{}

func (QuerySecret) CanCheck(r model.HookRequest) bool { return r.Query["secret"] != "" }

func (QuerySecret) IsGenuine(r model.HookRequest, secret string) bool {
	return subtle.ConstantTimeCompare([]byte(r.Query["secret"]), []byte(secret)) == 1
}

// ── 브랜치 ────────────────────────────────

type branchFilter struct{}

// NewBranchFilter는 등록한 브랜치의 push만 통과시킨다. ref가 없는 알림(레지스트리)과 브랜치가 없는 서비스(이미지)는 통과다.
func NewBranchFilter() contract.BranchFilter { return branchFilter{} }

func (branchFilter) Wanted(r model.HookRequest, s model.Service) (bool, string) {
	if r.Ref == "" || s.Branch == "" || r.Ref == "refs/heads/"+s.Branch {
		return true, ""
	}
	return false, strings.TrimPrefix(r.Ref, "refs/heads/")
}

// ── 받기 ─────────────────────────────────

type hookReceiver struct {
	services contract.ServiceReader
	secrets  contract.SecretStore
	checkers []contract.SignatureChecker
	branches contract.BranchFilter
	deployer contract.Deployer
	logs     contract.HookLogStore
	clock    contract.Clock
}

// NewHookReceiver는 웹훅을 확인하고 배포를 맡긴다. checkers는 앞에서부터 차례로 묻는다.
func NewHookReceiver(services contract.ServiceReader, secrets contract.SecretStore, checkers []contract.SignatureChecker,
	branches contract.BranchFilter, deployer contract.Deployer, logs contract.HookLogStore, clock contract.Clock) contract.HookReceiver {
	return hookReceiver{services, secrets, checkers, branches, deployer, logs, clock}
}

func (h hookReceiver) Receive(ctx context.Context, id model.ServiceID, r model.HookRequest) model.HookResult {
	s, err := h.services.Get(ctx, id)
	if err != nil {
		return model.HookResult{Status: http.StatusNotFound, Message: "unknown service"}
	}
	sec, err := h.secrets.Get(ctx, id)
	if err != nil || sec.WebhookSecret == "" || !h.genuine(r, sec.WebhookSecret) {
		return model.HookResult{Status: http.StatusUnauthorized, Message: "signature does not match"}
	}
	record := func(result string) {
		h.logs.Save(ctx, id, model.HookLog{At: h.clock.Now(), Result: result})
	}
	if r.Ping || r.Headers["x-github-event"] == "ping" {
		record("ping")
		return model.HookResult{Status: http.StatusOK, Message: "pong"}
	}
	if r.Ref == "" {
		var push struct {
			Ref string `json:"ref"`
		}
		json.Unmarshal(r.Body, &push)
		r.Ref = push.Ref
	}
	if ok, branch := h.branches.Wanted(r, s); !ok {
		record("ignored: " + branch)
		return model.HookResult{Status: http.StatusOK, Message: "ignored: not the " + s.Branch + " branch"}
	}
	if !s.AutoDeploy {
		record("ignored: auto deploy is off")
		return model.HookResult{Status: http.StatusAccepted, Message: "received; auto deploy is off"}
	}
	did, err := h.deployer.Deploy(ctx, id, model.ReasonPush)
	switch {
	case errors.Is(err, model.ErrQueued):
		record("queued")
		return model.HookResult{Status: http.StatusAccepted, Message: "a deploy is running; another will follow"}
	case errors.Is(err, model.ErrNothingToDeploy):
		record("ignored: nothing to deploy")
		return model.HookResult{Status: http.StatusOK, Message: "this service has nothing to deploy"}
	case err != nil:
		record("error: " + err.Error())
		return model.HookResult{Status: http.StatusInternalServerError, Message: "could not start a deploy"}
	}
	record("deploying #" + strconv.FormatInt(int64(did), 10))
	return model.HookResult{Status: http.StatusAccepted, Message: "deploying", Deployment: did}
}

// genuine은 자기 방식인 첫 확인자에게 묻는다. 아무도 자기 방식이 아니면 거짓이다.
func (h hookReceiver) genuine(r model.HookRequest, secret string) bool {
	for _, c := range h.checkers {
		if c.CanCheck(r) {
			return c.IsGenuine(r, secret)
		}
	}
	return false
}
