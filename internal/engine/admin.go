package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Admin은 Caddy 관리 API를 유닉스 소켓으로 부른다 (불변식 1 — TCP로 열면 모든 앱 컨테이너가 프록시를 바꿀 수 있다).
type Admin struct {
	socket string
	http   *http.Client
}

func NewAdmin(socket string) *Admin {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		MaxIdleConns:    2,
		IdleConnTimeout: 30 * time.Second,
	}
	return &Admin{socket: socket, http: &http.Client{Transport: transport, Timeout: 30 * time.Second}}
}

func (a *Admin) Socket() string { return a.socket }

// Load는 설정 전체를 교체한다. 관리 소켓이 그대로인지 먼저 확인한다.
func (a *Admin) Load(ctx context.Context, cfg []byte) error {
	if err := CheckAdmin(cfg, a.socket); err != nil {
		return err
	}
	_, err := a.do(ctx, http.MethodPost, "/load", cfg)
	return err
}

// Ping은 Caddy에 닿는가.
func (a *Admin) Ping(ctx context.Context) error {
	_, err := a.do(ctx, http.MethodGet, "/config/admin", nil)
	return err
}

func (a *Admin) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	// 유닉스 소켓의 관리 API는 Host가 127.0.0.1(또는 빈 값)일 때만 받는다.
	req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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
