package caddy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/contract"
)

// SocketSender는 Caddy 관리 API를 유닉스 소켓으로 부른다 (불변식 1 — TCP로 열면 모든 앱 컨테이너가 프록시를 바꿀 수 있다).
type SocketSender struct {
	socket string
	http   *http.Client
}

func NewSocketSender(socket string) *SocketSender {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		MaxIdleConns:    2,
		IdleConnTimeout: 30 * time.Second,
	}
	return &SocketSender{socket: socket, http: &http.Client{Transport: transport, Timeout: 30 * time.Second}}
}

// Send는 설정 전체를 교체한다 (POST /load).
func (a *SocketSender) Send(ctx context.Context, cfg []byte) error {
	if _, err := a.do(ctx, http.MethodPost, "/load", "application/json", cfg); err != nil {
		return caddySaid(err)
	}
	return nil
}

// Ping은 Caddy에 닿는가.
func (a *SocketSender) Ping(ctx context.Context) error {
	_, err := a.do(ctx, http.MethodGet, "/config/admin", "", nil)
	return err
}

func (a *SocketSender) do(ctx context.Context, method, path, contentType string, body []byte) ([]byte, error) {
	// 유닉스 소켓의 관리 API는 Host가 127.0.0.1(또는 빈 값)일 때만 받는다.
	req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caddy: %w", err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("caddy: %s %s: %d %s", method, path, res.StatusCode, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// AdminSocketGuard는 보내기 직전에 관리 소켓 설정이 그대로 들어 있는지 확인한다 (불변식 2).
// 빠진 설정을 보내면 Caddy는 200으로 받아들이고 Naru는 제어를 영영 잃는다 — 그래서 감싸서 빠뜨릴 수 없게 한다.
type AdminSocketGuard struct {
	inner  contract.ConfigSender
	socket string
}

func NewAdminSocketGuard(inner contract.ConfigSender, socket string) AdminSocketGuard {
	return AdminSocketGuard{inner: inner, socket: socket}
}

func (g AdminSocketGuard) Send(ctx context.Context, cfg []byte) error {
	if err := checkAdmin(cfg, g.socket); err != nil {
		return err
	}
	return g.inner.Send(ctx, cfg)
}

func (g AdminSocketGuard) Ping(ctx context.Context) error { return g.inner.Ping(ctx) }
