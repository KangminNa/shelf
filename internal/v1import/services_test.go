package v1import

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/KangminNa/naru/internal/service"
	"github.com/KangminNa/naru/internal/store"
)

func run(t *testing.T, dir, file string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(s, err)
		}
	}
}

const v1Projects = `CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, repo_url TEXT NOT NULL,
	branch TEXT NOT NULL DEFAULT 'main', port INTEGER, env TEXT DEFAULT '', domain TEXT DEFAULT '', webhook_secret TEXT NOT NULL,
	auto_deploy INTEGER NOT NULL DEFAULT 1, source_type TEXT NOT NULL DEFAULT 'git', image TEXT DEFAULT '', container_port INTEGER,
	volumes TEXT DEFAULT '', git_token TEXT DEFAULT '', build_path TEXT NOT NULL DEFAULT '')`

const v1Hosts = `CREATE TABLE proxy_hosts (id INTEGER PRIMARY KEY AUTOINCREMENT, domain TEXT NOT NULL UNIQUE, target_scheme TEXT NOT NULL DEFAULT 'http',
	target_host TEXT NOT NULL, target_port INTEGER NOT NULL, ssl_enabled INTEGER NOT NULL DEFAULT 0, force_ssl INTEGER NOT NULL DEFAULT 0,
	enabled INTEGER NOT NULL DEFAULT 1, description TEXT DEFAULT '', hsts_enabled INTEGER NOT NULL DEFAULT 0)`

func openRepo(t *testing.T, dir string) (*store.Store, *service.Repo) {
	t.Helper()
	st, err := store.Open(filepath.Join(dir, "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, service.NewRepo(st.DB)
}

func TestAppsAndHostsBecomeServices(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "deploy.db", v1Projects,
		`INSERT INTO projects (id, name, repo_url, webhook_secret, container_port, build_path, env, git_token) VALUES (3, 'blog', 'https://github.com/me/blog', 'whsec', 3000, '', 'A=1', 'ghp_x')`,
		`INSERT INTO projects (id, name, repo_url, webhook_secret, source_type, image, container_port, auto_deploy) VALUES (6, 'api', '', 'whsec2', 'image', 'ghcr.io/me/api:latest', 8080, 0)`,
	)
	run(t, dir, "proxy.db", v1Hosts,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, ssl_enabled, hsts_enabled, description) VALUES ('blog.example.com', 'shelf-blog', 3000, 1, 1, 'Auto-created by app blog')`,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, description) VALUES ('nas.example.com', '127.0.0.1', 5000, 'NAS')`,
		`INSERT INTO proxy_hosts (domain, target_scheme, target_host, target_port, description) VALUES ('router.example.com', 'https', '192.168.0.1', 443, '')`,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, enabled) VALUES ('off.example.com', '10.0.0.9', 80, 0)`,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, description) VALUES ('shelf.example.com', '127.0.0.1', 81, 'Shelf admin (auto-registered via ADMIN_DOMAIN)')`,
	)
	st, repo := openRepo(t, dir)

	r, err := RunServices(dir, st, repo)
	if err != nil {
		t.Fatal(err)
	}
	if r.Apps != 2 || r.External != 2 || r.Domains != 3 {
		t.Fatalf("%+v", r)
	}

	blog, err := repo.Get(3)
	if err != nil {
		t.Fatal("app ids are kept so webhook URLs survive:", err)
	}
	if blog.Kind != service.KindRepo || blog.Container != "shelf-blog" || blog.Port != 3000 || blog.Target() != "shelf-blog:3000" {
		t.Fatalf("%+v", blog)
	}
	if len(blog.Domains) != 1 || !blog.Domains[0].HTTPS || !blog.Domains[0].HSTS {
		t.Fatalf("the app's own host becomes its domain: %+v", blog.Domains)
	}

	api, _ := repo.Get(6)
	if api.Kind != service.KindImage || api.Source != "ghcr.io/me/api:latest" || api.AutoDeploy {
		t.Fatalf("%+v", api)
	}

	all, _ := repo.List()
	targets := map[string]string{}
	for _, s := range all {
		targets[s.PrimaryDomain()] = s.Target()
	}
	if targets["nas.example.com"] != "host.docker.internal:5000" {
		t.Fatalf("v1's 127.0.0.1 meant this server: %q", targets["nas.example.com"])
	}
	if targets["router.example.com"] != "https://192.168.0.1:443" {
		t.Fatalf("https upstreams keep their scheme: %q", targets["router.example.com"])
	}
	if _, ok := targets["shelf.example.com"]; ok {
		t.Fatal("the admin host is a setting, not a service")
	}
	if _, ok := targets["off.example.com"]; ok {
		t.Fatal("disabled hosts are not imported")
	}

	if again, _ := RunServices(dir, st, repo); !again.Skipped {
		t.Fatal("runs once")
	}
}

// 아주 오래된 v1(빌드 경로·이미지 칸이 생기기 전)도 읽는다.
func TestOldV1SchemaStillImports(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "deploy.db",
		`CREATE TABLE projects (id INTEGER PRIMARY KEY, name TEXT, repo_url TEXT, branch TEXT, port INTEGER, webhook_secret TEXT, auto_deploy INTEGER)`,
		`INSERT INTO projects VALUES (1, 'old', 'https://github.com/me/old', 'main', 4000, 's', 1)`,
	)
	st, repo := openRepo(t, dir)
	if r, err := RunServices(dir, st, repo); err != nil || r.Apps != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	old, _ := repo.Get(1)
	if old.Port != 4000 || old.Kind != service.KindRepo {
		t.Fatalf("%+v", old)
	}
}

func TestNameCollisionsGetASuffix(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "deploy.db", v1Projects,
		`INSERT INTO projects (id, name, repo_url, webhook_secret, container_port) VALUES (1, 'nas', 'https://github.com/me/nas', 's', 80)`)
	run(t, dir, "proxy.db", v1Hosts,
		`INSERT INTO proxy_hosts (domain, target_host, target_port) VALUES ('nas.example.com', '192.168.0.20', 5000)`)
	st, repo := openRepo(t, dir)
	if _, err := RunServices(dir, st, repo); err != nil {
		t.Fatal(err)
	}
	all, _ := repo.List()
	if len(all) != 2 || all[1].Name != "nas-2" {
		t.Fatalf("%+v", all)
	}
}
