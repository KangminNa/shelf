package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// SessionTTL은 로그인이 유지되는 기간이다.
const SessionTTL = 7 * 24 * time.Hour

// digest는 세션 토큰의 SHA-256이다. DB에는 이것만 남겨 DB가 새도 세션을 훔칠 수 없게 한다.
func digest(t model.SessionToken) model.SessionDigest {
	sum := sha256.Sum256([]byte(t))
	return model.SessionDigest(hex.EncodeToString(sum[:]))
}

// newSession은 쿠키에 넣을 토큰을 만들고 그 해시를 저장한다.
func newSession(ctx context.Context, sessions contract.SessionStore, random contract.RandomTokens, clock contract.Clock, who model.AccountID) (model.SessionToken, error) {
	t := model.SessionToken(random.New(32))
	if err := sessions.Save(ctx, digest(t), who, clock.Now().Add(SessionTTL)); err != nil {
		return "", err
	}
	return t, nil
}

type loginManager struct {
	accounts contract.AccountReader
	sessions contract.SessionStore
	hasher   contract.PasswordHasher
	limiter  contract.LoginLimiter
	random   contract.RandomTokens
	clock    contract.Clock
}

func NewLoginManager(accounts contract.AccountReader, sessions contract.SessionStore, hasher contract.PasswordHasher,
	limiter contract.LoginLimiter, random contract.RandomTokens, clock contract.Clock) contract.LoginManager {
	return loginManager{accounts, sessions, hasher, limiter, random, clock}
}

// LogIn은 비밀번호를 확인하고 세션을 연다. 같은 IP에서 연속으로 틀리면 잠근다.
func (m loginManager) LogIn(ctx context.Context, ip, user, password string) (model.SessionToken, error) {
	if !m.limiter.Allowed(ip) {
		return "", model.ErrLocked
	}
	who, hash, err := m.accounts.FindByName(ctx, user)
	if err != nil || !m.hasher.Matches(password, hash) {
		m.limiter.Failed(ip)
		return "", model.ErrBadLogin
	}
	m.limiter.Succeeded(ip)
	return newSession(ctx, m.sessions, m.random, m.clock, who.ID)
}

// WhoIs는 세션의 주인이다. 만료됐거나 계정이 사라졌으면 없다.
func (m loginManager) WhoIs(ctx context.Context, t model.SessionToken) (model.Account, bool) {
	if t == "" {
		return model.Account{}, false
	}
	return m.sessions.FindOwner(ctx, digest(t), m.clock.Now())
}

func (m loginManager) LogOut(ctx context.Context, t model.SessionToken) {
	if t != "" {
		m.sessions.Delete(ctx, digest(t))
	}
}
