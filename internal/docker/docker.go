// Package docker는 Docker Engine API를 유닉스 소켓으로 직접 부른다.
// 공식 SDK는 의존성이 크고, 필요한 건 엔드포인트 몇 개뿐이다.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	http *http.Client
}

// New는 socketPath(보통 /var/run/docker.sock)로 붙는 클라이언트를 만든다.
func New(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:    4,
		IdleConnTimeout: 30 * time.Second,
	}
	return &Client{http: &http.Client{Transport: transport, Timeout: 10 * time.Second}}
}

// State는 화면에 보여줄 컨테이너 상태다.
type State string

const (
	Running State = "running"
)

type Container struct {
	Name   string
	Image  string
	State  State
	Status string            // "Up 3 hours", "Exited (1) 2 minutes ago"
	IPs    map[string]string // 네트워크 이름 → 그 네트워크에서의 IP
}

// Ping은 Docker에 닿는가.
// Containers는 모든 컨테이너를 이름(앞의 / 없이) → 상태로 돌려준다. 앱 수와 상관없이 한 번 부른다.
func (c *Client) containers(ctx context.Context) (map[string]Container, error) {
	res, err := c.get(ctx, "/containers/json?all=1")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var raw []struct {
		Names           []string
		Image           string
		State           string
		Status          string
		NetworkSettings struct {
			Networks map[string]struct{ IPAddress string }
		}
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("docker: decode containers: %w", err)
	}
	out := make(map[string]Container, len(raw))
	for _, r := range raw {
		for _, n := range r.Names {
			name := strings.TrimPrefix(n, "/")
			ips := map[string]string{}
			for net, n := range r.NetworkSettings.Networks {
				ips[net] = n.IPAddress
			}
			out[name] = Container{Name: name, Image: r.Image, State: State(r.State), Status: r.Status, IPs: ips}
		}
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker: %w", err)
	}
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		res.Body.Close()
		return nil, fmt.Errorf("docker: %s %s: %d %s", http.MethodGet, path, res.StatusCode, strings.TrimSpace(string(body)))
	}
	return res, nil
}
