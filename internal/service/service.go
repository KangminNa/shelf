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
	Container  string
	Port       int
	Upstream   string
	AutoDeploy bool
	Domains    []Domain
}

// Secrets는 저장할 때만 쓰고 읽어서 화면으로 보내지 않는다.
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

// PrimaryDomain은 대표 주소다 (먼저 등록한 것).
func (s Service) PrimaryDomain() string {
	if len(s.Domains) == 0 {
		return ""
	}
	return s.Domains[0].Domain
}

// HasContainer는 이 서비스가 컨테이너로 도는가.
func (s Service) HasContainer() bool { return s.Kind == KindRepo || s.Kind == KindImage }

type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const columns = `id, name, kind, source, branch, build_path, container, port, upstream, auto_deploy`

func scan(row interface{ Scan(...any) error }) (Service, error) {
	var s Service
	var kind string
	err := row.Scan(&s.ID, &s.Name, &kind, &s.Source, &s.Branch, &s.BuildPath, &s.Container, &s.Port, &s.Upstream, &s.AutoDeploy)
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
	res, err := r.db.Exec(`INSERT INTO services (id, name, kind, source, branch, build_path, container, port, upstream, auto_deploy, env, volumes, git_token, webhook_secret)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, s.Name, string(s.Kind), s.Source, s.Branch, s.BuildPath, s.Container, s.Port, s.Upstream, s.AutoDeploy,
		sec.Env, sec.Volumes, sec.GitToken, sec.WebhookSecret)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AddDomain은 서비스에 주소를 붙인다. 다른 서비스가 쓰는 주소면 실패한다.
func (r *Repo) AddDomain(serviceID int64, d Domain) error {
	_, err := r.db.Exec(`INSERT INTO domains (service_id, domain, https, hsts) VALUES (?, ?, ?, ?)`,
		serviceID, strings.ToLower(d.Domain), d.HTTPS, d.HSTS)
	return err
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
