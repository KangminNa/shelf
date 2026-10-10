// Package events는 일어난 일을 구독자에게 전한다 (프로세스 안, 동기).
package events

import (
	"sync"

	"github.com/KangminNa/naru/internal/model"
)

// Bus는 EventPublisher이자 EventSubscriber다. 구독자는 빨리 돌아와야 한다 (오래 걸리면 신호만 남기고 나중에).
type Bus struct {
	mu        sync.RWMutex
	listeners []func(model.Event)
}

func NewBus() *Bus { return &Bus{} }

func (b *Bus) Subscribe(listen func(model.Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = append(b.listeners, listen)
}

func (b *Bus) Publish(e model.Event) {
	b.mu.RLock()
	listeners := append([]func(model.Event){}, b.listeners...)
	b.mu.RUnlock()
	for _, l := range listeners {
		l(e)
	}
}
