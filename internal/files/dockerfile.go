package files

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// DockerfileReader는 Dockerfile의 EXPOSE에서 TCP 포트를 읽는다.
type DockerfileReader struct{}

func (DockerfileReader) Ports(folder string) ([]model.Port, bool) {
	f, err := os.Open(filepath.Join(folder, "Dockerfile"))
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var ports []model.Port
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || !strings.EqualFold(fields[0], "EXPOSE") {
			continue
		}
		for _, field := range fields[1:] {
			num, proto, _ := strings.Cut(field, "/")
			if proto != "" && !strings.EqualFold(proto, "tcp") {
				continue
			}
			if n, err := strconv.Atoi(num); err == nil && n > 0 && n < 65536 {
				ports = append(ports, model.Port(n))
			}
		}
	}
	return ports, true
}
