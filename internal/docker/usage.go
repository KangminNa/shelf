package docker

import (
	"context"
	"net/http"
	"net/url"

	"github.com/KangminNa/naru/internal/model"
)

type cpuStats struct {
	CPUUsage struct {
		TotalUsage uint64 `json:"total_usage"`
	} `json:"cpu_usage"`
	SystemUsage uint64 `json:"system_cpu_usage"`
}

// usage는 컨테이너 하나의 CPU·메모리다. stream=false면 Docker가 두 번 재어 차이를 준다 (1초쯤 걸린다).
// CPU는 서버 전체를 100%로 본다. 메모리는 다시 읽을 수 있는 파일 캐시를 뺀다 (docker stats와 같은 셈).
func (c *Client) usage(ctx context.Context, name string) (model.ResourceUsage, error) {
	var raw struct {
		CPU    cpuStats `json:"cpu_stats"`
		PreCPU cpuStats `json:"precpu_stats"`
		Memory struct {
			Usage uint64            `json:"usage"`
			Limit uint64            `json:"limit"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if err := c.call(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/stats?stream=false", nil, &raw); err != nil {
		return model.ResourceUsage{}, err
	}
	var u model.ResourceUsage
	cpu := float64(raw.CPU.CPUUsage.TotalUsage) - float64(raw.PreCPU.CPUUsage.TotalUsage)
	system := float64(raw.CPU.SystemUsage) - float64(raw.PreCPU.SystemUsage)
	if cpu > 0 && system > 0 {
		u.CPUPercent = cpu / system * 100
	}
	cache := raw.Memory.Stats["inactive_file"]                // cgroup v2
	if v, ok := raw.Memory.Stats["total_inactive_file"]; ok { // cgroup v1
		cache = v
	}
	u.MemUsed, u.MemLimit = raw.Memory.Usage, raw.Memory.Limit
	if cache < u.MemUsed {
		u.MemUsed -= cache
	}
	return u, nil
}
