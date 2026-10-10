package caddy

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// adaptServer는 Caddy 관리 API의 /adapt인 척한다. 응답은 실제 Caddy(2.11)에서 녹음한 것이다.
func adaptServer(t *testing.T, status int, body []byte) (socket string, got *string) {
	t.Helper()
	dir, _ := os.MkdirTemp("", "ad")
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket = filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var seen string
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = r.Method + " " + r.URL.Path + " " + r.Header.Get("Content-Type") + " " + r.Host + "\n" + string(b)
		w.WriteHeader(status)
		w.Write(body)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return socket, &seen
}

func TestCompileKeepsOnlyTheSiteRoutes(t *testing.T) {
	recorded, err := os.ReadFile("testdata/adapt-result.recorded")
	if err != nil {
		t.Fatal(err)
	}
	socket, seen := adaptServer(t, 200, recorded)
	compiled, ignored, err := NewAdaptCompiler(socket).Compile(context.Background(), "respond /health 200\nheader X-Test yes\ntls internal\n}\nother.invalid {\nrespond hi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(*seen, "POST /adapt text/caddyfile 127.0.0.1\n") || !strings.Contains(*seen, snippetHost+" {\n") {
		t.Fatalf("the snippet is wrapped in a site block and sent to /adapt: %q", *seen)
	}
	var routes []map[string]any
	if err := json.Unmarshal(compiled, &routes); err != nil || len(routes) != 2 {
		t.Fatalf("only the routes inside our site: %s %v", compiled, err)
	}
	if !strings.Contains(string(compiled), "X-Test") || strings.Contains(string(compiled), `"hi"`) {
		t.Fatalf("another site's routes never leak in: %s", compiled)
	}
	joined := strings.Join(ignored, " | ")
	if !strings.Contains(joined, "tls") || !strings.Contains(joined, "other.invalid") {
		t.Fatalf("what was dropped is reported: %v", ignored)
	}
}

func TestCompileErrorCarriesCaddysWords(t *testing.T) {
	socket, _ := adaptServer(t, 400, []byte(`{"error":"adapting config using caddyfile adapter: Caddyfile:2: unrecognized directive: nonsense"}`))
	_, _, err := NewAdaptCompiler(socket).Compile(context.Background(), "nonsense")
	if err == nil || !strings.Contains(err.Error(), "unrecognized directive: nonsense") {
		t.Fatalf("got %v", err)
	}
}

func TestCompileWithoutCaddy(t *testing.T) {
	if _, _, err := NewAdaptCompiler("/nope/admin.sock").Compile(context.Background(), "respond hi"); err == nil {
		t.Fatal("no web server, no compile")
	}
}

// 실제 Caddy는 감싼 지시어마다 "input is not formatted" 경고를 붙인다 (검증 스택에서 확인) — 사용자에게 보이지 않는다.
func TestFormattingWarningsAreNotShown(t *testing.T) {
	body := `{"result":{"apps":{"http":{"servers":{"srv0":{"listen":[":443"],"routes":[{"match":[{"host":["naru.invalid"]}],` +
		`"handle":[{"handler":"subroute","routes":[{"handle":[{"handler":"static_response","status_code":200}]}]}],"terminal":true}]}}}}},` +
		`"warnings":[{"file":"Caddyfile","line":2,"message":"Caddyfile input is not formatted; run 'caddy fmt --overwrite' to fix inconsistencies"},` +
		`{"file":"Caddyfile","line":3,"message":"some other warning"}]}`
	socket, _ := adaptServer(t, 200, []byte(body))
	_, ignored, err := NewAdaptCompiler(socket).Compile(context.Background(), "respond 200")
	if err != nil || len(ignored) != 1 || ignored[0] != "some other warning" {
		t.Fatalf("%v %v", ignored, err)
	}
}
