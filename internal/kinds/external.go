package kinds

import "github.com/KangminNa/naru/internal/model"

// ExternalDestination은 적어 둔 주소 그대로다 (https://로 시작하면 TLS로 보낸다).
type ExternalDestination struct{}

func (ExternalDestination) Find(s model.Service) model.Destination {
	return model.Destination{Address: s.External}
}

// ExternalStatus는 언제나 "연결만 함"이다 — Naru가 띄우는 것이 아니다.
type ExternalStatus struct{}

func (ExternalStatus) Read(model.Service, model.ContainerStates) model.ServiceStatus {
	return model.ServiceStatus{Key: "routed", Tone: "muted"}
}
