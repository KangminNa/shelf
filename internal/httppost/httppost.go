// Package httppost는 알림을 HTTP POST로 보낸다 (AlertSender).
// 형식은 주소로 고른다 — Discord·Slack은 그쪽이 받는 모양으로, 그 밖은 v1과 같은 JSON에 HMAC 서명을 붙여서.
package httppost

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// AlertPoster는 알림 하나를 보낸다. Timeout이 0이면 10초.
type AlertPoster struct{ Timeout time.Duration }

func (p AlertPoster) Send(ctx context.Context, ch model.AlertChannel, a model.Alert) error {
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, format := payload(ch, a, time.Now())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "naru-notify/2")
	if format == "json" && ch.Secret != "" {
		mac := hmac.New(sha256.New, []byte(ch.Secret))
		mac.Write(body)
		req.Header.Set("X-Naru-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", model.AlertHostOf(ch.URL), unwrapURL(err))
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s answered %s", model.AlertHostOf(ch.URL), res.Status)
	}
	return nil
}

func payload(ch model.AlertChannel, a model.Alert, now time.Time) ([]byte, string) {
	format := model.AlertFormatOf(ch.URL)
	var v any
	switch format {
	case "discord":
		v = map[string]string{"content": fmt.Sprintf("%s **%s**\n%s", mark(a.Level), a.Title, a.Detail)}
	case "slack":
		v = map[string]string{"text": fmt.Sprintf("%s *%s*\n%s", mark(a.Level), a.Title, a.Detail)}
	default: // v1과 같은 모양 — v1 알림을 받던 곳이 그대로 받는다
		v = map[string]any{"event": a.Event, "level": a.Level, "title": a.Title, "detail": a.Detail, "service": nullable(a.Service), "sent_at": now.Unix()}
	}
	b, _ := json.Marshal(v)
	return b, format
}

func mark(l model.AlertLevel) string {
	switch l {
	case model.AlertProblem:
		return "🔴"
	case model.AlertRecovery:
		return "🟢"
	}
	return "🔵"
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// unwrapURL은 net/http가 오류에 붙이는 주소 전체를 떼어 낸다 (주소가 비밀이다).
func unwrapURL(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}
