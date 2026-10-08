// Package deploy는 서비스를 올린다 — 소스를 가져와 빌드하거나 받고, 새 컨테이너가 응답한 뒤에 옛 것을 내린다.
package deploy

import (
	"database/sql"
	"errors"
	"time"
)

type Status string

const (
	Running Status = "running"
	Success Status = "success"
	Failed  Status = "failed"
)

type Deployment struct {
	ID         int64
	ServiceID  int64
	Status     Status
	Trigger    string // manual | webhook | create | rollback | v1
	Commit     string
	Message    string
	Image      string
	Log        string
	StartedAt  time.Time
	FinishedAt time.Time
}

// Duration은 걸린 시간이다. 끝나지 않았으면 지금까지.
func (d Deployment) Duration() time.Duration {
	end := d.FinishedAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(d.StartedAt).Round(time.Second)
}

// ShortCommit은 화면에 보여줄 커밋 앞 7자리다.
func (d Deployment) ShortCommit() string {
	if len(d.Commit) > 7 {
		return d.Commit[:7]
	}
	return d.Commit
}

var ErrNotFound = errors.New("deployment not found")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Begin(serviceID int64, trigger string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO deployments (service_id, status, trigger_type) VALUES (?, 'running', ?)`, serviceID, trigger)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) SaveLog(id int64, log string) error {
	_, err := s.db.Exec(`UPDATE deployments SET log = ? WHERE id = ?`, log, id)
	return err
}

func (s *Store) Finish(d Deployment) error {
	_, err := s.db.Exec(`UPDATE deployments SET status = ?, commit_hash = ?, commit_message = ?, image = ?, log = ?, finished_at = unixepoch() WHERE id = ?`,
		string(d.Status), d.Commit, d.Message, d.Image, d.Log, d.ID)
	return err
}

// Interrupted는 Naru가 배포 도중에 꺼졌다 켜졌을 때 남은 "진행 중"을 실패로 정리한다.
func (s *Store) Interrupted(note string) (int64, error) {
	res, err := s.db.Exec(`UPDATE deployments SET status = 'failed', finished_at = unixepoch(), log = log || ? WHERE status = 'running'`, "\n"+note+"\n")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const deployColumns = `id, service_id, status, trigger_type, commit_hash, commit_message, image, log, started_at, finished_at`

func scanDeployment(row interface{ Scan(...any) error }) (Deployment, error) {
	var d Deployment
	var status string
	var started, finished int64
	err := row.Scan(&d.ID, &d.ServiceID, &status, &d.Trigger, &d.Commit, &d.Message, &d.Image, &d.Log, &started, &finished)
	d.Status = Status(status)
	d.StartedAt = time.Unix(started, 0)
	if finished > 0 {
		d.FinishedAt = time.Unix(finished, 0)
	}
	return d, err
}

func (s *Store) Get(id int64) (Deployment, error) {
	d, err := scanDeployment(s.db.QueryRow(`SELECT `+deployColumns+` FROM deployments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// Recent는 서비스의 최근 배포다 (로그는 빼고).
func (s *Store) Recent(serviceID int64, limit int) ([]Deployment, error) {
	rows, err := s.db.Query(`SELECT id, service_id, status, trigger_type, commit_hash, commit_message, image, '', started_at, finished_at
		FROM deployments WHERE service_id = ? ORDER BY id DESC LIMIT ?`, serviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Successful은 성공한 배포 번호들이다, 최신부터.
func (s *Store) Successful(serviceID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM deployments WHERE service_id = ? AND status = 'success' ORDER BY id DESC`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Import는 v1 배포 이력을 옮길 때 쓴다.
func (s *Store) Import(d Deployment) error {
	_, err := s.db.Exec(`INSERT INTO deployments (service_id, status, trigger_type, commit_hash, commit_message, log, started_at, finished_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ServiceID, string(d.Status), d.Trigger, d.Commit, d.Message, d.Log, d.StartedAt.Unix(), d.FinishedAt.Unix())
	return err
}
