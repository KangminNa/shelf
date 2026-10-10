package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// Deployments는 배포 기록을 저장하고 읽는다 (DeployHistoryReader · DeployHistoryStore).
type Deployments struct{ db *DB }

func NewDeployments(db *DB) Deployments { return Deployments{db} }

func (s Deployments) Start(ctx context.Context, id model.ServiceID, why model.DeployReason) (model.DeploymentID, error) {
	res, err := s.db.sql.ExecContext(ctx, `INSERT INTO deployments (service_id, status, trigger_type) VALUES (?, 'running', ?)`, int64(id), string(why))
	if err != nil {
		return 0, err
	}
	n, err := res.LastInsertId()
	return model.DeploymentID(n), err
}

func (s Deployments) SaveLog(ctx context.Context, d model.DeploymentID, log string) error {
	_, err := s.db.sql.ExecContext(ctx, `UPDATE deployments SET log = ? WHERE id = ?`, log, int64(d))
	return err
}

func (s Deployments) Finish(ctx context.Context, d model.Deployment) error {
	_, err := s.db.sql.ExecContext(ctx, `UPDATE deployments SET status = ?, commit_hash = ?, commit_message = ?, image = ?, log = ?, finished_at = unixepoch() WHERE id = ?`,
		string(d.Status), d.Commit, d.Message, string(d.Image), d.Log, int64(d.ID))
	return err
}

// CloseInterrupted는 Naru가 배포 도중에 꺼졌다 켜졌을 때 남은 "진행 중"을 실패로 정리한다.
func (s Deployments) CloseInterrupted(ctx context.Context, note string) (int, error) {
	res, err := s.db.sql.ExecContext(ctx, `UPDATE deployments SET status = 'failed', finished_at = unixepoch(), log = log || ? WHERE status = 'running'`, "\n"+note+"\n")
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

const deployColumns = `id, service_id, status, trigger_type, commit_hash, commit_message, image, log, started_at, finished_at`

func scanDeployment(row interface{ Scan(...any) error }) (model.Deployment, error) {
	var d model.Deployment
	var id, sid, started, finished int64
	var status, reason, image string
	err := row.Scan(&id, &sid, &status, &reason, &d.Commit, &d.Message, &image, &d.Log, &started, &finished)
	d.ID, d.ServiceID = model.DeploymentID(id), model.ServiceID(sid)
	d.Status, d.Reason, d.Image = model.DeployStatus(status), model.DeployReason(reason), model.ImageID(image)
	d.StartedAt = time.Unix(started, 0)
	if finished > 0 {
		d.FinishedAt = time.Unix(finished, 0)
	}
	return d, err
}

func (s Deployments) Get(ctx context.Context, d model.DeploymentID) (model.Deployment, error) {
	dep, err := scanDeployment(s.db.sql.QueryRowContext(ctx, `SELECT `+deployColumns+` FROM deployments WHERE id = ?`, int64(d)))
	if errors.Is(err, sql.ErrNoRows) {
		return dep, model.ErrNotFound
	}
	return dep, err
}

// Recent는 최근 배포다 (기록 본문은 빼고).
func (s Deployments) Recent(ctx context.Context, id model.ServiceID, n int) ([]model.Deployment, error) {
	rows, err := s.db.sql.QueryContext(ctx, `SELECT id, service_id, status, trigger_type, commit_hash, commit_message, image, '', started_at, finished_at
		FROM deployments WHERE service_id = ? ORDER BY id DESC LIMIT ?`, int64(id), n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Succeeded는 성공한 배포 번호들이다, 최신부터.
func (s Deployments) Succeeded(ctx context.Context, id model.ServiceID) ([]model.DeploymentID, error) {
	rows, err := s.db.sql.QueryContext(ctx, `SELECT id FROM deployments WHERE service_id = ? AND status = 'success' ORDER BY id DESC`, int64(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DeploymentID
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, model.DeploymentID(n))
	}
	return out, rows.Err()
}
