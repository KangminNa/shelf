package engine

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"sync"
	"time"
)

// Status는 화면에 보여줄 엔진 상태다.
type Status struct {
	Connected bool
	LastError string
	AppliedAt time.Time
}

// Engine은 DB에서 그린 설정을 Caddy에 맞춰 둔다.
// 바뀔 때(Kick) 바로, 그리고 주기적으로 다시 확인한다 — Caddy가 재시작돼도, Naru가 재시작돼도 맞춰진다.
type Engine struct {
	admin  *Admin
	plan   func(context.Context) (Plan, error)
	log    *slog.Logger
	kick   chan struct{}
	every  time.Duration
	reload time.Duration // 같은 설정이어도 이 간격마다 다시 보낸다

	mu       sync.RWMutex
	status   Status
	lastHash [32]byte
	lastLoad time.Time
}

func New(admin *Admin, plan func(context.Context) (Plan, error), log *slog.Logger) *Engine {
	return &Engine{admin: admin, plan: plan, log: log, kick: make(chan struct{}, 1), every: 30 * time.Second, reload: 5 * time.Minute}
}

// Kick은 설정이 바뀌었다고 알린다. 막히지 않는다.
func (e *Engine) Kick() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

func (e *Engine) Status() Status {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.status
}

// retry는 연결되지 않았을 때 다시 시도하는 간격이다. 함께 뜬 웹서버가 조금 늦게 준비되는 일이 흔하다.
const retry = 3 * time.Second

func (e *Engine) Run(ctx context.Context) {
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.kick:
		case <-t.C:
		}
		next := e.every
		if e.Sync(ctx) != nil {
			next = retry
		}
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(next)
	}
}

// Sync는 지금의 DB를 Caddy에 반영한다. 바뀐 게 없으면 닿는지만 본다.
func (e *Engine) Sync(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	p, err := e.plan(ctx)
	if err != nil {
		e.fail("plan", err)
		return err
	}
	cfg, err := Render(p)
	if err != nil {
		e.fail("render", err)
		return err
	}
	hash := sha256.Sum256(cfg)

	e.mu.RLock()
	same := hash == e.lastHash && e.status.Connected && time.Since(e.lastLoad) < e.reload
	e.mu.RUnlock()
	if same {
		if err := e.admin.Ping(ctx); err != nil {
			e.fail("ping", err)
			return err
		}
		return nil
	}

	if err := e.admin.Load(ctx, cfg); err != nil {
		e.fail("load", err)
		return err
	}
	e.mu.Lock()
	changed := hash != e.lastHash
	e.lastHash, e.lastLoad = hash, time.Now()
	e.status = Status{Connected: true, AppliedAt: time.Now()}
	e.mu.Unlock()
	if changed {
		e.log.Info("web server config applied", "sites", len(p.Sites), "admin", p.AdminHosts, "open_fallback", p.OpenFallback)
	}
	return nil
}

func (e *Engine) fail(stage string, err error) {
	e.mu.Lock()
	was := e.status.Connected || e.status.LastError != err.Error()
	e.status.Connected = false
	e.status.LastError = err.Error()
	e.lastHash = [32]byte{} // 다음에 다시 보낸다
	e.mu.Unlock()
	if was {
		e.log.Warn("web server unreachable or refused config", "stage", stage, "err", err)
	}
}
