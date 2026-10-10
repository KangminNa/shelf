// Package notify는 일어난 일을 등록한 알림 주소로 보낸다 (v2-objects §4 K).
// 이벤트를 들으면 줄에 넣고, 따로 도는 일꾼이 보낸다 — 보내기가 느려도 배포·지켜보기를 막지 않는다.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

// Parts는 알리기가 쓰는 것들이다.
type Parts struct {
	Channels   contract.ChannelStore
	Deliveries contract.DeliveryLog
	Sender     contract.AlertSender
	Services   contract.ServiceReader // 배포 실패 알림에 서비스 이름을 쓴다
	Events     contract.EventSubscriber
	Clock      contract.Clock
	Log        *slog.Logger
}

type alerts struct {
	p     Parts
	queue chan model.Event
}

// NewAlerts는 알리기를 만든다. run은 ctx가 끝날 때까지 줄에 쌓인 알림을 보낸다.
func NewAlerts(p Parts) (contract.AlertSettings, func(context.Context)) {
	a := &alerts{p: p, queue: make(chan model.Event, 100)}
	p.Events.Subscribe(func(e model.Event) {
		switch x := e.(type) {
		case model.ServiceDown, model.ServiceUp, model.WebServerDown, model.WebServerUp, model.DockerDown, model.DockerUp, model.CertificateEndingSoon:
		case model.DeployFinished:
			if x.OK {
				return
			}
		default:
			return
		}
		select {
		case a.queue <- e:
		default:
			p.Log.Warn("alert queue full — dropping an alert", "event", fmt.Sprintf("%T", e))
		}
	})
	return a, a.run
}

func (a *alerts) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-a.queue:
			alert := a.describe(ctx, e)
			channels, err := a.p.Channels.List(ctx)
			if err != nil {
				a.p.Log.Error("alert channels", "err", err)
				continue
			}
			for _, ch := range channels {
				a.send(ctx, ch, alert)
			}
		}
	}
}

// send는 한 주소로 보내고 결과를 남긴다. 실패 이유에 주소 전체는 넣지 않는다 (AlertSender가 약속한다).
func (a *alerts) send(ctx context.Context, ch model.AlertChannel, alert model.Alert) error {
	err := a.p.Sender.Send(ctx, ch, alert)
	d := model.Delivery{Channel: ch.ID, ChannelName: ch.Name, Event: alert.Event, Title: alert.Title, OK: err == nil, At: a.p.Clock.Now()}
	if err != nil {
		d.Detail = err.Error()
		a.p.Log.Warn("alert not delivered", "channel", ch.Name, "event", alert.Event, "err", err)
	}
	a.p.Deliveries.Save(ctx, d)
	return err
}

// describe는 일어난 일을 사람이 읽을 알림으로 바꾼다 (한국어 / English).
func (a *alerts) describe(ctx context.Context, e model.Event) model.Alert {
	switch x := e.(type) {
	case model.ServiceDown:
		return model.Alert{Event: "service.down", Level: model.AlertProblem, Service: x.Name,
			Title: "서비스가 멈췄어요: " + x.Name + " / Service down: " + x.Name, Detail: x.Name + ": " + downWhy(x.Why)}
	case model.ServiceUp:
		return model.Alert{Event: "service.up", Level: model.AlertRecovery, Service: x.Name,
			Title: "서비스가 돌아왔어요: " + x.Name + " / Service recovered: " + x.Name, Detail: x.Name + " 다시 응답해요 / is answering again"}
	case model.DeployFinished:
		name := fmt.Sprintf("#%d", x.Service)
		if s, err := a.p.Services.Get(ctx, x.Service); err == nil {
			name = s.Name.String()
		}
		return model.Alert{Event: "deploy.failed", Level: model.AlertProblem, Service: name,
			Title:  "배포 실패: " + name + " / Deploy failed: " + name,
			Detail: fmt.Sprintf("배포 #%d이 끝나지 못했어요. 지금 돌던 버전은 그대로예요 / deployment #%d failed; the running version is untouched", x.Deployment, x.Deployment)}
	case model.WebServerDown:
		return model.Alert{Event: "webserver.down", Level: model.AlertProblem, Title: "웹서버에 닿지 않아요 / Web server unreachable", Detail: x.Why}
	case model.WebServerUp:
		return model.Alert{Event: "webserver.up", Level: model.AlertRecovery, Title: "웹서버에 다시 닿아요 / Web server reachable again"}
	case model.DockerDown:
		return model.Alert{Event: "docker.down", Level: model.AlertProblem, Title: "Docker에 닿지 않아요 / Docker unreachable", Detail: x.Why}
	case model.DockerUp:
		return model.Alert{Event: "docker.up", Level: model.AlertRecovery, Title: "Docker에 다시 닿아요 / Docker reachable again"}
	case model.CertificateEndingSoon:
		days := int(x.NotAfter.Sub(a.p.Clock.Now()).Hours() / 24)
		return model.Alert{Event: "certificate.ending", Level: model.AlertProblem,
			Title:  "인증서가 곧 끝나요: " + x.Domain + " / Certificate ending soon: " + x.Domain,
			Detail: fmt.Sprintf("%d일 남았어요. 보통은 30일 전에 갱신돼요 — 갱신이 막힌 것 같아요 / %d days left; renewal seems stuck", days, days)}
	}
	return model.Alert{Event: "unknown", Level: model.AlertInfo, Title: fmt.Sprintf("%T", e)}
}

func downWhy(why string) string {
	switch why {
	case "crashed":
		return "컨테이너가 멈췄어요 / the container exited"
	case "restarting":
		return "컨테이너가 계속 다시 시작돼요 / the container keeps restarting"
	case "missing":
		return "컨테이너가 없어요 / the container is gone"
	case "noanswer":
		return "앱 포트가 응답하지 않아요 / the app port does not answer"
	}
	return why
}

// ── 설정 화면 ─────────────────────────────

func (a *alerts) Channels(ctx context.Context) ([]model.ChannelView, error) {
	all, err := a.p.Channels.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.ChannelView, 0, len(all))
	for _, c := range all {
		out = append(out, model.ChannelView{ID: c.ID, Name: c.Name, Target: model.AlertHostOf(c.URL) + "/…",
			Format: model.AlertFormatOf(c.URL), HasSecret: c.Secret != ""})
	}
	return out, nil
}

func (a *alerts) Add(ctx context.Context, in model.ChannelInput) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = model.AlertHostOf(in.URL.String())
	}
	if len(name) > 60 {
		name = name[:60]
	}
	_, err := a.p.Channels.Add(ctx, model.AlertChannel{Name: name, URL: in.URL.String(), Secret: strings.TrimSpace(in.Secret)})
	return err
}

func (a *alerts) Remove(ctx context.Context, id model.ChannelID) error {
	return a.p.Channels.Remove(ctx, id)
}

// Test는 그 주소로 시험 알림을 지금 보내고, 안 되면 그 이유를 돌려준다.
func (a *alerts) Test(ctx context.Context, id model.ChannelID) error {
	all, err := a.p.Channels.List(ctx)
	if err != nil {
		return err
	}
	for _, c := range all {
		if c.ID == id {
			return a.send(ctx, c, model.Alert{Event: "test", Level: model.AlertInfo,
				Title: "Naru 알림 시험 / Naru test alert", Detail: "이 메시지가 보이면 알림이 잘 와요 / alerts reach you"})
		}
	}
	return model.ErrNotFound
}

func (a *alerts) Deliveries(ctx context.Context, n int) ([]model.Delivery, error) {
	return a.p.Deliveries.Recent(ctx, n)
}
