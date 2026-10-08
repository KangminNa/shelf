// Package store는 Naru의 상태를 담는 SQLite 파일 하나를 연다.
// 마이그레이션은 바이너리에 들어 있고, 열 때마다 아직 적용하지 않은 것만 순서대로 적용한다.
package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// 설정 키. 값은 모두 문자열로 저장한다.
const (
	KeyAdminDomain = "admin_domain" // 관리 화면 주소
	KeyACMEEmail   = "acme_email"   // HTTPS 인증서 연락처
	KeySetupDone   = "setup_done"   // 첫 실행 마법사를 끝냈는가
	KeyV1Imported  = "v1_imported"  // v1 데이터를 옮겼는가 (한 번만)
)

type Store struct {
	DB *sql.DB
}

// Open은 path의 DB를 열고 마이그레이션을 적용한다.
func Open(path string) (*Store, error) {
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
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// OpenReadOnly는 다른 프로그램(v1)의 DB를 건드리지 않고 읽기만 한다.
func OpenReadOnly(path string) (*sql.DB, error) {
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

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
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
		if err := s.DB.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = ?`, name).Scan(&done); err != nil {
			return err
		}
		if done > 0 {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.DB.Begin()
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

// Setting은 설정 값 하나를 읽는다. 없으면 ok가 false다.
func (s *Store) Setting(key string) (value string, ok bool) {
	err := s.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return "", false
	}
	return value, true
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) DeleteSetting(key string) error {
	_, err := s.DB.Exec(`DELETE FROM settings WHERE key = ?`, key)
	return err
}
