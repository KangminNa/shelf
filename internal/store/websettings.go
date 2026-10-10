package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/KangminNa/naru/internal/model"
)

// WebSettings는 서비스마다 웹서버 설정을 저장한다 (WebSettingsStore). 없으면 빈 설정이다.
type WebSettings struct{ db *DB }

func NewWebSettings(db *DB) WebSettings { return WebSettings{db} }

func (w WebSettings) Get(ctx context.Context, id model.ServiceID) (model.WebSettings, error) {
	var raw string
	err := w.db.sql.QueryRowContext(ctx, `SELECT data FROM web_settings WHERE service_id = ?`, int64(id)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WebSettings{}, nil
	}
	if err != nil {
		return model.WebSettings{}, err
	}
	var s model.WebSettings
	err = json.Unmarshal([]byte(raw), &s) // 읽을 때 값 모양을 다시 확인한다
	return s, err
}

func (w WebSettings) Set(ctx context.Context, id model.ServiceID, s model.WebSettings) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = w.db.sql.ExecContext(ctx, `INSERT INTO web_settings (service_id, data) VALUES (?, ?)
		ON CONFLICT(service_id) DO UPDATE SET data = excluded.data`, int64(id), string(raw))
	return err
}
