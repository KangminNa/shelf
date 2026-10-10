package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// v1(Shelf)이 남긴 데이터를 옮겼는지 표시하는 설정 키. 옮기기는 이 패키지 안의 일이라 밖에 내보내지 않는다.
const (
	keyV1Imported = "v1_imported" // 계정·관리 주소
	// 앱·프록시 호스트. M1에서 계정만 옮긴 설치도 다시 볼 수 있게 따로 둔다.
	keyV1ImportedServices = "v1_imported_services"
	keyV1ImportedAlerts   = "v1_imported_alerts" // 알림 주소 (M6)
)

// v1은 ADMIN_DOMAIN을 이 설명이 붙은 프록시 호스트로 등록했다.
const adminHostMarker = "%auto-registered via ADMIN_DOMAIN%"

// V1Report는 v1 데이터를 옮긴 결과다.
type V1Report struct {
	Found       bool   // v1 데이터가 있었다
	Users       int    // 옮긴 계정
	AdminDomain string // 옮긴 관리 주소
	Apps        int    // 앱 → repo·image 서비스
	External    int    // 직접 만든 프록시 호스트 → external 서비스
	Domains     int
	History     int // 옮긴 배포 기록
	Channels    int // 옮긴 알림 주소
	// Skipped는 옮기지 못한 것과 이유다 — 실서버를 넘길 때 무엇이 빠졌는지 보이게. 비밀(토큰·알림 주소)은 담지 않는다.
	Skipped []string
}

func (r *V1Report) skip(format string, args ...any) {
	r.Skipped = append(r.Skipped, fmt.Sprintf(format, args...))
}

// ImportV1은 dataDir에 있는 v1 DB(auth.db · proxy.db · deploy.db)를 처음 한 번만 옮긴다.
// v1 DB는 읽기 전용으로만 연다 — 문제가 생기면 v1 이미지를 다시 띄워 그대로 돌아갈 수 있어야 한다.
//   - 앱 번호를 그대로 쓴다 — GitHub에 걸어 둔 웹훅 주소(/hooks/{번호})가 바뀌지 않게.
//   - 앱 컨테이너 이름(shelf-{이름})을 그대로 넘겨받는다 — 앱을 다시 띄우지 않는다.
//   - 관리 화면용 호스트는 서비스가 아니라 설정(관리 주소)으로 옮긴다.
//   - 모양이 맞지 않는 값(이름·주소)은 건너뛴다 — 웹서버 설정에 잘못된 값이 닿지 않게.
//
// 종류 이름(repo·image·external)을 이 함수가 직접 쓰는 것은 규칙 R9의 예외다 — v1의 칸을 v2의 종류로 옮기는 곳이 여기뿐이다.
func ImportV1(ctx context.Context, db *DB, dataDir string) (V1Report, error) {
	var r V1Report
	set := NewSettings(db)
	if _, done := set.Get(ctx, keyV1Imported); !done {
		if err := importAccounts(ctx, db, dataDir, &r); err != nil {
			return r, err
		}
		if err := set.Set(ctx, keyV1Imported, "1"); err != nil {
			return r, err
		}
	}
	if _, done := set.Get(ctx, keyV1ImportedServices); !done {
		if err := importServices(ctx, db, dataDir, &r); err != nil {
			return r, err
		}
		if err := set.Set(ctx, keyV1ImportedServices, "1"); err != nil {
			return r, err
		}
	}
	if _, done := set.Get(ctx, keyV1ImportedAlerts); !done {
		if err := importChannels(ctx, db, dataDir, &r); err != nil {
			return r, err
		}
		if err := set.Set(ctx, keyV1ImportedAlerts, "1"); err != nil {
			return r, err
		}
	}
	return r, nil
}

// importChannels는 v1의 알림 채널 중 켜져 있고 주소가 올바른 것을 옮긴다. 발송 기록은 옮기지 않는다.
func importChannels(ctx context.Context, db *DB, dataDir string, r *V1Report) error {
	v1, ok, err := openV1(dataDir, "notify.db")
	if err != nil || !ok {
		return err
	}
	defer v1.Close()
	rows, err := v1.QueryContext(ctx, `SELECT id, url, secret, description FROM channels WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		return err
	}
	type row struct {
		id                int64
		url, secret, name string
	}
	var all []row
	for rows.Next() {
		var x row
		var secret, desc sql.NullString
		if err := rows.Scan(&x.id, &x.url, &secret, &desc); err != nil {
			rows.Close()
			return err
		}
		x.secret, x.name = secret.String, desc.String
		all = append(all, x)
	}
	rows.Close()
	r.Found = true
	channels := NewChannels(db)
	for _, x := range all {
		u, err := model.ParseAlertURL(x.url)
		if err != nil {
			label := strings.TrimSpace(x.name) // 주소는 비밀이라 이름(없으면 번호)으로만 말한다
			if label == "" {
				label = fmt.Sprintf("#%d", x.id)
			}
			r.skip("알림 주소 %s: 주소 모양이 틀림", label)
			continue
		}
		name := strings.TrimSpace(x.name)
		if name == "" {
			name = hostOf(u.String())
		}
		if _, err := channels.Add(ctx, model.AlertChannel{Name: name, URL: u.String(), Secret: x.secret}); err != nil {
			return err
		}
		r.Channels++
	}
	return nil
}

func hostOf(raw string) string {
	rest := raw[strings.Index(raw, "://")+3:]
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func openV1(dataDir, file string) (*sql.DB, bool, error) {
	path := filepath.Join(dataDir, file)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	db, err := openReadOnly(path)
	if err != nil {
		return nil, false, err
	}
	return db, true, nil
}

// ── 계정 · 관리 주소 ─────────────────────────

func importAccounts(ctx context.Context, db *DB, dataDir string, r *V1Report) error {
	set := NewSettings(db)
	if v1, ok, err := openV1(dataDir, "auth.db"); err != nil {
		return err
	} else if ok {
		r.Found = true
		n, err := copyUsers(ctx, v1, db, r)
		v1.Close()
		if err != nil {
			return err
		}
		r.Users = n
	}

	if v1, ok, err := openV1(dataDir, "proxy.db"); err != nil {
		return err
	} else if ok {
		r.Found = true
		var domain string
		err := v1.QueryRowContext(ctx, `SELECT domain FROM proxy_hosts WHERE description LIKE ? ORDER BY id LIMIT 1`, adminHostMarker).Scan(&domain)
		v1.Close()
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if d, err := model.ParseDomainName(domain); err != nil && domain != "" {
			r.skip("관리 주소 %s: 주소 모양이 틀림", domain)
		} else if err == nil {
			if _, already := set.Get(ctx, model.SettingAdminDomain); !already {
				if err := set.Set(ctx, model.SettingAdminDomain, d.String()); err != nil {
					return err
				}
				r.AdminDomain = d.String()
			}
		}
	}

	// v1을 쓰던 서버는 이미 설정을 마친 서버다 — 마법사를 다시 띄우지 않는다.
	if r.Users > 0 {
		return set.Set(ctx, model.SettingSetupDone, "1")
	}
	return nil
}

// copyUsers는 v1 계정을 비밀번호 해시 그대로 옮긴다 (해시 형식이 같다). 이미 있는 이름은 건너뛴다.
func copyUsers(ctx context.Context, v1 *sql.DB, db *DB, r *V1Report) (int, error) {
	rows, err := v1.QueryContext(ctx, `SELECT username, password_hash FROM users ORDER BY id`)
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
		res, err := db.sql.ExecContext(ctx, `INSERT OR IGNORE INTO users (username, password_hash) VALUES (?, ?)`, x.name, x.hash)
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		} else {
			r.skip("계정 %s: 같은 이름이 이미 있음", x.name)
		}
	}
	return added, nil
}

// ── 앱 · 프록시 호스트 · 배포 기록 ─────────────────

func importServices(ctx context.Context, db *DB, dataDir string, r *V1Report) error {
	byContainer := map[string]model.ServiceID{}
	appPort := map[model.ServiceID]int{}

	if v1, ok, err := openV1(dataDir, "deploy.db"); err != nil {
		return err
	} else if ok {
		r.Found = true
		apps, err := readApps(ctx, v1, r)
		if err != nil {
			v1.Close()
			return err
		}
		for _, a := range apps {
			if err := insertApp(ctx, db, a); err != nil {
				v1.Close()
				return fmt.Errorf("app %s: %w", a.name, err)
			}
			byContainer[a.alias] = a.id
			appPort[a.id] = a.port
			r.Apps++
		}
		n, lost, err := copyHistory(ctx, v1, db)
		v1.Close()
		if err != nil {
			return fmt.Errorf("deploy history: %w", err)
		}
		r.History = n
		if lost > 0 {
			r.skip("배포 기록 %d개: 지워진 앱의 것", lost)
		}
	}

	if v1, ok, err := openV1(dataDir, "proxy.db"); err != nil {
		return err
	} else if ok {
		r.Found = true
		hosts, err := readHosts(ctx, v1, r)
		var covered certNames
		if err == nil {
			covered, err = readCertificates(ctx, v1, r)
		}
		v1.Close()
		if err != nil {
			return err
		}
		services, domains := NewServices(db), NewDomains(db)
		for _, h := range hosts {
			if _, taken := domains.FindOwner(ctx, h.domain); taken {
				r.skip("주소 %s: 이미 다른 서비스에 있음", h.domain)
				continue
			}
			sid, isApp := byContainer[h.targetHost]
			if isApp && appPort[sid] == 0 && h.targetPort > 0 {
				// 포트가 비어 있던 앱 — v1이 실제로 보내던 포트를 쓴다
				if _, err := db.sql.ExecContext(ctx, `UPDATE services SET port = ? WHERE id = ?`, h.targetPort, int64(sid)); err != nil {
					return fmt.Errorf("app port %s: %w", h.domain, err)
				}
				appPort[sid] = h.targetPort
			}
			if !isApp {
				ext, err := model.ParseExternalAddress(h.address())
				if err != nil {
					r.skip("주소 %s: 연결 대상 %s 모양이 틀림", h.domain, h.address())
					continue
				}
				base := model.SuggestServiceName(h.domain.FirstLabel())
				name := base
				for i := 2; services.NameTaken(ctx, name); i++ {
					name = base.WithSuffix(i)
				}
				id, err := services.Create(ctx, model.NewService{Name: name, Kind: model.KindExternal, External: ext.String()})
				if err != nil {
					return fmt.Errorf("host %s: %w", h.domain, err)
				}
				sid = id
				r.External++
			}
			// v1은 호스트의 "인증서 사용"과 상관없이 그 주소를 덮는 인증서가 있으면 HTTPS로 서빙했다 (SNI) — 그 모습 그대로
			https := h.ssl || covered.cover(h.domain.String())
			if _, err := db.sql.ExecContext(ctx, `INSERT INTO domains (service_id, domain, https, hsts) VALUES (?, ?, ?, ?)`,
				int64(sid), h.domain.String(), https, h.hsts); err != nil {
				return fmt.Errorf("domain %s: %w", h.domain, err)
			}
			r.Domains++
		}
	}
	return nil
}

type v1App struct {
	id                                 model.ServiceID
	name                               string
	kind                               model.KindName
	source, branch, buildPath, alias   string
	port                               int
	autoDeploy                         bool
	env, volumes, gitToken, hookSecret string
}

func insertApp(ctx context.Context, db *DB, a v1App) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO services (id, name, kind, source, branch, build_path, container, port, auto_deploy, env, volumes, git_token, webhook_secret)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(a.id), a.name, string(a.kind), a.source, a.branch, a.buildPath, a.alias, a.port, a.autoDeploy, a.env, a.volumes, a.gitToken, a.hookSecret)
	return err
}

// readApps는 v1의 어느 버전이 만든 DB든 읽는다 — 나중에 생긴 칸이 없으면 기본값으로 둔다.
func readApps(ctx context.Context, v1 *sql.DB, r *V1Report) ([]v1App, error) {
	cols, err := columnsOf(ctx, v1, "projects")
	if err != nil {
		return nil, err
	}
	pick := func(name, fallback string) string {
		if cols[name] {
			return name
		}
		return fallback
	}
	// 초기 v1이 만든 앱은 포트가 예전 칸(port)에만 있다 — container_port가 비면 port
	port := "port"
	if cols["container_port"] {
		port = "coalesce(container_port, port)"
	}
	q := fmt.Sprintf(`SELECT id, name, %s, repo_url, %s, branch, %s, %s, auto_deploy, %s, %s, %s, webhook_secret FROM projects ORDER BY id`,
		pick("source_type", "'git'"), pick("image", "''"), pick("build_path", "''"),
		port, pick("env", "''"), pick("volumes", "''"), pick("git_token", "''"))
	rows, err := v1.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []v1App
	for rows.Next() {
		var (
			a                                      v1App
			id                                     int64
			sourceType, repoURL, image             string
			buildPath, env, volumes, token, secret sql.NullString
			port                                   sql.NullInt64
		)
		if err := rows.Scan(&id, &a.name, &sourceType, &repoURL, &image, &a.branch, &buildPath, &port,
			&a.autoDeploy, &env, &volumes, &token, &secret); err != nil {
			return nil, err
		}
		if _, err := model.ParseServiceName(a.name); err != nil {
			r.skip("앱 %s: 이름 모양이 틀림 (컨테이너 shelf-%s는 그대로 돈다 — 새 서비스로 다시 만든다)", a.name, a.name)
			continue
		}
		a.id, a.kind, a.source = model.ServiceID(id), model.KindRepo, repoURL
		if sourceType == "image" {
			a.kind, a.source, a.branch = model.KindImage, image, ""
		}
		a.buildPath, a.alias, a.port = buildPath.String, "shelf-"+a.name, int(port.Int64)
		a.env, a.volumes, a.gitToken, a.hookSecret = env.String, volumes.String, token.String, secret.String
		out = append(out, a)
	}
	return out, rows.Err()
}

// copyHistory는 v1 배포 기록을 옮긴다. 로그는 끝부분만 남긴다 — 실패 원인은 대개 끝에 있다.
// lost는 옮길 곳이 없던 기록(지워진 앱의 것) 수다.
func copyHistory(ctx context.Context, v1 *sql.DB, db *DB) (n, lost int, err error) {
	cols, err := columnsOf(ctx, v1, "deployments")
	if err != nil || !cols["project_id"] {
		return 0, 0, err
	}
	rows, err := v1.QueryContext(ctx, `SELECT project_id, status, trigger_type, commit_hash, commit_message, log, created_at, duration_ms FROM deployments ORDER BY id`)
	if err != nil {
		return 0, 0, err
	}
	type row struct {
		project             int64
		status, trigger     string
		hash, msg, log      sql.NullString
		created, durationMs sql.NullInt64
	}
	var all []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.project, &x.status, &x.trigger, &x.hash, &x.msg, &x.log, &x.created, &x.durationMs); err != nil {
			rows.Close()
			return 0, 0, err
		}
		all = append(all, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	for _, x := range all {
		status := model.DeployFailed
		if x.status == "success" {
			status = model.DeploySuccess
		}
		log := x.log.String
		if len(log) > 64<<10 {
			log = "…\n" + log[len(log)-64<<10:]
		}
		started := time.Unix(x.created.Int64, 0)
		finished := started.Add(time.Duration(x.durationMs.Int64) * time.Millisecond)
		_, err := db.sql.ExecContext(ctx, `INSERT INTO deployments (service_id, status, trigger_type, commit_hash, commit_message, log, started_at, finished_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			x.project, string(status), "v1:"+x.trigger, x.hash.String, x.msg.String, log, started.Unix(), finished.Unix())
		if err != nil {
			lost++ // 지워진 앱의 기록 — 옮길 곳이 없다
			continue
		}
		n++
	}
	return n, lost, nil
}

type v1Host struct {
	domain             model.DomainName
	scheme, targetHost string
	targetPort         int
	ssl, hsts          bool
}

// address는 v1의 연결 대상이다. 127.0.0.1(이 서버)은 ParseExternalAddress가 host.docker.internal로 바꾼다.
func (h v1Host) address() string {
	addr := net.JoinHostPort(h.targetHost, strconv.Itoa(h.targetPort))
	if h.scheme == "https" {
		return "https://" + addr
	}
	return addr
}

func readHosts(ctx context.Context, v1 *sql.DB, r *V1Report) ([]v1Host, error) {
	cols, err := columnsOf(ctx, v1, "proxy_hosts")
	if err != nil {
		return nil, err
	}
	hsts := "0"
	if cols["hsts_enabled"] {
		hsts = "hsts_enabled"
	}
	rows, err := v1.QueryContext(ctx, fmt.Sprintf(`SELECT domain, target_scheme, target_host, target_port, ssl_enabled, %s, enabled
		FROM proxy_hosts WHERE coalesce(description, '') NOT LIKE ? ORDER BY id`, hsts), adminHostMarker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []v1Host
	for rows.Next() {
		var h v1Host
		var domain string
		var enabled bool
		if err := rows.Scan(&domain, &h.scheme, &h.targetHost, &h.targetPort, &h.ssl, &h.hsts, &enabled); err != nil {
			return nil, err
		}
		if !enabled {
			r.skip("주소 %s: v1에서 꺼져 있음", domain)
			continue
		}
		d, err := model.ParseDomainName(strings.ToLower(domain))
		if err != nil {
			r.skip("주소 %s: 주소 모양이 틀림", domain)
			continue
		}
		h.domain = d
		out = append(out, h)
	}
	return out, rows.Err()
}

// certNames는 v1 인증서가 덮던 이름이다 ("*.example.com" 포함, 소문자).
type certNames map[string]bool

// cover는 그 주소를 덮는 인증서가 있었는가 — 같은 이름, 또는 한 단계 와일드카드.
func (c certNames) cover(domain string) bool {
	if c[domain] {
		return true
	}
	_, parent, ok := strings.Cut(domain, ".")
	return ok && c["*."+parent]
}

// readCertificates는 v1 인증서가 덮던 이름을 모으고, v2가 그대로 이어받지 못하는 것을 알린다.
// 인증서 자체는 옮기지 않는다 — Caddy가 새로 받는다. 보통의 Let's Encrypt 인증서(HTTP 확인)는 새로 받으면 되니 말하지 않는다.
// DNS 토큰은 읽지 않는다.
func readCertificates(ctx context.Context, v1 *sql.DB, r *V1Report) (certNames, error) {
	names := certNames{}
	cols, err := columnsOf(ctx, v1, "ssl_certs")
	if err != nil || !cols["domain"] {
		return names, err
	}
	pick := func(name string) string {
		if cols[name] {
			return "coalesce(" + name + ", '')"
		}
		return "''"
	}
	rows, err := v1.QueryContext(ctx, fmt.Sprintf(`SELECT domain, coalesce(provider, ''), %s, %s FROM ssl_certs ORDER BY id`, pick("domains"), pick("dns_provider")))
	if err != nil {
		return names, err
	}
	defer rows.Close()
	for rows.Next() {
		var domain, provider, domains, dns string
		if err := rows.Scan(&domain, &provider, &domains, &dns); err != nil {
			return names, err
		}
		list := strings.Fields(domains)
		if len(list) == 0 {
			list = []string{domain}
		}
		for _, n := range list {
			names[strings.ToLower(n)] = true
		}
		label := strings.Join(list, ", ")
		switch {
		case dns != "":
			r.skip("인증서 %s: DNS-01(%s) — v2는 HTTP 확인으로만 받는다. 와일드카드는 받지 못하고, 이름마다 이 서버 80으로 닿아야 한다", label, dns)
		case provider == "manual":
			r.skip("인증서 %s: 직접 올린 인증서 — 옮기지 않는다. 이 서버 80으로 닿으면 Caddy가 새로 받는다", label)
		case provider == "selfsigned":
			r.skip("인증서 %s: 자체 서명 — 옮기지 않는다. 이 서버 80으로 닿으면 Caddy가 새로 받는다", label)
		}
	}
	return names, rows.Err()
}

func columnsOf(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
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
