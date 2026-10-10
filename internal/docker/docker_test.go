package docker

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDocker는 유닉스 소켓에서 Docker인 척한다.
func fakeDocker(t *testing.T, h http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock") // 유닉스 소켓 경로는 짧아야 한다
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock
}

func TestContainers(t *testing.T) {
	sock := fakeDocker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" || r.URL.Query().Get("all") != "1" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`[{"Names":["/shelf-blog"],"Image":"shelf-app-blog","State":"running","Status":"Up 3 hours"},
			{"Names":["/naru-api"],"Image":"ghcr.io/me/api","State":"restarting","Status":"Restarting (1) 5 seconds ago"}]`))
	}))
	got, err := New(sock).containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["shelf-blog"].State != Running || got["naru-api"].State != "restarting" || len(got) != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestErrorsCarryTheReason(t *testing.T) {
	sock := fakeDocker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"client version too old"}`, http.StatusBadRequest)
	}))
	if _, err := New(sock).containers(context.Background()); err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("got %v", err)
	}
}

func TestUnreachableDocker(t *testing.T) {
	if _, err := New("/nonexistent/docker.sock").containers(context.Background()); err == nil {
		t.Fatal("a missing socket is an error")
	}
}

func TestSplitRef(t *testing.T) {
	cases := map[string][2]string{
		"traefik/whoami":       {"traefik/whoami", "latest"},
		"ghcr.io/me/api:v1":    {"ghcr.io/me/api", "v1"},
		"localhost:5000/app":   {"localhost:5000/app", "latest"},
		"localhost:5000/app:2": {"localhost:5000/app", "2"},
		"nginx@sha256:abc":     {"nginx@sha256:abc", ""},
	}
	for in, want := range cases {
		if n, tag := splitRef(in); n != want[0] || tag != want[1] {
			t.Errorf("%s → %s %s", in, n, tag)
		}
	}
}

func TestFollowReportsErrorsAndImageID(t *testing.T) {
	var log strings.Builder
	id, err := follow(strings.NewReader(`{"stream":"Step 1/2 : FROM alpine\n"}{"aux":{"ID":"sha256:abc"}}{"stream":"Successfully built\n"}`), &log)
	if err != nil || id != "sha256:abc" || !strings.Contains(log.String(), "Step 1/2") {
		t.Fatalf("%q %v %q", id, err, log.String())
	}
	if _, err := follow(strings.NewReader(`{"stream":"RUN false\n"}{"error":"The command '/bin/sh -c false' returned a non-zero code: 1"}`), &log); err == nil || !strings.Contains(err.Error(), "non-zero") {
		t.Fatalf("build errors surface: %v", err)
	}
}

func TestDemuxStripsFrameHeaders(t *testing.T) {
	frame := func(stream byte, s string) []byte {
		h := []byte{stream, 0, 0, 0, 0, 0, 0, byte(len(s))}
		return append(h, s...)
	}
	raw := append(frame(1, "listening on :3000\n"), frame(2, "warn: x\n")...)
	if got := demux(raw); got != "listening on :3000\nwarn: x\n" {
		t.Fatalf("%q", got)
	}
	if got := demux([]byte("tty output\n")); got != "tty output\n" {
		t.Fatalf("tty logs pass through: %q", got)
	}
}
