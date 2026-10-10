// Package websettings는 서비스마다 웹서버 설정을 정한다 (v2-objects §4 I).
// 설정은 그 서비스의 모든 주소에 똑같이 적용되고, 적용하면 바로 웹서버에 맞춰 본다 — 거절되면 되돌린다.
package websettings

import (
	"context"
	"errors"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// EditorParts는 설정 적용이 쓰는 것들이다.
type EditorParts struct {
	Services contract.ServiceReader
	Store    contract.WebSettingsStore
	Compiler contract.SnippetCompiler
	Hasher   contract.LoginHasher
	Sync     contract.WebServerSync
}

type webSettingsEditor struct{ p EditorParts }

func NewWebSettingsEditor(p EditorParts) contract.WebSettingsEditor { return webSettingsEditor{p} }

// Apply는 입력을 확인하고 저장한 뒤 웹서버에 맞춰 본다. 웹서버가 거절하면 옛 설정으로 되돌린다 —
// 틀린 설정이 저장된 채로 남으면 이후 모든 맞추기가 실패해 다른 서비스까지 바뀌지 않게 되기 때문이다.
func (e webSettingsEditor) Apply(ctx context.Context, id model.ServiceID, in model.WebSettingsInput) ([]string, error) {
	self, err := e.p.Services.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	old, err := e.p.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	next := model.WebSettings{Headers: in.Headers, AllowFrom: in.AllowFrom, Maintenance: in.Maintenance, Advanced: in.Advanced}

	if next.Paths, err = e.paths(ctx, self, in.Paths); err != nil {
		return nil, err
	}
	if next.Login, err = e.login(old.Login, in); err != nil {
		return nil, err
	}
	var warnings []string
	if in.Advanced != "" {
		compiled, ignored, err := e.p.Compiler.Compile(ctx, in.Advanced)
		if err != nil {
			return nil, model.RefusedError{Reason: err.Error()}
		}
		next.Compiled, warnings = compiled, ignored
	}

	if err := e.p.Store.Set(ctx, id, next); err != nil {
		return nil, err
	}
	if err := e.p.Sync.SyncNow(ctx); err != nil {
		if rerr := e.p.Store.Set(ctx, id, old); rerr != nil {
			return nil, errors.Join(model.RefusedError{Reason: err.Error()}, rerr)
		}
		e.p.Sync.SyncNow(ctx)
		return nil, model.RefusedError{Reason: err.Error()}
	}
	return warnings, nil
}

// paths는 경로 대상을 정한다 — 서비스 이름이면 그 서비스로, 아니면 "호스트:포트".
func (e webSettingsEditor) paths(ctx context.Context, self model.Service, in []model.PathRouteInput) ([]model.PathRoute, error) {
	if len(in) == 0 {
		return nil, nil
	}
	all, err := e.p.Services.List(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]model.ServiceID{}
	for _, s := range all {
		byName[s.Name.String()] = s.ID
	}
	var out []model.PathRoute
	for _, r := range in {
		route := model.PathRoute{Prefix: r.Prefix, StripPrefix: r.StripPrefix}
		if id, ok := byName[r.Target]; ok {
			if id == self.ID {
				return nil, model.InputError{Field: "paths", Code: "pathself"}
			}
			route.Service = id
		} else if addr, err := model.ParseExternalAddress(r.Target); err == nil {
			route.External = addr
		} else {
			return nil, model.InputError{Field: "paths", Code: "pathtarget"}
		}
		out = append(out, route)
	}
	return out, nil
}

// login은 기본 인증 계정을 정한다. 비밀번호를 비워 두면 지금 것을 그대로 둔다.
func (e webSettingsEditor) login(old model.BasicLogin, in model.WebSettingsInput) (model.BasicLogin, error) {
	if in.RemoveLogin || in.LoginUser == "" {
		return model.BasicLogin{}, nil
	}
	if in.Password == "" {
		if old.Hash == "" {
			return model.BasicLogin{}, model.InputError{Field: "password", Code: "weakpassword"}
		}
		return model.BasicLogin{User: in.LoginUser, Hash: old.Hash}, nil
	}
	if len(in.Password) < model.MinPasswordLength {
		return model.BasicLogin{}, model.InputError{Field: "password", Code: "weakpassword"}
	}
	hash, err := e.p.Hasher.Hash(in.Password)
	if err != nil {
		return model.BasicLogin{}, err
	}
	return model.BasicLogin{User: in.LoginUser, Hash: hash}, nil
}

// BcryptHasher는 기본 인증 비밀번호를 bcrypt로 해시한다 — Caddy가 그대로 검사할 수 있는 형식이다.
type BcryptHasher struct{ Cost int }

func (h BcryptHasher) Hash(password string) (string, error) {
	cost := h.Cost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	return string(b), err
}
