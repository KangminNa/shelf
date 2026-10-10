package docker

import (
	"context"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// 컨테이너에 붙이는 라벨 — 어느 서비스의 몇 번째 배포인지
const (
	labelService    = "naru.service"
	labelDeployment = "naru.deployment"
	stopGrace       = 10 * time.Second
)

// Builder는 빌드할 폴더(묶은 것)로 이미지를 만든다.
type Builder struct{ c *Client }

func NewBuilder(c *Client) Builder { return Builder{c} }

func (b Builder) Build(ctx context.Context, folder io.Reader, tag string, log io.Writer) (model.ImageID, error) {
	id, err := b.c.build(ctx, folder, tag, "", log)
	return model.ImageID(id), err
}

// Puller는 이미지를 받고 그 ID·열어둔 포트를 알려준다.
type Puller struct{ c *Client }

func NewPuller(c *Client) Puller { return Puller{c} }

func (p Puller) Pull(ctx context.Context, ref model.ImageRef, log io.Writer) (model.ImageDetails, error) {
	if err := p.c.pull(ctx, ref.String(), log); err != nil {
		return model.ImageDetails{}, err
	}
	img, err := p.c.image(ctx, ref.String())
	if err != nil {
		return model.ImageDetails{}, err
	}
	d := model.ImageDetails{ID: model.ImageID(img.ID)}
	for _, port := range img.Ports {
		d.Ports = append(d.Ports, model.Port(port))
	}
	return d, nil
}

// Images는 이미지가 있는지 보고, 지운다.
type Images struct{ c *Client }

func NewImages(c *Client) Images { return Images{c} }

func (i Images) Exists(ctx context.Context, id model.ImageID) bool {
	_, err := i.c.image(ctx, string(id))
	return err == nil
}

func (i Images) Remove(ctx context.Context, ref string) error { return i.c.removeImage(ctx, ref) }

// Containers는 컨테이너를 띄우고·없애고·멈추고·켜고, 상태를 본다. network는 앱 컨테이너가 붙는 네트워크다.
type Containers struct {
	c       *Client
	network string
}

func NewContainers(c *Client, network string) Containers { return Containers{c, network} }

// Start는 만들고 띄운 뒤 앱 네트워크에서의 IP를 돌려준다. 네트워크가 없으면 만든다.
// 띄우지 못하면 만든 것을 지운다.
func (k Containers) Start(ctx context.Context, s model.ContainerSpec) (string, error) {
	spec := Spec{
		Name: s.Name, Image: string(s.Image), Env: s.Env, Binds: s.Binds, Network: s.Network, Aliases: s.Aliases,
		Labels: map[string]string{
			labelService:    strconv.FormatInt(int64(s.Service), 10),
			labelDeployment: strconv.FormatInt(int64(s.Deploy), 10),
		},
	}
	// 네트워크가 없으면 먼저 만든다. Docker는 없는 네트워크로도 create를 받아 두고 start에서야 실패한다 (dockerd 27에서 확인).
	if s.Network != "" {
		if err := k.c.ensureNetwork(ctx, s.Network); err != nil {
			return "", err
		}
	}
	if _, err := k.c.create(ctx, spec); err != nil {
		return "", err
	}
	if err := k.c.start(ctx, s.Name); err != nil {
		k.c.remove(context.Background(), s.Name)
		return "", err
	}
	in, err := k.c.inspect(ctx, s.Name)
	if err != nil {
		return "", err
	}
	return in.IPs[s.Network], nil
}

// Remove는 없앤다. 이미 없으면 괜찮다.
func (k Containers) Remove(ctx context.Context, name string) error {
	if err := k.c.remove(ctx, name); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

func (k Containers) TurnOff(ctx context.Context, name string) error {
	return k.c.stop(ctx, name, stopGrace)
}
func (k Containers) TurnOn(ctx context.Context, name string) error { return k.c.start(ctx, name) }

// All은 모든 컨테이너 상태를 한 번에 읽는다.
func (k Containers) All(ctx context.Context) (model.ContainerStates, error) {
	raw, err := k.c.containers(ctx)
	if err != nil {
		return nil, err
	}
	out := make(model.ContainerStates, len(raw))
	for name, c := range raw {
		var ports []model.Port
		for _, p := range c.Ports {
			ports = append(ports, model.Port(p))
		}
		out[name] = model.ContainerState{Name: name, Running: c.State == Running, State: string(c.State), Status: c.Status, IP: c.IPs[k.network], Ports: ports}
	}
	return out, nil
}

func (k Containers) One(ctx context.Context, name string) (model.ContainerState, error) {
	in, err := k.c.inspect(ctx, name)
	if err != nil {
		return model.ContainerState{}, err
	}
	return model.ContainerState{Name: name, Running: in.Running, State: in.Status, ExitCode: in.ExitCode, IP: in.IPs[k.network]}, nil
}

func (k Containers) Logs(ctx context.Context, name string, lines int) (string, error) {
	return k.c.logs(ctx, name, lines)
}

// Recent는 컨테이너가 찍은 최근 n줄이다 (ContainerLogReader).
func (k Containers) Recent(ctx context.Context, name string, n int) ([]model.LogLine, error) {
	return k.c.logLines(ctx, name, n)
}

func (k Containers) BelongingTo(ctx context.Context, id model.ServiceID) ([]string, error) {
	return k.c.byLabel(ctx, labelService, strconv.FormatInt(int64(id), 10))
}
