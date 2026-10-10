// Package system은 시계와 난수다.
package system

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Clock은 진짜 시계다.
type Clock struct{}

func (Clock) Now() time.Time { return time.Now() }

// Random은 암호학적으로 안전한 난수 문자열(16진수)을 만든다.
type Random struct{}

func (Random) New(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		panic("system: no randomness: " + err.Error()) // 난수가 없으면 비밀을 만들 수 없다 — 계속하면 안 된다
	}
	return hex.EncodeToString(b)
}
