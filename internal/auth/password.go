package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"strings"

	"golang.org/x/crypto/scrypt"
)

// v1(Node crypto.scryptSync 기본값)과 같은 매개변수 — 바꾸면 기존 계정이 로그인하지 못한다.
const (
	scryptN      = 16384
	scryptR      = 8
	scryptP      = 1
	scryptKeyLen = 64
)

// HashPassword는 "salt:hash" 형식으로 만든다. v1처럼 salt는 hex 문자열을 그대로 바이트로 쓴다.
func HashPassword(password string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	salt := hex.EncodeToString(raw)
	key, err := scrypt.Key([]byte(password), []byte(salt), scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return "", err
	}
	return salt + ":" + hex.EncodeToString(key), nil
}

// VerifyPassword는 v1과 v2가 만든 해시를 모두 검증한다.
func VerifyPassword(password, stored string) bool {
	salt, want, ok := strings.Cut(stored, ":")
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
