package v1import

import (
	"database/sql"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/deploy"
	"github.com/KangminNa/naru/internal/service"
	"github.com/KangminNa/naru/internal/store"
)

// ServicesReport는 v1 앱과 프록시 호스트를 서비스로 옮긴 결과다.
type ServicesReport struct {
	Skipped  bool
	Apps     int // 앱 → repo·image 서비스
	External int // 직접 만든 프록시 호스트 → external 서비스
	Domains  int
	History  int // 옮긴 배포 기록
}

// RunServices는 v1 앱(deploy.db)과 프록시 호스트(proxy.db)를 서비스로 옮긴다. 처음 한 번만.
//   - 앱 번호를 그대로 쓴다 — GitHub에 걸어 둔 웹훅 주소(/hooks/{번호})가 바뀌지 않게.
//   - 앱 컨테이너 이름(shelf-{이름})을 그대로 넘겨받는다 — 앱을 다시 띄우지 않는다.
//   - 관리 화면용 호스트는 서비스가 아니라 설정(관리 주소)으로 이미 옮겼다.
func RunServices(dataDir string, st *store.Store, repo *service.Repo) (ServicesReport, error) {
	if _, done := st.Setting(store.KeyV1ImportedServices); done {
		return ServicesReport{Skipped: true}, nil
	}
	var r ServicesReport
	byContainer := map[string]int64{}

	if db, ok, err := open(filepath.Join(dataDir, "deploy.db")); err != nil {
		return r, err
	} else if ok {
		apps, err := readApps(db)
		db.Close()
		if err != nil {
			return r, err
		}
		for _, a := range apps {
			id, err := repo.Create(a.svc, a.secrets)
			if err != nil {
				return r, fmt.Errorf("app %s: %w", a.svc.Name, err)
			}
			byContainer[a.svc.Container] = id
			r.Apps++
		}
		if db, ok, err := open(filepath.Join(dataDir, "deploy.db")); err == nil && ok {
			n, err := importHistory(db, deploy.NewStore(st.DB))
			db.Close()
			if err != nil {
				return r, fmt.Errorf("deploy history: %w", err)
			}
			r.History = n
		}
	}

	if db, ok, err := open(filepath.Join(dataDir, "proxy.db")); err != nil {
		return r, err
	} else if ok {
		hosts, err := readHosts(db)
		db.Close()
		if err != nil {
			return r, err
		}
		for _, h := range hosts {
			if repo.DomainTaken(h.domain) {
				continue
			}
			sid, isApp := byContainer[h.targetHost]
			if !isApp {
				name := uniqueName(repo, nameFromDomain(h.domain))
				id, err := repo.Create(service.Service{Name: name, Kind: service.KindExternal, Upstream: h.upstream()}, service.Secrets{})
				if err != nil {
					return r, fmt.Errorf("host %s: %w", h.domain, err)
				}
				sid = id
				r.External++
			}
			if err := repo.AddDomain(sid, service.Domain{Domain: h.domain, HTTPS: h.ssl, HSTS: h.hsts}); err != nil {
				return r, fmt.Errorf("domain %s: %w", h.domain, err)
			}
			r.Domains++
		}
	}
	return r, st.SetSetting(store.KeyV1ImportedServices, "1")
}

// importHistory는 v1 배포 기록을 옮긴다. 로그는 끝부분만 남긴다 — 실패 원인은 대개 끝에 있다.
func importHistory(db *sql.DB, history *deploy.Store) (int, error) {
	cols, err := columnsOf(db, "deployments")
	if err != nil || !cols["project_id"] {
		return 0, err
	}
	rows, err := db.Query(`SELECT project_id, status, trigger_type, commit_hash, commit_message, log, created_at, duration_ms FROM deployments ORDER BY id`)
	if err != nil {
		return 0, err
	}
	type row struct {
		project                         int64
		status, trigger, hash, msg, log string
		created, durationMs             int64
	}
	var all []row
	for rows.Next() {
		var x row
		var hash, msg, log sql.NullString
		if err := rows.Scan(&x.project, &x.status, &x.trigger, &hash, &msg, &log, &x.created, &x.durationMs); err != nil {
			rows.Close()
			return 0, err
		}
		x.hash, x.msg, x.log = hash.String, msg.String, log.String
		all = append(all, x)
	}
	rows.Close()
	n := 0
	for _, x := range all {
		status := deploy.Failed
		if x.status == "success" {
			status = deploy.Success
		}
		logTail := x.log
		if len(logTail) > 64<<10 {
			logTail = "…\n" + logTail[len(logTail)-64<<10:]
		}
		started := time.Unix(x.created, 0)
		err := history.Import(deploy.Deployment{
			ServiceID: x.project, Status: status, Trigger: "v1:" + x.trigger, Commit: x.hash, Message: x.msg, Log: logTail,
			StartedAt: started, FinishedAt: started.Add(time.Duration(x.durationMs) * time.Millisecond),
		})
		if err != nil {
			continue // 지워진 앱의 기록 — 옮길 곳이 없다
		}
		n++
	}
	return n, nil
}

type v1App struct {
	svc     service.Service
	secrets service.Secrets
}

// readApps는 v1의 어느 버전이 만든 DB든 읽는다 — 나중에 생긴 칸이 없으면 기본값으로 둔다.
func readApps(db *sql.DB) ([]v1App, error) {
	cols, err := columnsOf(db, "projects")
	if err != nil {
		return nil, err
	}
	pick := func(name, fallback string) string {
		if cols[name] {
			return name
		}
		return fallback
	}
	q := fmt.Sprintf(`SELECT id, name, %s, repo_url, %s, branch, %s, %s, auto_deploy, %s, %s, %s, webhook_secret FROM projects ORDER BY id`,
		pick("source_type", "'git'"), pick("image", "''"), pick("build_path", "''"),
		pick("container_port", "port"), pick("env", "''"), pick("volumes", "''"), pick("git_token", "''"))
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []v1App
	for rows.Next() {
		var (
			a                                                   v1App
			sourceType, repoURL, image, buildPath, env, volumes string
			gitToken, secret                                    string
			port                                                sql.NullInt64
		)
		if err := rows.Scan(&a.svc.ID, &a.svc.Name, &sourceType, &repoURL, &image, &a.svc.Branch, &buildPath, &port,
			&a.svc.AutoDeploy, &env, &volumes, &gitToken, &secret); err != nil {
			return nil, err
		}
		a.svc.Kind, a.svc.Source = service.KindRepo, repoURL
		if sourceType == "image" {
			a.svc.Kind, a.svc.Source = service.KindImage, image
		}
		a.svc.BuildPath = buildPath
		a.svc.Container = "shelf-" + a.svc.Name
		a.svc.Port = int(port.Int64)
		a.secrets = service.Secrets{Env: env, Volumes: volumes, GitToken: gitToken, WebhookSecret: secret}
		out = append(out, a)
	}
	return out, rows.Err()
}

type v1Host struct {
	domain, scheme, targetHost string
	targetPort                 int
	ssl, hsts                  bool
}

// upstream은 웹서버 컨테이너에서 본 연결 대상이다.
// v1의 127.0.0.1은 "이 서버"라는 뜻이었다 — 웹서버 컨테이너 안에서는 host.docker.internal이다.
func (h v1Host) upstream() string {
	host := h.targetHost
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		host = "host.docker.internal"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(h.targetPort))
	if h.scheme == "https" {
		return "https://" + addr
	}
	return addr
}

func readHosts(db *sql.DB) ([]v1Host, error) {
	cols, err := columnsOf(db, "proxy_hosts")
	if err != nil {
		return nil, err
	}
	hsts := "0"
	if cols["hsts_enabled"] {
		hsts = "hsts_enabled"
	}
	rows, err := db.Query(fmt.Sprintf(`SELECT domain, target_scheme, target_host, target_port, ssl_enabled, %s
		FROM proxy_hosts WHERE enabled = 1 AND description NOT LIKE ? ORDER BY id`, hsts), adminHostMarker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []v1Host
	for rows.Next() {
		var h v1Host
		if err := rows.Scan(&h.domain, &h.scheme, &h.targetHost, &h.targetPort, &h.ssl, &h.hsts); err != nil {
			return nil, err
		}
		h.domain = strings.ToLower(h.domain)
		out = append(out, h)
	}
	return out, rows.Err()
}

func columnsOf(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		cols[n] = true
	}
	return cols, rows.Err()
}

var nonName = regexp.MustCompile(`[^a-z0-9-]+`)

// nameFromDomain은 nas.example.com → "nas" 처럼 주소에서 서비스 이름을 만든다.
func nameFromDomain(domain string) string {
	first, _, _ := strings.Cut(domain, ".")
	name := strings.Trim(nonName.ReplaceAllString(strings.ToLower(first), "-"), "-")
	if name == "" {
		return "site"
	}
	return name
}

func uniqueName(repo *service.Repo, base string) string {
	name := base
	for i := 2; repo.NameTaken(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}
