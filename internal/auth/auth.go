// Package auth는 관리자 계정과 세션을 다룬다.
// 관리자는 Docker 소켓을 쥐므로 서버의 root와 같다 — 그래서 계정은 처음 한 번,
// 서버 로그를 볼 수 있는 사람(설정 토큰을 가진 사람)만 만들 수 있다.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"sync"
	"time"
)

const (
	SessionTTL       = 7 * 24 * time.Hour
	MaxLoginFailures = 5
	LockoutDuration  = 15 * time.Minute
	MinPasswordLen   = 8
)

var (
	ErrSetupClosed     = errors.New("setup is already complete")
	ErrBadSetupToken   = errors.New("setup token is missing or wrong")
	ErrInvalidUsername = errors.New("invalid username")
	ErrWeakPassword    = errors.New("password too short")
	ErrBadCredentials  = errors.New("invalid username or password")
	ErrLocked          = errors.New("too many failed attempts")
	ErrNoSuchUser      = errors.New("no such user")
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{2,32}$`)

type User struct {
	ID       int64
	Username string
}

type failure struct {
	count       int
	lockedUntil time.Time
}

type Service struct {
	db  *sql.DB
	now func() time.Time

	mu         sync.Mutex
	setupToken string
	failures   map[string]*failure
}

func New(db *sql.DB) (*Service, error) {
	s := &Service{db: db, now: time.Now, failures: map[string]*failure{}}
	token, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	s.setupToken = token
	return s, nil
}

// NeedsSetup은 아직 계정이 하나도 없는가.
func (s *Service) NeedsSetup() bool {
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil {
		return false // 읽지 못하면 열어주지 않는다
	}
	return n == 0
}

// SetupToken은 이번 실행에서만 쓰는 토큰이다. 서버 로그에만 찍힌다.
func (s *Service) SetupToken() string { return s.setupToken }

// CheckSetupToken은 계정이 없을 때만, 토큰이 맞을 때만 참이다.
func (s *Service) CheckSetupToken(token string) bool {
	if token == "" || !s.NeedsSetup() {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.setupToken)) == 1
}

// CreateFirstAccount는 첫 관리자를 만든다. 동시에 두 요청이 와도 하나만 성공한다.
func (s *Service) CreateFirstAccount(token, username, password string) (User, error) {
	if !s.NeedsSetup() {
		return User{}, ErrSetupClosed
	}
	if !s.CheckSetupToken(token) {
		return User{}, ErrBadSetupToken
	}
	if err := validate(username, password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash)
		SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`, username, hash)
	if err != nil {
		return User{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return User{}, ErrSetupClosed
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Username: username}, nil
}

// ImportUser는 v1에서 옮겨온 계정을 해시 그대로 넣는다. 이미 있으면 건너뛴다.
func (s *Service) ImportUser(username, passwordHash string) (bool, error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO users (username, password_hash) VALUES (?, ?)`, username, passwordHash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Login은 비밀번호를 확인한다. 같은 IP에서 연속으로 틀리면 잠근다.
func (s *Service) Login(ip, username, password string) (User, error) {
	if wait := s.lockedFor(ip); wait > 0 {
		return User{}, ErrLocked
	}
	var u User
	var hash string
	err := s.db.QueryRow(`SELECT id, username, password_hash FROM users WHERE username = ?`, username).Scan(&u.ID, &u.Username, &hash)
	if err != nil || !VerifyPassword(password, hash) {
		s.recordFailure(ip)
		return User{}, ErrBadCredentials
	}
	s.mu.Lock()
	delete(s.failures, ip)
	s.mu.Unlock()
	return u, nil
}

// LockedFor는 그 IP가 얼마나 더 기다려야 하는가. 0이면 잠기지 않았다.
func (s *Service) LockedFor(ip string) time.Duration { return s.lockedFor(ip) }

func (s *Service) lockedFor(ip string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[ip]
	if f == nil {
		return 0
	}
	if wait := f.lockedUntil.Sub(s.now()); wait > 0 {
		return wait
	}
	return 0
}

func (s *Service) recordFailure(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[ip]
	if f == nil {
		f = &failure{}
		s.failures[ip] = f
	}
	f.count++
	if f.count >= MaxLoginFailures {
		f.lockedUntil = s.now().Add(LockoutDuration)
		f.count = 0
	}
}

// NewSession은 쿠키에 넣을 토큰을 돌려준다. DB에는 해시만 남는다.
func (s *Service) NewSession(userID int64) (string, error) {
	token, err := randomHex(32)
	if err != nil {
		return "", err
	}
	expires := s.now().Add(SessionTTL).Unix()
	if _, err := s.db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, hashToken(token), userID, expires); err != nil {
		return "", err
	}
	return token, nil
}

// UserFor는 세션 토큰의 주인을 찾는다. 만료됐거나 계정이 사라졌으면 없다.
func (s *Service) UserFor(token string) (User, bool) {
	if token == "" {
		return User{}, false
	}
	var u User
	err := s.db.QueryRow(`SELECT u.id, u.username FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, hashToken(token), s.now().Unix()).Scan(&u.ID, &u.Username)
	if err != nil {
		return User{}, false
	}
	return u, true
}

func (s *Service) EndSession(token string) {
	s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
}

// ChangePassword는 지금 비밀번호를 확인하고 바꾼다. 지금 쓰는 세션만 남기고 다른 기기는 모두 로그아웃된다.
func (s *Service) ChangePassword(userID int64, current, next, keepToken string) error {
	var hash string
	if err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		return ErrNoSuchUser
	}
	if !VerifyPassword(current, hash) {
		return ErrBadCredentials
	}
	if len(next) < MinPasswordLen {
		return ErrWeakPassword
	}
	newHash, err := HashPassword(next)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, newHash, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, hashToken(keepToken)); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPassword는 서버 셸에서 계정을 되찾을 때 쓴다. 그 계정의 세션은 모두 끊긴다.
func (s *Service) SetPassword(username, password string) error {
	if len(password) < MinPasswordLen {
		return ErrWeakPassword
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE username = ?`, hash, username)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoSuchUser
	}
	_, err = s.db.Exec(`DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE username = ?)`, username)
	return err
}

// Usernames는 계정 목록이다 (서버 셸의 복구 명령용).
func (s *Service) Usernames() ([]string, error) {
	rows, err := s.db.Query(`SELECT username FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// Reset은 모든 계정과 세션을 지운다. 다음 실행에서 첫 설정이 다시 열린다.
func (s *Service) Reset() error {
	_, err := s.db.Exec(`DELETE FROM users`)
	return err
}

func validate(username, password string) error {
	if !usernamePattern.MatchString(username) {
		return ErrInvalidUsername
	}
	if len(password) < MinPasswordLen {
		return ErrWeakPassword
	}
	return nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
