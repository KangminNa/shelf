package deploy

import (
	"context"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// ControlParts는 멈추기·켜기·지우기가 쓰는 것들이다.
type ControlParts struct {
	Services contract.ServiceReader
	Store    contract.ServiceStore
	Live     contract.LiveStateStore
	Lock     contract.DeployLock
	Switch   contract.ContainerSwitch
	Remover  contract.ContainerRemover
	Watcher  contract.ContainerWatcher
	Images   contract.ImageCleaner
	Files    contract.SiteFiles
	Past     contract.DeployHistoryReader
	Events   contract.EventPublisher
}

type serviceControl struct{ p ControlParts }

func NewServiceControl(p ControlParts) contract.ServiceControl { return serviceControl{p} }

// Stop은 컨테이너를 멈춘다. 직접 멈춘 것으로 표시해 두어 장애로 보이지 않게 한다.
func (c serviceControl) Stop(ctx context.Context, id model.ServiceID) error {
	s, err := c.p.Services.Get(ctx, id)
	if err != nil {
		return err
	}
	name := s.Live.CurrentContainer()
	if name == "" {
		return model.ErrNothingToDeploy
	}
	s.Live.Stopped = true
	if err := c.p.Live.Save(ctx, id, s.Live); err != nil {
		return err
	}
	return c.p.Switch.TurnOff(ctx, name)
}

func (c serviceControl) Start(ctx context.Context, id model.ServiceID) error {
	s, err := c.p.Services.Get(ctx, id)
	if err != nil {
		return err
	}
	name := s.Live.CurrentContainer()
	if name == "" {
		return model.ErrNothingToDeploy
	}
	if err := c.p.Switch.TurnOn(ctx, name); err != nil {
		return err
	}
	s.Live.Stopped = false
	return c.p.Live.Save(ctx, id, s.Live)
}

// Remove는 서비스와 그 컨테이너·빌드 이미지·정적 파일을 모두 지운다. 다른 곳의 데이터(볼륨)는 건드리지 않는다.
func (c serviceControl) Remove(ctx context.Context, id model.ServiceID) error {
	if c.p.Lock.IsLocked(id) {
		return model.ErrBusy
	}
	s, err := c.p.Services.Get(ctx, id)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	if n := s.Live.CurrentContainer(); n != "" {
		names[n] = true
	}
	if more, err := c.p.Watcher.BelongingTo(ctx, id); err == nil {
		for _, n := range more {
			names[n] = true
		}
	}
	for n := range names {
		c.p.Remover.Remove(ctx, n)
	}
	if s.Live.CurrentContainer() != "" {
		if ids, err := c.p.Past.Succeeded(ctx, id); err == nil {
			for _, d := range ids {
				c.p.Images.Remove(ctx, model.ImageTag(s.Name, d))
			}
		}
	}
	c.p.Files.RemoveSite(s.Name) // 정적 사이트가 아니면 폴더가 없다
	if err := c.p.Store.Delete(ctx, id); err != nil {
		return err
	}
	c.p.Events.Publish(model.ServiceDeleted{Service: id})
	c.p.Events.Publish(model.SiteMapChanged{Reason: "service deleted"})
	return nil
}
