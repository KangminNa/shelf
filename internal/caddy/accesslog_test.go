package caddy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testdata/access.recorded는 실제 Caddy(2.11)가 남긴 접근 로그 두 줄이다 — 관리 주소의 요청은 처음부터 없다.
func TestAccessLogReadsWhatCaddyWrote(t *testing.T) {
	lines, err := NewAccessLogFile("testdata/access.recorded").Recent(context.Background(), []string{"svc.localhost"}, 10)
	if err != nil || len(lines) != 2 {
		t.Fatalf("%+v %v", lines, err)
	}
	l := lines[0]
	if l.Method != "GET" || l.Path != "/hello?x=1" || l.Status != 200 || l.At.IsZero() || l.Took <= 0 {
		t.Fatalf("%+v", l)
	}
	if other, _ := NewAccessLogFile("testdata/access.recorded").Recent(context.Background(), []string{"blog.example.com"}, 10); len(other) != 0 {
		t.Fatal("only the asked hosts")
	}
}

func TestAccessLogKeepsTheNewestAndSkipsJunk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	var b strings.Builder
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 50; i++ {
		host := "a.example.com"
		if i%2 == 1 {
			host = "b.example.com:443" // 포트가 붙어 와도 주소로 맞춘다
		}
		fmt.Fprintf(&b, `{"logger":"http.log.access.access","ts":%d.5,"request":{"host":"%s","method":"POST","uri":"/n/%d"},"status":%d,"duration":0.002}`+"\n",
			start.Add(time.Duration(i)*time.Second).Unix(), host, i, 200+i%3)
		if i == 10 {
			b.WriteString("not json at all\n")
		}
	}
	os.WriteFile(path, []byte(b.String()), 0o644)
	lines, err := NewAccessLogFile(path).Recent(context.Background(), []string{"b.example.com"}, 5)
	if err != nil || len(lines) != 5 {
		t.Fatalf("%d %v", len(lines), err)
	}
	if lines[0].Path != "/n/41" || lines[4].Path != "/n/49" {
		t.Fatalf("the newest five, oldest first: %s … %s", lines[0].Path, lines[4].Path)
	}
	if none, err := NewAccessLogFile(filepath.Join(dir, "nope.log")).Recent(context.Background(), []string{"a"}, 5); err != nil || len(none) != 0 {
		t.Fatal("no log yet is an empty list, not an error")
	}
}
