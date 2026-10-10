// Package access는 관리자로 들어오는 일을 맡는다 — 첫 계정, 로그인, 세션, 비밀번호.
// 관리자는 Docker 소켓을 쥐므로 서버의 root와 같다 — 그래서 계정은 처음 한 번,
// 서버 로그를 볼 수 있는 사람(설정 열쇠를 가진 사람)만 만들 수 있다.
package access

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"strings"

	"github.com/KangminNa/naru/internal/model"
	"golang.org/x/crypto/scrypt"
)

// v1(Node crypto.scryptSync 기본값)과 같은 매개변수 — 바꾸면 기존 계정이 로그인하지 못한다.
const (
	scryptN      = 16384
	scryptR      = 8
	scryptP      = 1
	scryptKeyLen = 64
)

// ScryptHasher는 "salt:hash" 형식으로 만든다. v1처럼 salt는 hex 문자열을 그대로 바이트로 쓴다.
type ScryptHasher struct{}

func (ScryptHasher) Hash(password string) (model.PasswordHash, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	salt := hex.EncodeToString(raw)
	key, err := scrypt.Key([]byte(password), []byte(salt), scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return "", err
	}
	return model.PasswordHash(salt + ":" + hex.EncodeToString(key)), nil
}

// Matches는 v1과 v2가 만든 해시를 모두 검증한다.
func (ScryptHasher) Matches(password string, h model.PasswordHash) bool {
	salt, want, ok := strings.Cut(string(h), ":")
	if !ok || salt == "" || want == "" {
		return false
	}
	wantKey, err := hex.DecodeString(want)
	if err != nil || len(wantKey) != scryptKeyLen {
		return false
	}
	got, err := scrypt.Key([]byte(password), []byte(salt), scryptN, scryptR, scryptP, scryptKeyLen)
	return err == nil && subtle.ConstantTimeCompare(got, wantKey) == 1
}
