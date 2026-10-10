package docker

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KangminNa/naru/internal/model"
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
		w.Write([]byte(`[{"Names":["/shelf-blog"],"Image":"shelf-app-blog","State":"running","Status":"Up 3 hours",
			"Ports":[{"PrivatePort":3000,"Type":"tcp"},{"IP":"0.0.0.0","PrivatePort":3000,"PublicPort":3893,"Type":"tcp"},{"PrivatePort":53,"Type":"udp"}]},
			{"Names":["/naru-api"],"Image":"ghcr.io/me/api","State":"restarting","Status":"Restarting (1) 5 seconds ago"}]`))
	}))
	got, err := New(sock).containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["shelf-blog"].State != Running || got["naru-api"].State != "restarting" || len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if p := got["shelf-blog"].Ports; len(p) != 1 || p[0] != 3000 {
		t.Fatalf("exposed TCP ports, once each: %v", p)
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

// 설치형에서는 compose가 네트워크를 만들어 주지 않는다 — 처음 띄울 때 없으면 만든다.
// 실제 dockerd(27)는 없는 네트워크로도 create를 받고 start에서야 실패한다 — 그래서 먼저 확인한다.
func TestStartCreatesTheNetworkAndReturnsTheIP(t *testing.T) {
	var calls []string
	networkExists := false
	sock := fakeDocker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/networks/naru-net":
			if !networkExists {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"network naru-net not found"}`))
				return
			}
			w.Write([]byte(`{"Name":"naru-net"}`))
		case r.URL.Path == "/networks/create":
			networkExists = true
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"Id":"net1"}`))
		case r.URL.Path == "/containers/create":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"Id":"abc"}`))
		case r.URL.Path == "/containers/naru-x-1/start":
			if !networkExists {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"network naru-net not found"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/containers/naru-x-1/json":
			w.Write([]byte(`{"State":{"Running":true,"Status":"running"},"NetworkSettings":{"Networks":{"naru-net":{"IPAddress":"172.18.0.5"},"other":{"IPAddress":"10.1.1.1"}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	c := NewContainers(New(sock), "naru-net")
	spec := model.ContainerSpec{Name: "naru-x-1", Image: "img", Network: "naru-net", Aliases: []string{"naru-x"}}
	ip, err := c.Start(context.Background(), spec)
	if err != nil || ip != "172.18.0.5" {
		t.Fatalf("ip=%q err=%v calls=%v", ip, err, calls)
	}
	if strings.Count(strings.Join(calls, " "), "/networks/create") != 1 {
		t.Fatalf("the network is created once: %v", calls)
	}
}

func TestStatesCarryTheIPOnTheAppNetwork(t *testing.T) {
	sock := fakeDocker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"Names":["/naru-x-1"],"State":"running","Status":"Up","NetworkSettings":{"Networks":{"bridge":{"IPAddress":"172.17.0.2"},"naru-net":{"IPAddress":"172.18.0.5"}}}}]`))
	}))
	all, err := NewContainers(New(sock), "naru-net").All(context.Background())
	if err != nil || all["naru-x-1"].IP != "172.18.0.5" {
		t.Fatalf("%+v %v", all, err)
	}
}

// mux는 Docker가 TTY 없는 컨테이너 로그를 보내는 모양이다 — 8바이트 머리(흐름·길이) + 내용.
func mux(stream byte, s string) []byte {
	h := []byte{stream, 0, 0, 0, byte(len(s) >> 24), byte(len(s) >> 16), byte(len(s) >> 8), byte(len(s))}
	return append(h, s...)
}

func TestRecentLogsKeepTimeAndStream(t *testing.T) {
	var query string
	sock := fakeDocker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		body := append(mux(1, "2026-10-10T12:00:01.500000000Z listening on :80\n"), mux(2, "2026-10-10T12:00:02.000000000Z warn: slow\n2026-10-10T12:00:03.000000000Z second line\n")...)
		w.Write(body)
	}))
	lines, err := NewContainers(New(sock), "naru-net").Recent(context.Background(), "naru-x-1", 50)
	if err != nil || len(lines) != 3 {
		t.Fatalf("%+v %v", lines, err)
	}
	if !strings.Contains(query, "timestamps=1") || !strings.Contains(query, "tail=50") {
		t.Fatalf("asks for timestamps and the tail: %s", query)
	}
	if lines[0].Text != "listening on :80" || lines[0].Stream != "stdout" || lines[0].At.Second() != 1 || lines[0].Source != model.LogApp {
		t.Fatalf("%+v", lines[0])
	}
	if lines[2].Stream != "stderr" || lines[2].Text != "second line" {
		t.Fatalf("%+v", lines[2])
	}
}
