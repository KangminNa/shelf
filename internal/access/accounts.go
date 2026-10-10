package access

import (
	"context"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

type accountManager struct {
	reader   contract.AccountReader
	store    contract.AccountStore
	sessions contract.SessionStore
	hasher   contract.PasswordHasher
	key      contract.SetupKey
	random   contract.RandomTokens
	clock    contract.Clock
}

func NewAccountManager(reader contract.AccountReader, store contract.AccountStore, sessions contract.SessionStore,
	hasher contract.PasswordHasher, key contract.SetupKey, random contract.RandomTokens, clock contract.Clock) contract.AccountManager {
	return accountManager{reader, store, sessions, hasher, key, random, clock}
}

// CreateFirst는 첫 관리자를 만들고 바로 로그인시킨다. 동시에 두 요청이 와도 하나만 성공한다.
func (m accountManager) CreateFirst(ctx context.Context, setupKey string, user model.Username, password string) (model.SessionToken, error) {
	if n, err := m.reader.Count(ctx); err != nil || n > 0 {
		return "", model.ErrSetupClosed
	}
	if !m.key.Matches(ctx, setupKey) {
		return "", model.ErrBadSetupKey
	}
	if len(password) < model.MinPasswordLength {
		return "", model.ErrWeakPassword
	}
	hash, err := m.hasher.Hash(password)
	if err != nil {
		return "", err
	}
	id, err := m.store.CreateFirst(ctx, user, hash)
	if err != nil {
		return "", err
	}
	return newSession(ctx, m.sessions, m.random, m.clock, id)
}

// ChangePassword는 지금 비밀번호를 확인하고 바꾼다. 지금 쓰는 세션만 남기고 다른 기기는 모두 로그아웃된다.
func (m accountManager) ChangePassword(ctx context.Context, who model.Account, current, next string, keep model.SessionToken) error {
	_, hash, err := m.reader.FindByName(ctx, who.Username.String())
	if err != nil {
		return err
	}
	if !m.hasher.Matches(current, hash) {
		return model.ErrWrongPassword
	}
	if len(next) < model.MinPasswordLength {
		return model.ErrWeakPassword
	}
	newHash, err := m.hasher.Hash(next)
	if err != nil {
		return err
	}
	if err := m.store.SetPassword(ctx, who.ID, newHash); err != nil {
		return err
	}
	return m.sessions.DeleteOthers(ctx, who.ID, digest(keep))
}

// Recover는 서버 셸에서 계정을 되찾을 때 쓴다. 그 계정의 세션은 모두 끊긴다.
func (m accountManager) Recover(ctx context.Context, user, password string) error {
	if len(password) < model.MinPasswordLength {
		return model.ErrWeakPassword
	}
	who, _, err := m.reader.FindByName(ctx, user)
	if err != nil {
		return err
	}
	hash, err := m.hasher.Hash(password)
	if err != nil {
		return err
	}
	if err := m.store.SetPassword(ctx, who.ID, hash); err != nil {
		return err
	}
	return m.sessions.DeleteOthers(ctx, who.ID, "")
}

// ResetAll은 모든 계정과 세션을 지운다. 다음 실행에서 첫 설정이 다시 열린다.
func (m accountManager) ResetAll(ctx context.Context) error { return m.store.DeleteAll(ctx) }

func (m accountManager) Names(ctx context.Context) ([]string, error) { return m.reader.Names(ctx) }
