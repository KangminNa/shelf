package httppost

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

var alert = model.Alert{Event: "service.down", Level: model.AlertProblem, Title: "서비스가 멈췄어요: blog / Service down: blog", Detail: "blog: crashed", Service: "blog"}

func TestFormatsByAddress(t *testing.T) {
	cases := map[string]string{
		"https://discord.com/api/webhooks/1/x":    "content",
		"https://discordapp.com/api/webhooks/1/x": "content",
		"https://hooks.slack.com/services/T/B/x":  "text",
		"https://ops.example.com/naru":            "event",
	}
	for u, field := range cases {
		body, _ := payload(model.AlertChannel{URL: u}, alert, time.Unix(1790000000, 0))
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil || m[field] == nil {
			t.Errorf("%s: want %q in %s", u, field, body)
		}
		if field != "event" && !strings.Contains(m[field].(string), "blog") {
			t.Errorf("%s: the message says what happened: %s", u, body)
		}
	}
}

func TestGenericJSONIsSignedLikeV1(t *testing.T) {
	var got http.Header
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, body = r.Header, mustRead(r)
	}))
	defer srv.Close()
	err := AlertPoster{}.Send(context.Background(), model.AlertChannel{URL: srv.URL + "/hook", Secret: "s3cret"}, alert)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(body)
	if got.Get("X-Naru-Signature-256") != "sha256="+hex.EncodeToString(mac.Sum(nil)) || got.Get("Content-Type") != "application/json" {
		t.Fatalf("signed with HMAC-SHA256 over the body: %v", got)
	}
	var m map[string]any
	json.Unmarshal(body, &m)
	for _, k := range []string{"event", "level", "title", "detail", "service", "sent_at"} {
		if _, ok := m[k]; !ok {
			t.Errorf("v1 field %q is kept", k)
		}
	}
}

func TestFailuresSayWhy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	if err := (AlertPoster{}).Send(context.Background(), model.AlertChannel{URL: srv.URL}, alert); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("got %v", err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer slow.Close()
	start := time.Now()
	if err := (AlertPoster{Timeout: 50 * time.Millisecond}).Send(context.Background(), model.AlertChannel{URL: slow.URL}, alert); err == nil || time.Since(start) > 250*time.Millisecond {
		t.Fatalf("a slow receiver is cut off: %v", err)
	}
}

func mustRead(r *http.Request) []byte { b, _ := io.ReadAll(r.Body); return b }
