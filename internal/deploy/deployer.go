// Package deploy는 배포를 정해진 순서로 한다 — 만들기(Build) → 바꿔 끼우기(Swap) → 기록 → 정리.
// 무엇을 어떻게 만들고 끼우는지는 종류 담당자(KindTools)가 안다. 이 패키지는 순서만 안다 (Template Method).
package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// Parts는 배포가 쓰는 것들이다. 모두 인터페이스다.
type Parts struct {
	Lock     contract.DeployLock
	History  contract.DeployHistoryStore
	Past     contract.DeployHistoryReader
	Logs     contract.DeployLog
	Services contract.ServiceReader
	Live     contract.LiveStateStore
	Kinds    contract.KindLookup
	Cleaner  contract.OldVersionCleaner
	Events   contract.EventPublisher
	Sync     contract.WebServerSync // 옛 것을 내리기 전에 웹서버가 새 것을 가리키게 한다
}

type deployer struct {
	p    Parts
	base context.Context
	log  *slog.Logger
	wg   *sync.WaitGroup
}

// NewDeployer는 배포기를 만든다. 배포는 base의 수명을 따른다 — Naru가 꺼지면 진행 중인 배포도 멈춘다.
// wait는 진행 중인 배포가 모두 끝나기를 기다린다 (종료·테스트용).
func NewDeployer(base context.Context, log *slog.Logger, p Parts) (d contract.Deployer, wait func()) {
	wg := &sync.WaitGroup{}
	return deployer{p: p, base: base, log: log, wg: wg}, wg.Wait
}

// Deploy는 배포를 시작하고 그 번호를 돌려준다. 이미 배포 중이면 끝난 뒤 한 번 더 한다 (model.ErrQueued).
func (d deployer) Deploy(ctx context.Context, id model.ServiceID, why model.DeployReason) (model.DeploymentID, error) {
	s, tools, err := d.deployable(ctx, id)
	if err != nil {
		return 0, err
	}
	return d.start(ctx, s, tools, why, nil)
}

// RollBack은 예전 배포(to)를 다시 올린다. 빌드하지 않고 그때의 이미지·파일을 그대로 쓴다.
// 배포 중이면 합치지 않고 거절한다 (model.ErrBusy).
func (d deployer) RollBack(ctx context.Context, id model.ServiceID, to model.DeploymentID) (model.DeploymentID, error) {
	prev, err := d.p.Past.Get(ctx, to)
	if err != nil || prev.ServiceID != id || prev.Status != model.DeploySuccess {
		return 0, model.ErrCannotRollBack
	}
	s, tools, err := d.deployable(ctx, id)
	if err != nil {
		return 0, err
	}
	return d.start(ctx, s, tools, model.ReasonRollBack, &prev)
}

func (d deployer) IsDeploying(id model.ServiceID) bool { return d.p.Lock.IsLocked(id) }

func (d deployer) deployable(ctx context.Context, id model.ServiceID) (model.Service, contract.KindTools, error) {
	s, err := d.p.Services.Get(ctx, id)
	if err != nil {
		return s, contract.KindTools{}, err
	}
	tools, ok := d.p.Kinds.Find(s.Kind)
	if !ok || tools.Builder == nil || tools.Swapper == nil {
		return s, tools, model.ErrNothingToDeploy
	}
	return s, tools, nil
}

func (d deployer) start(ctx context.Context, s model.Service, tools contract.KindTools, why model.DeployReason, from *model.Deployment) (model.DeploymentID, error) {
	queueAs := why
	if from != nil {
		queueAs = "" // 되돌리기는 합치지 않는다 — 끝난 뒤 저절로 다시 되돌리면 안 된다
	}
	unlock, locked := d.p.Lock.TryLock(s.ID, queueAs)
	if locked {
		if from != nil {
			return 0, model.ErrBusy
		}
		return 0, model.ErrQueued
	}
	did, err := d.p.History.Start(ctx, s.ID, why)
	if err != nil {
		unlock()
		return 0, err
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.run(s, tools, did, from)
		if again, ok := unlock(); ok {
			d.Deploy(d.base, s.ID, again)
		}
	}()
	return did, nil
}

// run은 배포 하나를 끝까지 한다:
// 만들기 → 새 것 띄우기 → 지금 상태 저장 → 웹서버 맞추기 → 옛 것 내리기 → 기록 → 정리 → 알림.
// 어디서 실패해도 지금 도는 것은 그대로 남는다. 웹서버가 새 것을 가리키기 전에는 옛 것을 내리지 않는다 —
// 설치형은 컨테이너 IP로 보내므로, 먼저 내리면 그 사이 요청이 끊긴다.
func (d deployer) run(s model.Service, tools contract.KindTools, did model.DeploymentID, from *model.Deployment) {
	ctx, cancel := context.WithTimeout(d.base, 30*time.Minute)
	defer cancel()
	w, closeLog := d.p.Logs.Open(did)
	started := time.Now()

	var v model.Version
	var err error
	if from != nil {
		w.Step(fmt.Sprintf("#%d 배포를 다시 올립니다", from.ID), fmt.Sprintf("restoring deployment #%d", from.ID))
		v = model.Version{
			Image:  from.Image,
			Folder: model.SiteFolder{Site: s.Name, Release: from.ID},
			Commit: model.Commit{Hash: from.Commit, Message: from.Message},
		}
	} else {
		v, err = tools.Builder.Build(ctx, model.BuildRequest{Service: s, Deployment: did}, w)
	}
	var live model.LiveState
	if err == nil {
		live, err = tools.Swapper.Swap(ctx, model.SwapRequest{Service: s, Deployment: did}, v, w)
	}
	if err == nil {
		err = d.p.Live.Save(ctx, s.ID, live)
	}
	if err == nil {
		if serr := d.p.Sync.SyncNow(ctx); serr != nil {
			fmt.Fprintf(w, "\n웹서버를 맞추지 못해 옛 버전을 남겨 둬요 — 웹서버가 다시 닿으면 새 버전으로 넘어가고, 옛 것은 다음 배포에서 정리해요 / "+
				"could not update the web server, so the old version is kept until it can: %v\n", serr)
		} else {
			tools.Swapper.Retire(ctx, s, live, w)
		}
	}

	result := model.Deployment{ID: did, ServiceID: s.ID, Commit: v.Commit.Hash, Message: v.Commit.Message, Image: v.Image}
	took := time.Since(started).Round(time.Second)
	if err != nil {
		fmt.Fprintf(w, "\n✗ %s\n", err)
		result.Status = model.DeployFailed
		d.log.Warn("deploy failed", "service", s.Name.String(), "deployment", did, "err", err)
	} else {
		fmt.Fprintf(w, "\n✓ %s (%s)\n", "완료 / done", took)
		result.Status = model.DeploySuccess
		d.log.Info("deployed", "service", s.Name.String(), "deployment", did, "took", took)
	}
	result.Log = closeLog()
	if ferr := d.p.History.Finish(context.Background(), result); ferr != nil {
		d.log.Error("record deploy", "err", ferr)
	}
	if err == nil {
		s.Live = live
		d.p.Cleaner.Clean(ctx, s, did)
		d.p.Events.Publish(model.SiteMapChanged{Reason: "deployed"})
	}
	d.p.Events.Publish(model.DeployFinished{Service: s.ID, Deployment: did, OK: err == nil})
}
