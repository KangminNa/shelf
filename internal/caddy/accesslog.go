package caddy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net"
	"os"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// accessTail만큼 파일 끝을 읽는다 — 로그 화면은 최근 것만 보여준다.
const accessTail = 4 << 20

// AccessLogFile은 Caddy가 남긴 접근 로그(JSON 줄)에서 주소별 최근 요청을 읽는다 (AccessLogReader).
// 어떤 요청을 남길지(관리 주소는 빼기)는 설정을 쓸 때 정한다 — 여기서는 읽기만 한다.
type AccessLogFile struct{ path string }

func NewAccessLogFile(path string) AccessLogFile { return AccessLogFile{path: path} }

func (f AccessLogFile) Recent(ctx context.Context, hosts []string, n int) ([]model.LogLine, error) {
	file, err := os.Open(f.path)
	if os.IsNotExist(err) {
		return nil, nil // 아직 요청을 받은 적이 없다
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > accessTail {
		file.Seek(info.Size()-accessTail, io.SeekStart)
	}
	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, h := range hosts {
		want[strings.ToLower(h)] = true
	}
	var out []model.LogLine
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var e struct {
			TS       float64 `json:"ts"`
			Status   int     `json:"status"`
			Duration float64 `json:"duration"`
			Request  struct {
				Host   string `json:"host"`
				Method string `json:"method"`
				URI    string `json:"uri"`
			} `json:"request"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.TS == 0 {
			continue // 앞을 잘라 읽은 첫 줄이나 깨진 줄
		}
		host := strings.ToLower(e.Request.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !want[host] {
			continue
		}
		sec, frac := math.Modf(e.TS)
		out = append(out, model.LogLine{
			At: time.Unix(int64(sec), int64(frac*1e9)), Source: model.LogRequest,
			Method: e.Request.Method, Path: e.Request.URI, Status: e.Status, Took: time.Duration(e.Duration * float64(time.Second)),
		})
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}
