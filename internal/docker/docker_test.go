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
	got, err := New(sock).Containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["shelf-blog"].State != Running || got["naru-api"].State != Restarting || len(got) != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestErrorsCarryTheReason(t *testing.T) {
	sock := fakeDocker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"client version too old"}`, http.StatusBadRequest)
	}))
	if _, err := New(sock).Containers(context.Background()); err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("got %v", err)
	}
}

func TestUnreachableDocker(t *testing.T) {
	if err := New("/nonexistent/docker.sock").Ping(context.Background()); err == nil {
		t.Fatal("a missing socket is an error")
	}
}
