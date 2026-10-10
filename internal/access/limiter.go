package access

import (
	"sync"
	"time"

	"github.com/KangminNa/naru/internal/contract"
)

const (
	MaxLoginFailures = 5
	LockoutDuration  = 15 * time.Minute
)

// MemoryLoginLimiter는 같은 IP에서 다섯 번 틀리면 15분 잠근다. 메모리에만 있다 — 다시 켜면 풀린다.
type MemoryLoginLimiter struct {
	clock contract.Clock

	mu       sync.Mutex
	failures map[string]*failure
}

type failure struct {
	count       int
	lockedUntil time.Time
}

func NewMemoryLoginLimiter(clock contract.Clock) *MemoryLoginLimiter {
	return &MemoryLoginLimiter{clock: clock, failures: map[string]*failure{}}
}

func (l *MemoryLoginLimiter) Allowed(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.failures[ip]
	return f == nil || !f.lockedUntil.After(l.clock.Now())
}

func (l *MemoryLoginLimiter) Failed(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.failures[ip]
	if f == nil {
		f = &failure{}
		l.failures[ip] = f
	}
	f.count++
	if f.count >= MaxLoginFailures {
		f.lockedUntil = l.clock.Now().Add(LockoutDuration)
		f.count = 0
	}
}

func (l *MemoryLoginLimiter) Succeeded(ip string) {
	l.mu.Lock()
	delete(l.failures, ip)
	l.mu.Unlock()
}
