package deploy

import (
	"sync"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// memoryDeployLock은 서비스마다 배포를 한 번에 하나만 하게 한다.
// 배포 중에 온 요청은 "끝나고 한 번 더" 하나로 합친다 — push가 열 번 와도 배포는 두 번이다.
type memoryDeployLock struct {
	mu    sync.Mutex
	busy  map[model.ServiceID]bool
	again map[model.ServiceID]model.DeployReason
}

func NewMemoryDeployLock() contract.DeployLock {
	return &memoryDeployLock{busy: map[model.ServiceID]bool{}, again: map[model.ServiceID]model.DeployReason{}}
}

// TryLock은 잠그고 풀 함수를 준다. 이미 잠겨 있으면 why를 "한 번 더"로 남기고 locked가 참이다 (why가 비면 남기지 않는다).
// 풀 때 남은 "한 번 더"가 있으면 그 이유를 돌려준다.
func (l *memoryDeployLock) TryLock(id model.ServiceID, why model.DeployReason) (func() (model.DeployReason, bool), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.busy[id] {
		if why != "" {
			l.again[id] = why
		}
		return nil, true
	}
	l.busy[id] = true
	return func() (model.DeployReason, bool) {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.busy, id)
		again, ok := l.again[id]
		delete(l.again, id)
		return again, ok
	}, false
}

func (l *memoryDeployLock) IsLocked(id model.ServiceID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.busy[id]
}
