package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// Accounts는 계정을 저장하고 읽는다 (AccountReader · AccountStore).
type Accounts struct{ db *DB }

func NewAccounts(db *DB) Accounts { return Accounts{db} }

func (a Accounts) Count(ctx context.Context) (int, error) {
	var n int
	err := a.db.sql.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (a Accounts) FindByName(ctx context.Context, user string) (model.Account, model.PasswordHash, error) {
	var id int64
	var name, hash string
	err := a.db.sql.QueryRowContext(ctx, `SELECT id, username, password_hash FROM users WHERE username = ?`, user).Scan(&id, &name, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Account{}, "", model.ErrNotFound
	}
	if err != nil {
		return model.Account{}, "", err
	}
	u, _ := model.ParseUsername(name)
	return model.Account{ID: model.AccountID(id), Username: u}, model.PasswordHash(hash), nil
}

func (a Accounts) Names(ctx context.Context) ([]string, error) {
	rows, err := a.db.sql.QueryContext(ctx, `SELECT username FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// CreateFirst는 계정이 하나도 없을 때만 만든다. 동시에 두 요청이 와도 하나만 성공한다.
func (a Accounts) CreateFirst(ctx context.Context, u model.Username, h model.PasswordHash) (model.AccountID, error) {
	res, err := a.db.sql.ExecContext(ctx, `INSERT INTO users (username, password_hash)
		SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`, u.String(), string(h))
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, model.ErrSetupClosed
	}
	id, _ := res.LastInsertId()
	return model.AccountID(id), nil
}

func (a Accounts) SetPassword(ctx context.Context, id model.AccountID, h model.PasswordHash) error {
	res, err := a.db.sql.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(h), int64(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (a Accounts) DeleteAll(ctx context.Context) error {
	_, err := a.db.sql.ExecContext(ctx, `DELETE FROM users`)
	return err
}

// Sessions는 로그인 세션을 저장한다 (SessionStore). 토큰 원문은 저장하지 않는다.
type Sessions struct{ db *DB }

func NewSessions(db *DB) Sessions { return Sessions{db} }

func (s Sessions) Save(ctx context.Context, d model.SessionDigest, who model.AccountID, until time.Time) error {
	_, err := s.db.sql.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, string(d), int64(who), until.Unix())
	return err
}

// FindOwner는 만료되지 않았고 계정이 살아 있는 세션의 주인이다.
func (s Sessions) FindOwner(ctx context.Context, d model.SessionDigest, now time.Time) (model.Account, bool) {
	var id int64
	var name string
	err := s.db.sql.QueryRowContext(ctx, `SELECT u.id, u.username FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, string(d), now.Unix()).Scan(&id, &name)
	if err != nil {
		return model.Account{}, false
	}
	u, _ := model.ParseUsername(name)
	return model.Account{ID: model.AccountID(id), Username: u}, true
}

func (s Sessions) Delete(ctx context.Context, d model.SessionDigest) error {
	_, err := s.db.sql.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, string(d))
	return err
}

// DeleteOthers는 who의 세션 중 keep만 남긴다 (keep이 비면 모두 지운다).
func (s Sessions) DeleteOthers(ctx context.Context, who model.AccountID, keep model.SessionDigest) error {
	_, err := s.db.sql.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, int64(who), string(keep))
	return err
}
