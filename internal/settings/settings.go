// Package settings는 서버 설정(관리 주소·인증서 이메일·첫 설정 완료)을 정하고 알려준다.
// 환경 변수로 정한 값이 화면에서 정한 값보다 우선한다 — 환경 변수가 있으면 화면에서 바꿀 수 없다.
// 웹서버 설정에 영향을 주는 값이 바뀌면 SiteMapChanged를 알린다.
package settings

import (
	"context"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

type adminDomainSetting struct {
	env    model.DomainName
	store  contract.SettingStore
	events contract.EventPublisher
}

// NewAdminDomainSetting은 관리 주소다. env는 ADMIN_DOMAIN (비어 있으면 화면에서 정한다).
func NewAdminDomainSetting(env model.DomainName, store contract.SettingStore, events contract.EventPublisher) contract.AdminDomainSetting {
	return adminDomainSetting{env, store, events}
}

func (s adminDomainSetting) Get(ctx context.Context) (model.DomainName, model.SetBy) {
	if !s.env.IsZero() {
		return s.env, model.SetByEnv
	}
	if v, ok := s.store.Get(ctx, model.SettingAdminDomain); ok {
		if d, err := model.ParseDomainName(v); err == nil {
			return d, model.SetByScreen
		}
	}
	return model.DomainName{}, model.SetByNobody
}

func (s adminDomainSetting) Set(ctx context.Context, d model.DomainName) error {
	if !s.env.IsZero() {
		return model.ErrSetByEnv
	}
	var err error
	if d.IsZero() {
		err = s.store.Delete(ctx, model.SettingAdminDomain)
	} else {
		err = s.store.Set(ctx, model.SettingAdminDomain, d.String())
	}
	if err == nil {
		s.events.Publish(model.SiteMapChanged{Reason: "admin domain"})
	}
	return err
}

type certEmailSetting struct {
	env    model.Email
	store  contract.SettingStore
	events contract.EventPublisher
}

// NewCertEmailSetting은 인증서 연락처 이메일이다. env는 ACME_EMAIL.
func NewCertEmailSetting(env model.Email, store contract.SettingStore, events contract.EventPublisher) contract.CertEmailSetting {
	return certEmailSetting{env, store, events}
}

func (s certEmailSetting) Get(ctx context.Context) (model.Email, model.SetBy) {
	if !s.env.IsZero() {
		return s.env, model.SetByEnv
	}
	if v, ok := s.store.Get(ctx, model.SettingACMEEmail); ok {
		if e, err := model.ParseEmail(v); err == nil && !e.IsZero() {
			return e, model.SetByScreen
		}
	}
	return model.Email{}, model.SetByNobody
}

func (s certEmailSetting) Set(ctx context.Context, e model.Email) error {
	if !s.env.IsZero() {
		return model.ErrSetByEnv
	}
	var err error
	if e.IsZero() {
		err = s.store.Delete(ctx, model.SettingACMEEmail)
	} else {
		err = s.store.Set(ctx, model.SettingACMEEmail, e.String())
	}
	if err == nil {
		s.events.Publish(model.SiteMapChanged{Reason: "certificate email"})
	}
	return err
}

type setupProgress struct {
	store  contract.SettingStore
	events contract.EventPublisher
}

// NewSetupProgress는 첫 설정 마법사를 마쳤는지다. 마치기 전에는 IP로 들어와도 관리 화면에 닿는다.
func NewSetupProgress(store contract.SettingStore, events contract.EventPublisher) contract.SetupProgress {
	return setupProgress{store, events}
}

func (s setupProgress) Done(ctx context.Context) bool {
	_, ok := s.store.Get(ctx, model.SettingSetupDone)
	return ok
}

func (s setupProgress) MarkDone(ctx context.Context) error {
	if err := s.store.Set(ctx, model.SettingSetupDone, "1"); err != nil {
		return err
	}
	s.events.Publish(model.SiteMapChanged{Reason: "setup done"})
	return nil
}
