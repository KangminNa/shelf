package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// Services는 서비스를 저장하고 읽는다.
// 구현하는 것: ServiceReader · ServiceStore · SecretStore · LiveStateStore · HookLogStore
type Services struct{ db *DB }

func NewServices(db *DB) Services { return Services{db} }

const serviceColumns = `id, name, kind, source, branch, build_path, folder, upstream, port, auto_deploy,
	container, instance, release, stopped, hook_at, hook_result`

func scanService(row interface{ Scan(...any) error }) (model.Service, error) {
	var (
		s                    model.Service
		id, hookAt           int64
		name, kind, hookRes  string
		port                 int
		alias, inst, release string
	)
	err := row.Scan(&id, &name, &kind, &s.Source, &s.Branch, &s.BuildPath, &s.Folder, &s.External, &port, &s.AutoDeploy,
		&alias, &inst, &release, &s.Live.Stopped, &hookAt, &hookRes)
	if err != nil {
		return s, err
	}
	s.ID, s.Kind, s.Port = model.ServiceID(id), model.KindName(kind), model.Port(port)
	s.Name, _ = model.ParseServiceName(name)
	s.Live.Alias, s.Live.Instance, s.Live.Release, s.Live.Port = alias, inst, release, model.Port(port)
	if hookAt > 0 {
		s.HookLog = model.HookLog{At: time.Unix(hookAt, 0), Result: hookRes}
	}
	return s, nil
}

func (r Services) List(ctx context.Context) ([]model.Service, error) {
	rows, err := r.db.sql.QueryContext(ctx, `SELECT `+serviceColumns+` FROM services ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var all []model.Service
	for rows.Next() {
		s, err := scanService(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	domains, err := r.domains(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		all[i].Domains = domains[all[i].ID]
	}
	return all, nil
}

func (r Services) Get(ctx context.Context, id model.ServiceID) (model.Service, error) {
	s, err := scanService(r.db.sql.QueryRowContext(ctx, `SELECT `+serviceColumns+` FROM services WHERE id = ?`, int64(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return s, model.ErrNotFound
	}
	if err != nil {
		return s, err
	}
	domains, err := r.domains(ctx)
	if err != nil {
		return s, err
	}
	s.Domains = domains[s.ID]
	return s, nil
}

func (r Services) domains(ctx context.Context) (map[model.ServiceID][]model.Domain, error) {
	rows, err := r.db.sql.QueryContext(ctx, `SELECT id, service_id, domain, https, hsts FROM domains ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[model.ServiceID][]model.Domain{}
	for rows.Next() {
		var id, sid int64
		var name string
		var d model.Domain
		if err := rows.Scan(&id, &sid, &name, &d.HTTPS, &d.HSTS); err != nil {
			return nil, err
		}
		dn, err := model.ParseDomainName(name)
		if err != nil {
			continue // 저장된 값이 모양에 맞지 않으면 웹서버에 넘기지 않는다
		}
		d.ID, d.Domain = model.DomainID(id), dn
		out[model.ServiceID(sid)] = append(out[model.ServiceID(sid)], d)
	}
	return out, rows.Err()
}

func (r Services) NameTaken(ctx context.Context, n model.ServiceName) bool {
	var c int
	r.db.sql.QueryRowContext(ctx, `SELECT count(*) FROM services WHERE name = ?`, n.String()).Scan(&c)
	return c > 0
}

// Create는 서비스를 넣는다. ID가 0이 아니면 그 번호를 쓴다 — v1 앱 번호를 지켜야 웹훅 주소가 그대로다.
func (r Services) Create(ctx context.Context, s model.NewService) (model.ServiceID, error) {
	var id any
	if s.ID != 0 {
		id = int64(s.ID)
	}
	res, err := r.db.sql.ExecContext(ctx, `INSERT INTO services (id, name, kind, source, branch, build_path, folder, upstream, port, auto_deploy, container)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, s.Name.String(), string(s.Kind), s.Source, s.Branch, s.BuildPath, s.Folder, s.External, int(s.Port), s.AutoDeploy, s.Alias)
	if err != nil {
		return 0, err
	}
	n, err := res.LastInsertId()
	return model.ServiceID(n), err
}

func (r Services) Update(ctx context.Context, id model.ServiceID, s model.ServiceSettings) error {
	res, err := r.db.sql.ExecContext(ctx, `UPDATE services SET source = ?, branch = ?, build_path = ?, folder = ?, upstream = ?, port = ?, auto_deploy = ? WHERE id = ?`,
		s.Source, s.Branch, s.BuildPath, s.Folder, s.External, int(s.Port), s.AutoDeploy, int64(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (r Services) Delete(ctx context.Context, id model.ServiceID) error {
	res, err := r.db.sql.ExecContext(ctx, `DELETE FROM services WHERE id = ?`, int64(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// ── 비밀 ─────────────────────────────────

func (r Services) secrets(ctx context.Context, id model.ServiceID) (model.ServiceSecrets, error) {
	var env, vols, token, hook string
	err := r.db.sql.QueryRowContext(ctx, `SELECT env, volumes, git_token, webhook_secret FROM services WHERE id = ?`, int64(id)).Scan(&env, &vols, &token, &hook)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ServiceSecrets{}, model.ErrNotFound
	}
	if err != nil {
		return model.ServiceSecrets{}, err
	}
	return model.ServiceSecrets{Env: model.StoredEnvVars(env), Volumes: model.StoredVolumes(vols), GitToken: token, WebhookSecret: hook}, nil
}

// Secrets는 SecretStore다 (Services와 같은 행을 쓰지만 하는 일이 달라 따로 내보낸다).
type Secrets struct{ r Services }

func NewSecrets(db *DB) Secrets { return Secrets{Services{db}} }

func (s Secrets) Get(ctx context.Context, id model.ServiceID) (model.ServiceSecrets, error) {
	return s.r.secrets(ctx, id)
}

func (s Secrets) Set(ctx context.Context, id model.ServiceID, sec model.ServiceSecrets) error {
	_, err := s.r.db.sql.ExecContext(ctx, `UPDATE services SET env = ?, volumes = ?, git_token = ?, webhook_secret = ? WHERE id = ?`,
		sec.Env.Text(), sec.Volumes.Text(), sec.GitToken, sec.WebhookSecret, int64(id))
	return err
}

// LiveStates는 지금 도는 것을 저장한다 (LiveStateStore).
type LiveStates struct{ db *DB }

func NewLiveStates(db *DB) LiveStates { return LiveStates{db} }

func (l LiveStates) Save(ctx context.Context, id model.ServiceID, s model.LiveState) error {
	_, err := l.db.sql.ExecContext(ctx, `UPDATE services SET container = ?, instance = ?, release = ?, port = CASE WHEN ? > 0 THEN ? ELSE port END, stopped = ? WHERE id = ?`,
		s.Alias, s.Instance, s.Release, int(s.Port), int(s.Port), s.Stopped, int64(id))
	return err
}

// HookLogs는 웹훅을 받은 기록을 저장한다 (HookLogStore).
type HookLogs struct{ db *DB }

func NewHookLogs(db *DB) HookLogs { return HookLogs{db} }

func (h HookLogs) Save(ctx context.Context, id model.ServiceID, l model.HookLog) error {
	_, err := h.db.sql.ExecContext(ctx, `UPDATE services SET hook_at = ?, hook_result = ? WHERE id = ?`, l.At.Unix(), l.Result, int64(id))
	return err
}

// ── 주소 ─────────────────────────────────

// Domains는 서비스에 붙은 주소를 저장한다 (DomainStore).
type Domains struct{ db *DB }

func NewDomains(db *DB) Domains { return Domains{db} }

func (d Domains) FindOwner(ctx context.Context, name model.DomainName) (model.ServiceID, bool) {
	var id int64
	if err := d.db.sql.QueryRowContext(ctx, `SELECT service_id FROM domains WHERE domain = ?`, name.String()).Scan(&id); err != nil {
		return 0, false
	}
	return model.ServiceID(id), true
}

func (d Domains) Add(ctx context.Context, id model.ServiceID, in model.DomainInput) error {
	_, err := d.db.sql.ExecContext(ctx, `INSERT INTO domains (service_id, domain, https) VALUES (?, ?, ?)`, int64(id), in.Domain.String(), in.HTTPS)
	return err
}

func (d Domains) Remove(ctx context.Context, id model.ServiceID, domain model.DomainID) error {
	res, err := d.db.sql.ExecContext(ctx, `DELETE FROM domains WHERE id = ? AND service_id = ?`, int64(domain), int64(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.ErrNotFound
	}
	return nil
}
