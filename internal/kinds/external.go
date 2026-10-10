package kinds

import (
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// thisServer는 저장된 주소에서 "이 서버"를 뜻하는 이름이다 (localhost를 적으면 이것으로 저장된다).
const thisServer = "host.docker.internal"

// ExternalDestination은 적어 둔 주소다 (https://로 시작하면 TLS로 보낸다).
// ThisServer가 있으면 "이 서버"를 그 주소로 바꿔 그린다 — 설치형은 127.0.0.1. 저장된 값은 그대로 둔다.
type ExternalDestination struct{ ThisServer string }

func (e ExternalDestination) Find(s model.Service) model.Destination {
	addr := s.External
	if e.ThisServer != "" {
		scheme, rest, ok := strings.Cut(addr, "://")
		if !ok {
			scheme, rest = "", addr
		} else {
			scheme += "://"
		}
		if host, port, found := strings.Cut(rest, ":"); found && host == thisServer {
			addr = scheme + e.ThisServer + ":" + port
		}
	}
	return model.Destination{Address: addr}
}

// ExternalStatus는 언제나 "연결만 함"이다 — Naru가 띄우는 것이 아니다.
type ExternalStatus struct{}

func (ExternalStatus) Read(model.Service, model.ContainerStates) model.ServiceStatus {
	return model.ServiceStatus{Key: "routed", Tone: "muted"}
}
