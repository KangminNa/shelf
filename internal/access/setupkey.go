package access

import (
	"context"
	"crypto/subtle"

	"github.com/KangminNa/naru/internal/contract"
)

// RandomSetupKey는 이번 실행에서만 쓰는 첫 설정 열쇠다. 서버 로그에만 찍힌다.
// 계정이 하나라도 생기면 더는 맞지 않는다.
type RandomSetupKey struct {
	value    string
	accounts contract.AccountReader
}

func NewRandomSetupKey(accounts contract.AccountReader, random contract.RandomTokens) RandomSetupKey {
	return RandomSetupKey{value: random.New(16), accounts: accounts}
}

func (k RandomSetupKey) Value() string { return k.value }

func (k RandomSetupKey) Matches(ctx context.Context, key string) bool {
	if key == "" {
		return false
	}
	if n, err := k.accounts.Count(ctx); err != nil || n > 0 {
		return false // 읽지 못하면 열어주지 않는다
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(k.value)) == 1
}
