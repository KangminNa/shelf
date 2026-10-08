package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 빌드·받기는 오래 걸린다. 짧은 기본 제한시간이 아니라 호출한 쪽의 ctx로 끊는다.
func (c *Client) stream(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	long := &http.Client{Transport: c.http.Transport}
	res, err := long.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker: %w", err)
	}
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		res.Body.Close()
		return nil, fmt.Errorf("docker: %s %s: %d %s", method, path, res.StatusCode, apiMessage(msg))
	}
	return res, nil
}

func (c *Client) call(ctx context.Context, method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", ErrNotFound, apiMessage(raw))
	}
	if res.StatusCode >= 300 && res.StatusCode != http.StatusNotModified {
		return fmt.Errorf("docker: %s %s: %d %s", method, path, res.StatusCode, apiMessage(raw))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

var ErrNotFound = errors.New("docker: not found")

func apiMessage(raw []byte) string {
	var m struct{ Message string }
	if json.Unmarshal(raw, &m) == nil && m.Message != "" {
		return m.Message
	}
	return strings.TrimSpace(string(raw))
}

// streamMessage는 build·pull이 한 줄씩 보내는 JSON이다.
type streamMessage struct {
	Stream string `json:"stream"`
	Status string `json:"status"`
	ID     string `json:"id"`
	Error  string `json:"error"`
	Aux    struct {
		ID string `json:"ID"`
	} `json:"aux"`
}

// follow는 진행 메시지를 log로 흘리고, 오류와 마지막 이미지 ID를 돌려준다.
func follow(r io.Reader, log io.Writer) (imageID string, err error) {
	dec := json.NewDecoder(r)
	lastStatus := ""
	for {
		var m streamMessage
		if err := dec.Decode(&m); err == io.EOF {
			return imageID, nil
		} else if err != nil {
			return imageID, fmt.Errorf("docker: reading progress: %w", err)
		}
		if m.Error != "" {
			return imageID, errors.New(strings.TrimSpace(m.Error))
		}
		if m.Aux.ID != "" {
			imageID = m.Aux.ID
		}
		if m.Stream != "" {
			io.WriteString(log, m.Stream)
		}
		// 받기 진행률은 레이어마다 수백 줄이 나온다 — 상태가 바뀔 때만 적는다
		if m.Status != "" && m.ID == "" && m.Status != lastStatus {
			fmt.Fprintln(log, m.Status)
			lastStatus = m.Status
		}
	}
}

// Build는 tar로 묶은 빌드 컨텍스트로 이미지를 만든다. 진행은 log로 흐른다.
func (c *Client) Build(ctx context.Context, tarball io.Reader, tag, dockerfile string, log io.Writer) (string, error) {
	q := url.Values{"t": {tag}, "rm": {"1"}, "forcerm": {"1"}}
	if dockerfile != "" {
		q.Set("dockerfile", dockerfile)
	}
	res, err := c.stream(ctx, http.MethodPost, "/build?"+q.Encode(), tarball, "application/x-tar")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	id, err := follow(res.Body, log)
	if err != nil {
		return "", err
	}
	if id == "" { // 오래된 빌더는 aux를 주지 않는다 — 태그로 찾는다
		img, ierr := c.Image(ctx, tag)
		if ierr != nil {
			return "", ierr
		}
		id = img.ID
	}
	return id, nil
}

// Pull은 이미지를 받는다. ref에는 태그나 다이제스트가 붙어 있어도 된다.
func (c *Client) Pull(ctx context.Context, ref string, log io.Writer) error {
	name, tag := splitRef(ref)
	q := url.Values{"fromImage": {name}}
	if tag != "" {
		q.Set("tag", tag)
	}
	res, err := c.stream(ctx, http.MethodPost, "/images/create?"+q.Encode(), nil, "")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = follow(res.Body, log)
	return err
}

// splitRef는 "ghcr.io:443/me/api:v1" → ("ghcr.io:443/me/api", "v1"). 다이제스트(@sha256:…)는 이름에 둔다.
func splitRef(ref string) (name, tag string) {
	if strings.Contains(ref, "@") {
		return ref, ""
	}
	slash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")
	if colon > slash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, "latest"
}

type Image struct {
	ID    string
	Ports []int // EXPOSE 한 TCP 포트
}

func (c *Client) Image(ctx context.Context, ref string) (Image, error) {
	var raw struct {
		ID     string `json:"Id"`
		Config struct {
			ExposedPorts map[string]struct{}
		}
	}
	if err := c.call(ctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/json", nil, &raw); err != nil {
		return Image{}, err
	}
	img := Image{ID: raw.ID}
	for p := range raw.Config.ExposedPorts {
		num, proto, _ := strings.Cut(p, "/")
		if proto != "" && proto != "tcp" {
			continue
		}
		if n, err := strconv.Atoi(num); err == nil {
			img.Ports = append(img.Ports, n)
		}
	}
	sort.Ints(img.Ports)
	return img, nil
}

func (c *Client) RemoveImage(ctx context.Context, ref string) error {
	return c.call(ctx, http.MethodDelete, "/images/"+url.PathEscape(ref), nil, nil)
}

// Spec은 만들 컨테이너다.
type Spec struct {
	Name    string
	Image   string
	Env     []string
	Labels  map[string]string
	Binds   []string
	Network string
	Aliases []string // 네트워크에서 이 이름들로도 불린다
}

func (c *Client) Create(ctx context.Context, s Spec) (string, error) {
	body := map[string]any{
		"Image":  s.Image,
		"Env":    s.Env,
		"Labels": s.Labels,
		"HostConfig": map[string]any{
			"RestartPolicy": map[string]string{"Name": "unless-stopped"},
			"Binds":         s.Binds,
			"NetworkMode":   s.Network,
		},
	}
	if s.Network != "" {
		body["NetworkingConfig"] = map[string]any{
			"EndpointsConfig": map[string]any{s.Network: map[string]any{"Aliases": s.Aliases}},
		}
	}
	var out struct {
		ID string `json:"Id"`
	}
	err := c.call(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(s.Name), body, &out)
	return out.ID, err
}

func (c *Client) Start(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(name)+"/start", nil, nil)
}

// Stop은 SIGTERM 뒤 grace만큼 기다린다.
func (c *Client) Stop(ctx context.Context, name string, grace time.Duration) error {
	return c.call(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/stop?t=%d", url.PathEscape(name), int(grace.Seconds())), nil, nil)
}

func (c *Client) Remove(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodDelete, "/containers/"+url.PathEscape(name)+"?force=1", nil, nil)
}

type Inspection struct {
	Running  bool
	Status   string
	ExitCode int
}

func (c *Client) Inspect(ctx context.Context, name string) (Inspection, error) {
	var raw struct {
		State struct {
			Running  bool
			Status   string
			ExitCode int
		}
	}
	if err := c.call(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil, &raw); err != nil {
		return Inspection{}, err
	}
	return Inspection{Running: raw.State.Running, Status: raw.State.Status, ExitCode: raw.State.ExitCode}, nil
}

// Logs는 컨테이너 출력의 마지막 n줄이다 (stdout·stderr 섞어서).
func (c *Client) Logs(ctx context.Context, name string, n int) (string, error) {
	res, err := c.stream(ctx, http.MethodGet, fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1&tail=%d", url.PathEscape(name), n), nil, "")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return demux(raw), nil
}

// demux는 TTY 없이 돈 컨테이너 로그의 8바이트 머리말을 걷어낸다.
func demux(raw []byte) string {
	var out bytes.Buffer
	for len(raw) >= 8 && (raw[0] == 1 || raw[0] == 2) && raw[1] == 0 && raw[2] == 0 && raw[3] == 0 {
		size := int(binary.BigEndian.Uint32(raw[4:8]))
		raw = raw[8:]
		if size > len(raw) {
			size = len(raw)
		}
		out.Write(raw[:size])
		raw = raw[size:]
	}
	out.Write(raw) // TTY로 돈 컨테이너는 머리말이 없다
	return out.String()
}

// ByLabel은 라벨이 key=value인 컨테이너 이름들이다.
func (c *Client) ByLabel(ctx context.Context, key, value string) ([]string, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {key + "=" + value}})
	var raw []struct{ Names []string }
	if err := c.call(ctx, http.MethodGet, "/containers/json?all=1&filters="+url.QueryEscape(string(filters)), nil, &raw); err != nil {
		return nil, err
	}
	var names []string
	for _, r := range raw {
		for _, n := range r.Names {
			names = append(names, strings.TrimPrefix(n, "/"))
		}
	}
	return names, nil
}
