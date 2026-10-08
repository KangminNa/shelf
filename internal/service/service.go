// Package service는 서비스와 그 도메인을 저장하고 읽는다. SQL은 이 패키지 안에만 있다.
package service

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

type Kind string

const (
	KindRepo     Kind = "repo"
	KindImage    Kind = "image"
	KindStatic   Kind = "static"
	KindExternal Kind = "external"
)

func (k Kind) Valid() bool {
	return k == KindRepo || k == KindImage || k == KindStatic || k == KindExternal
}

type Domain struct {
	ID     int64
	Domain string
	HTTPS  bool
	HSTS   bool
}

// Service에는 비밀(env·토큰·웹훅 시크릿)이 들어 있지 않다 — 화면으로 가는 값만 담는다.
type Service struct {
	ID         int64
	Name       string
	Kind       Kind
	Source     string
	Branch     string
	BuildPath  string
	Folder     string // static: 서빙할 저장소 안의 폴더
	Container  string // 네트워크에서 이 서비스를 부르는 이름 (웹서버가 여기로 보낸다)
	Instance   string // 지금 요청을 받는 실제 컨테이너
	Release    string // static: 지금 서빙 중인 배포
	Port       int
	Upstream   string
	AutoDeploy bool
	Stopped    bool
	HookAt     int64  // 웹훅을 마지막으로 받은 때 (unix 초, 0이면 없음)
	HookResult string // 그때의 결과
	Domains    []Domain
}

// Secrets는 배포할 때만 읽는다. 화면으로 보내지 않는다.
type Secrets struct {
	Env           string
	Volumes       string
	GitToken      string
	WebhookSecret string
}

var ErrNotFound = errors.New("service not found")

// Target은 웹서버가 요청을 보낼 곳이다. 모르면 빈 문자열.
func (s Service) Target() string {
	switch s.Kind {
	case KindRepo, KindImage:
		if s.Container == "" || s.Port == 0 {
			return ""
		}
		return s.Container + ":" + strconv.Itoa(s.Port)
	case KindExternal:
		return s.Upstream
	}
	return ""
}

// CurrentContainer는 지금 요청을 받는 컨테이너 이름이다.
func (s Service) CurrentContainer() string {
	if s.Instance != "" {
		return s.Instance
	}
	return s.Container
}

// PrimaryDomain은 대표 주소다 (먼저 등록한 것).
func (s Service) PrimaryDomain() string {
	if len(s.Domains) == 0 {
		return ""
	}
	return s.Domains[0].Domain
}

// HasContainer는 이 서비스가 컨테이너로 도는가.
func (s Service) HasContainer() bool { return s.Kind == KindRepo || s.Kind == KindImage }

// Deployable은 배포라는 것이 있는가 (외부 연결은 없다).
func (s Service) Deployable() bool { return s.Kind != KindExternal }

type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const columns = `id, name, kind, source, branch, build_path, folder, container, instance, release, port, upstream, auto_deploy, stopped, hook_at, hook_result`

func scan(row interface{ Scan(...any) error }) (Service, error) {
	var s Service
	var kind string
	err := row.Scan(&s.ID, &s.Name, &kind, &s.Source, &s.Branch, &s.BuildPath, &s.Folder, &s.Container, &s.Instance, &s.Release,
		&s.Port, &s.Upstream, &s.AutoDeploy, &s.Stopped, &s.HookAt, &s.HookResult)
	s.Kind = Kind(kind)
	return s, err
}

// List는 모든 서비스를 이름순으로, 도메인과 함께 돌려준다.
func (r *Repo) List() ([]Service, error) {
	rows, err := r.db.Query(`SELECT ` + columns + ` FROM services ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var all []Service
	for rows.Next() {
		s, err := scan(rows)
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
	domains, err := r.allDomains()
	if err != nil {
		return nil, err
	}
	for i := range all {
		all[i].Domains = domains[all[i].ID]
	}
	return all, nil
}

func (r *Repo) Get(id int64) (Service, error) {
	s, err := scan(r.db.QueryRow(`SELECT `+columns+` FROM services WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Service{}, ErrNotFound
	}
	if err != nil {
		return Service{}, err
	}
	domains, err := r.allDomains()
	if err != nil {
		return Service{}, err
	}
	s.Domains = domains[s.ID]
	return s, nil
}

func (r *Repo) allDomains() (map[int64][]Domain, error) {
	rows, err := r.db.Query(`SELECT id, service_id, domain, https, hsts FROM domains ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Domain{}
	for rows.Next() {
		var d Domain
		var sid int64
		if err := rows.Scan(&d.ID, &sid, &d.Domain, &d.HTTPS, &d.HSTS); err != nil {
			return nil, err
		}
		out[sid] = append(out[sid], d)
	}
	return out, rows.Err()
}

// Create는 서비스를 넣는다. s.ID가 0이 아니면 그 번호를 쓴다 — v1 앱 번호를 지켜야 웹훅 주소가 그대로다.
func (r *Repo) Create(s Service, sec Secrets) (int64, error) {
	if s.Branch == "" {
		s.Branch = "main"
	}
	var id any
	if s.ID != 0 {
		id = s.ID
	}
	res, err := r.db.Exec(`INSERT INTO services (id, name, kind, source, branch, build_path, folder, container, port, upstream, auto_deploy, env, volumes, git_token, webhook_secret)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, s.Name, string(s.Kind), s.Source, s.Branch, s.BuildPath, s.Folder, s.Container, s.Port, s.Upstream, s.AutoDeploy,
		sec.Env, sec.Volumes, sec.GitToken, sec.WebhookSecret)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Settings는 화면에서 고칠 수 있는 값이다.
type Settings struct {
	Source     string
	Branch     string
	BuildPath  string
	Folder     string
	Port       int
	Upstream   string
	AutoDeploy bool
	Env        string
	Volumes    string
	GitToken   *string // nil이면 그대로 둔다 (화면은 토큰을 보여주지 않으므로 빈 칸 = 바꾸지 않음)
}

func (r *Repo) Update(id int64, st Settings) error {
	if st.Branch == "" {
		st.Branch = "main"
	}
	res, err := r.db.Exec(`UPDATE services SET source = ?, branch = ?, build_path = ?, folder = ?, port = ?, upstream = ?, auto_deploy = ?, env = ?, volumes = ? WHERE id = ?`,
		st.Source, st.Branch, st.BuildPath, st.Folder, st.Port, st.Upstream, st.AutoDeploy, st.Env, st.Volumes, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if st.GitToken != nil {
		_, err = r.db.Exec(`UPDATE services SET git_token = ? WHERE id = ?`, *st.GitToken, id)
	}
	return err
}

// Secrets는 배포에 쓸 비밀을 읽는다.
func (r *Repo) Secrets(id int64) (Secrets, error) {
	var s Secrets
	err := r.db.QueryRow(`SELECT env, volumes, git_token, webhook_secret FROM services WHERE id = ?`, id).Scan(&s.Env, &s.Volumes, &s.GitToken, &s.WebhookSecret)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

// HasGitToken은 토큰이 저장돼 있는가 (값은 돌려주지 않는다).
func (r *Repo) HasGitToken(id int64) bool {
	var n int
	r.db.QueryRow(`SELECT count(*) FROM services WHERE id = ? AND git_token <> ''`, id).Scan(&n)
	return n > 0
}

func (r *Repo) SetInstance(id int64, instance string) error {
	_, err := r.db.Exec(`UPDATE services SET instance = ? WHERE id = ?`, instance, id)
	return err
}

func (r *Repo) SetRelease(id int64, release string) error {
	_, err := r.db.Exec(`UPDATE services SET release = ? WHERE id = ?`, release, id)
	return err
}

func (r *Repo) SetPort(id int64, port int) error {
	_, err := r.db.Exec(`UPDATE services SET port = ? WHERE id = ?`, port, id)
	return err
}

func (r *Repo) SetStopped(id int64, stopped bool) error {
	_, err := r.db.Exec(`UPDATE services SET stopped = ? WHERE id = ?`, stopped, id)
	return err
}

// RecordHook은 웹훅을 받았다는 사실과 결과를 남긴다.
func (r *Repo) RecordHook(id, at int64, result string) error {
	_, err := r.db.Exec(`UPDATE services SET hook_at = ?, hook_result = ? WHERE id = ?`, at, result, id)
	return err
}

// SetContainer는 네트워크에서 서비스를 부르는 이름을 정한다 (처음 배포할 때).
func (r *Repo) SetContainer(id int64, name string) error {
	_, err := r.db.Exec(`UPDATE services SET container = ? WHERE id = ?`, name, id)
	return err
}

func (r *Repo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM services WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddDomain은 서비스에 주소를 붙인다. 다른 서비스가 쓰는 주소면 실패한다.
func (r *Repo) AddDomain(serviceID int64, d Domain) error {
	_, err := r.db.Exec(`INSERT INTO domains (service_id, domain, https, hsts) VALUES (?, ?, ?, ?)`,
		serviceID, strings.ToLower(d.Domain), d.HTTPS, d.HSTS)
	return err
}

// RemoveDomain은 그 서비스의 주소 하나를 뗀다.
func (r *Repo) RemoveDomain(serviceID, domainID int64) error {
	res, err := r.db.Exec(`DELETE FROM domains WHERE id = ? AND service_id = ?`, domainID, serviceID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// NameTaken은 이름이 이미 쓰이고 있는가.
func (r *Repo) NameTaken(name string) bool {
	var n int
	r.db.QueryRow(`SELECT count(*) FROM services WHERE name = ?`, name).Scan(&n)
	return n > 0
}

// DomainTaken은 주소가 이미 어떤 서비스에 붙어 있는가.
func (r *Repo) DomainTaken(domain string) bool {
	var n int
	r.db.QueryRow(`SELECT count(*) FROM domains WHERE domain = ?`, strings.ToLower(domain)).Scan(&n)
	return n > 0
}
