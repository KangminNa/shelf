package deploy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// maxLog보다 길면 앞을 잘라 끝을 남긴다 — 실패 원인은 대개 끝에 있다.
const maxLog = 512 << 10

type deployLog struct {
	history contract.DeployHistoryStore
	every   time.Duration
}

// NewDeployLog는 배포 기록을 모으고 1초마다 저장한다 — 화면이 진행 중에도 볼 수 있게.
func NewDeployLog(history contract.DeployHistoryStore) contract.DeployLog {
	return deployLog{history: history, every: time.Second}
}

func (l deployLog) Open(d model.DeploymentID) (contract.DeployLogWriter, func() string) {
	w := &logWriter{save: func(s string) { l.history.SaveLog(context.Background(), d, s) }, done: make(chan struct{})}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		t := time.NewTicker(l.every)
		defer t.Stop()
		for {
			select {
			case <-w.done:
				return
			case <-t.C:
				w.flush()
			}
		}
	}()
	return w, w.close
}

type logWriter struct {
	mu    sync.Mutex
	buf   []byte
	dirty bool
	save  func(string)
	done  chan struct{}
	wg    sync.WaitGroup
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > maxLog {
		w.buf = append([]byte("…\n"), w.buf[len(w.buf)-maxLog:]...)
	}
	w.dirty = true
	w.mu.Unlock()
	return len(p), nil
}

// Step은 "▶ 한국어 / English" 한 줄을 쓴다. 화면은 이 줄로 배포 단계를 그린다.
func (w *logWriter) Step(ko, en string) { fmt.Fprintf(w, "\n▶ %s / %s\n", ko, en) }

func (w *logWriter) flush() {
	w.mu.Lock()
	if !w.dirty {
		w.mu.Unlock()
		return
	}
	s := string(w.buf)
	w.dirty = false
	w.mu.Unlock()
	w.save(s)
}

func (w *logWriter) close() string {
	close(w.done)
	w.wg.Wait()
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}
