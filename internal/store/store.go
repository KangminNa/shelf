// Package store는 Naru의 상태를 SQLite 파일 하나에 둔다. SQL은 이 패키지에만 있다.
// 마이그레이션은 바이너리에 들어 있고, 열 때마다 아직 적용하지 않은 것만 순서대로 적용한다.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DB는 열린 Naru DB다. 저장 객체들은 이것을 나눠 쓴다.
type DB struct{ sql *sql.DB }

// Open은 path의 DB를 열고 마이그레이션을 적용한다.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	// 쓰기가 많지 않은 관리 도구라 연결 하나로 직렬화한다 — SQLITE_BUSY를 원천 차단.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	d := &DB{sql: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// openReadOnly는 다른 프로그램(v1)의 DB를 건드리지 않고 읽기만 한다.
func openReadOnly(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	if readOnly {
		q.Add("mode", "ro")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "foreign_keys(1)")
	}
	return "file:" + path + "?" + q.Encode()
}

func (d *DB) migrate() error {
	if _, err := d.sql.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var done int
		if err := d.sql.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = ?`, name).Scan(&done); err != nil {
			return err
		}
		if done > 0 {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := d.sql.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ── 서버 설정 ─────────────────────────────

// Settings는 서버 설정 값을 저장한다 (SettingStore).
type Settings struct{ db *DB }

func NewSettings(db *DB) Settings { return Settings{db} }

func (s Settings) Get(ctx context.Context, key string) (string, bool) {
	var v string
	if err := s.db.sql.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func (s Settings) Set(ctx context.Context, key, value string) error {
	_, err := s.db.sql.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s Settings) Delete(ctx context.Context, key string) error {
	_, err := s.db.sql.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}
