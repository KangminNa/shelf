// Package v1import는 v1(Shelf)이 남긴 데이터를 v2로 옮긴다.
// v1 DB는 읽기 전용으로만 연다 — 문제가 생기면 v1 이미지를 다시 띄워 그대로 돌아갈 수 있어야 한다.
package v1import

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"github.com/KangminNa/naru/internal/auth"
	"github.com/KangminNa/naru/internal/store"
)

// v1은 ADMIN_DOMAIN을 이 설명이 붙은 프록시 호스트로 등록했다.
const adminHostMarker = "%auto-registered via ADMIN_DOMAIN%"

type Report struct {
	Skipped     bool   // 이미 옮겼다
	Found       bool   // v1 데이터가 있었다
	Users       int    // 옮긴 계정 수
	AdminDomain string // 옮긴 관리 도메인
}

// Run은 처음 한 번만 옮긴다. v1 데이터가 없으면 아무것도 하지 않고 다시 보지 않는다.
func Run(dataDir string, st *store.Store, accounts *auth.Service) (Report, error) {
	if _, done := st.Setting(store.KeyV1Imported); done {
		return Report{Skipped: true}, nil
	}
	var r Report

	if db, ok, err := open(filepath.Join(dataDir, "auth.db")); err != nil {
		return r, err
	} else if ok {
		r.Found = true
		n, err := importUsers(db, accounts)
		db.Close()
		if err != nil {
			return r, err
		}
		r.Users = n
	}

	if db, ok, err := open(filepath.Join(dataDir, "proxy.db")); err != nil {
		return r, err
	} else if ok {
		r.Found = true
		var domain string
		err := db.QueryRow(`SELECT domain FROM proxy_hosts WHERE description LIKE ? ORDER BY id LIMIT 1`, adminHostMarker).Scan(&domain)
		db.Close()
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return r, err
		}
		if _, set := st.Setting(store.KeyAdminDomain); domain != "" && !set {
			if err := st.SetSetting(store.KeyAdminDomain, domain); err != nil {
				return r, err
			}
			r.AdminDomain = domain
		}
	}

	// v1을 쓰던 서버는 이미 설정을 마친 서버다 — 마법사를 다시 띄우지 않는다.
	if r.Users > 0 {
		if err := st.SetSetting(store.KeySetupDone, "1"); err != nil {
			return r, err
		}
	}
	return r, st.SetSetting(store.KeyV1Imported, "1")
}

func open(path string) (*sql.DB, bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	db, err := store.OpenReadOnly(path)
	if err != nil {
		return nil, false, err
	}
	return db, true, nil
}

func importUsers(db *sql.DB, accounts *auth.Service) (int, error) {
	rows, err := db.Query(`SELECT username, password_hash FROM users ORDER BY id`)
	if err != nil {
		return 0, err
	}
	type row struct{ name, hash string }
	var all []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.name, &x.hash); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	added := 0
	for _, x := range all {
		ok, err := accounts.ImportUser(x.name, x.hash)
		if err != nil {
			return added, err
		}
		if ok {
			added++
		}
	}
	return added, nil
}
