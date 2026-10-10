package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

var ctx = context.Background()

func open(t *testing.T, dir string) *DB {
	t.Helper()
	db, err := Open(filepath.Join(dir, "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func name(s string) model.ServiceName {
	n, err := model.ParseServiceName(s)
	if err != nil {
		panic(err)
	}
	return n
}

func domain(s string) model.DomainName {
	d, err := model.ParseDomainName(s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestCreateKeepsAnExplicitID(t *testing.T) {
	r := NewServices(open(t, t.TempDir()))
	id, err := r.Create(ctx, model.NewService{ID: 6, Name: name("landing"), Kind: model.KindRepo, Alias: "shelf-landing", Port: 4023})
	if err != nil || id != 6 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	next, _ := r.Create(ctx, model.NewService{Name: name("nas"), Kind: model.KindExternal, External: "host.docker.internal:5000"})
	if next != 7 {
		t.Fatalf("later services continue after the kept id, got %d", next)
	}
	if _, err := r.Create(ctx, model.NewService{Name: name("landing"), Kind: model.KindImage}); err == nil {
		t.Fatal("names are unique")
	}
	if _, err := r.Create(ctx, model.NewService{Name: name("bad"), Kind: "nonsense"}); err == nil {
		t.Fatal("unknown kinds are refused by the schema")
	}
	if !r.NameTaken(ctx, name("nas")) || r.NameTaken(ctx, name("other")) {
		t.Fatal("NameTaken")
	}
}

func TestListCarriesDomainsInOrder(t *testing.T) {
	db := open(t, t.TempDir())
	r, d := NewServices(db), NewDomains(db)
	blog, _ := r.Create(ctx, model.NewService{Name: name("blog"), Kind: model.KindRepo, Alias: "shelf-blog", Port: 3000})
	r.Create(ctx, model.NewService{Name: name("api"), Kind: model.KindImage, Alias: "naru-api", Port: 8080})
	if err := d.Add(ctx, blog, model.DomainInput{Domain: domain("Blog.Example.com"), HTTPS: true}); err != nil {
		t.Fatal(err)
	}
	d.Add(ctx, blog, model.DomainInput{Domain: domain("www.blog.example.com"), HTTPS: true})
	if err := d.Add(ctx, blog, model.DomainInput{Domain: domain("blog.example.com")}); err == nil {
		t.Fatal("a domain belongs to one service")
	}

	all, err := r.List(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("%v %v", all, err)
	}
	if all[0].Name.String() != "api" || len(all[0].Domains) != 0 {
		t.Fatal("sorted by name; api has no domains")
	}
	b := all[1]
	if b.PrimaryDomain() != "blog.example.com" || len(b.Domains) != 2 || b.Live.Alias != "shelf-blog" || b.Port != 3000 {
		t.Fatalf("%+v", b)
	}
	if owner, ok := d.FindOwner(ctx, domain("BLOG.example.com")); !ok || owner != blog {
		t.Fatal("FindOwner matches the normalized name")
	}
	if err := d.Remove(ctx, 999, b.Domains[0].ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("a domain is removed only through its own service")
	}
	if err := d.Remove(ctx, blog, b.Domains[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestGetUnknown(t *testing.T) {
	if _, err := NewServices(open(t, t.TempDir())).Get(ctx, 42); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestSecretsAndLiveStateStaySeparate(t *testing.T) {
	db := open(t, t.TempDir())
	r := NewServices(db)
	id, _ := r.Create(ctx, model.NewService{Name: name("blog"), Kind: model.KindRepo, Alias: "naru-blog", Port: 3000})
	env, _ := model.ParseEnvVars("A=1\nB=2")
	if err := NewSecrets(db).Set(ctx, id, model.ServiceSecrets{Env: env, GitToken: "ghp_x", WebhookSecret: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := NewLiveStates(db).Save(ctx, id, model.LiveState{Alias: "naru-blog", Instance: "naru-blog-4"}); err != nil {
		t.Fatal(err)
	}
	sec, _ := NewSecrets(db).Get(ctx, id)
	if sec.GitToken != "ghp_x" || len(sec.Env.List()) != 2 {
		t.Fatalf("%+v", sec)
	}
	s, _ := r.Get(ctx, id)
	if s.Live.Instance != "naru-blog-4" || s.Port != 3000 {
		t.Fatalf("a zero port in the live state keeps the saved port: %+v", s.Live)
	}
	if s.Live.LiveDeployment() != 4 {
		t.Fatal("the live deployment comes from the instance name")
	}
	NewHookLogs(db).Save(ctx, id, model.HookLog{At: time.Unix(1790000000, 0), Result: "queued"})
	s, _ = r.Get(ctx, id)
	if s.HookLog.Result != "queued" {
		t.Fatalf("%+v", s.HookLog)
	}
}

func TestDeploymentHistory(t *testing.T) {
	db := open(t, t.TempDir())
	id, _ := NewServices(db).Create(ctx, model.NewService{Name: name("blog"), Kind: model.KindRepo})
	h := NewDeployments(db)
	first, _ := h.Start(ctx, id, model.ReasonManual)
	h.Finish(ctx, model.Deployment{ID: first, Status: model.DeploySuccess, Commit: "abc", Image: "sha256:1", Log: "ok"})
	second, _ := h.Start(ctx, id, model.ReasonPush)
	h.SaveLog(ctx, second, "building")
	if n, _ := h.CloseInterrupted(ctx, "naru restarted"); n != 1 {
		t.Fatalf("closed %d", n)
	}
	got, _ := h.Get(ctx, second)
	if got.Status != model.DeployFailed || got.Log != "building\nnaru restarted\n" || got.Reason != model.ReasonPush {
		t.Fatalf("%+v", got)
	}
	recent, _ := h.Recent(ctx, id, 10)
	if len(recent) != 2 || recent[0].ID != second || recent[0].Log != "" {
		t.Fatalf("recent is newest first and without logs: %+v", recent)
	}
	ok, _ := h.Succeeded(ctx, id)
	if len(ok) != 1 || ok[0] != first {
		t.Fatalf("%v", ok)
	}
	if _, err := h.Get(ctx, 99); !errors.Is(err, model.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestAccountsAndSessions(t *testing.T) {
	db := open(t, t.TempDir())
	a, s := NewAccounts(db), NewSessions(db)
	u, _ := model.ParseUsername("kangmin")
	id, err := a.CreateFirst(ctx, u, "salt:hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateFirst(ctx, u, "x"); !errors.Is(err, model.ErrSetupClosed) {
		t.Fatal("only the first account is created this way")
	}
	now := time.Unix(1790000000, 0)
	s.Save(ctx, "d1", id, now.Add(time.Hour))
	s.Save(ctx, "d2", id, now.Add(time.Hour))
	if who, ok := s.FindOwner(ctx, "d1", now); !ok || who.Username.String() != "kangmin" {
		t.Fatal("session owner")
	}
	if _, ok := s.FindOwner(ctx, "d1", now.Add(2*time.Hour)); ok {
		t.Fatal("expired sessions have no owner")
	}
	s.DeleteOthers(ctx, id, "d2")
	if _, ok := s.FindOwner(ctx, "d1", now); ok {
		t.Fatal("other sessions are gone")
	}
	if _, ok := s.FindOwner(ctx, "d2", now); !ok {
		t.Fatal("the kept session stays")
	}
	a.DeleteAll(ctx)
	if _, ok := s.FindOwner(ctx, "d2", now); ok {
		t.Fatal("sessions go with their account")
	}
}

// ── v1 ────────────────────────────────────

const v1Hash = "0123456789abcdef0123456789abcdef:6822e8648a09c289b3fb964753d46cc34c9b8fac18f3bd52e8513551b035ceb65985818b05aa03f43c1dd0e6d7b5cf496e9ba336bc9b599c4e76b57f03df2ba0"

func writeV1(t *testing.T, dir, file string, stmts ...string) {
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

func writeV1Accounts(t *testing.T, dir string) {
	writeV1(t, dir, "auth.db",
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at INTEGER)`,
		`INSERT INTO users (username, password_hash) VALUES ('kangmin', '`+v1Hash+`')`,
	)
	writeV1(t, dir, "proxy.db",
		`CREATE TABLE proxy_hosts (id INTEGER PRIMARY KEY AUTOINCREMENT, domain TEXT NOT NULL UNIQUE, target_scheme TEXT NOT NULL DEFAULT 'http', target_host TEXT, target_port INTEGER,
			ssl_enabled INTEGER NOT NULL DEFAULT 0, enabled INTEGER NOT NULL DEFAULT 1, description TEXT DEFAULT '')`,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, description) VALUES ('shelf.example.com', '127.0.0.1', 81, 'Shelf admin (auto-registered via ADMIN_DOMAIN)')`,
	)
}

func TestImportCarriesAccountsAndAdminDomain(t *testing.T) {
	dir := t.TempDir()
	writeV1Accounts(t, dir)
	db := open(t, dir)

	r, err := ImportV1(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Found || r.Users != 1 || r.AdminDomain != "shelf.example.com" {
		t.Fatalf("unexpected report %+v", r)
	}
	if _, h, err := NewAccounts(db).FindByName(ctx, "kangmin"); err != nil || h != v1Hash {
		t.Fatal("the v1 password hash is kept as is")
	}
	if v, _ := NewSettings(db).Get(ctx, model.SettingSetupDone); v != "1" {
		t.Fatal("a server that ran v1 skips the setup wizard")
	}
	again, err := ImportV1(ctx, db, dir)
	if err != nil || again.Users != 0 || again.Found {
		t.Fatalf("the import runs once: %+v %v", again, err)
	}
}

func TestImportLeavesV1Untouched(t *testing.T) {
	dir := t.TempDir()
	writeV1Accounts(t, dir)
	if _, err := ImportV1(ctx, open(t, dir), dir); err != nil {
		t.Fatal(err)
	}
	db, _ := sql.Open("sqlite", filepath.Join(dir, "auth.db"))
	defer db.Close()
	var tables int
	db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables)
	if tables != 1 {
		t.Fatalf("v1 auth.db must not gain tables, has %d", tables)
	}
}

func TestFreshInstallImportsNothing(t *testing.T) {
	dir := t.TempDir()
	db := open(t, dir)
	r, err := ImportV1(ctx, db, dir)
	if err != nil || r.Found || r.Users != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if n, _ := NewAccounts(db).Count(ctx); n != 0 {
		t.Fatal("a fresh install still needs setup")
	}
	if _, set := NewSettings(db).Get(ctx, model.SettingSetupDone); set {
		t.Fatal("setup is not marked done without accounts")
	}
}

func TestExistingAdminDomainWins(t *testing.T) {
	dir := t.TempDir()
	writeV1Accounts(t, dir)
	db := open(t, dir)
	NewSettings(db).Set(ctx, model.SettingAdminDomain, "naru.example.com")
	if _, err := ImportV1(ctx, db, dir); err != nil {
		t.Fatal(err)
	}
	if v, _ := NewSettings(db).Get(ctx, model.SettingAdminDomain); v != "naru.example.com" {
		t.Fatalf("an admin domain already set must win, got %q", v)
	}
}

const v1Projects = `CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, repo_url TEXT NOT NULL,
	branch TEXT NOT NULL DEFAULT 'main', port INTEGER, env TEXT DEFAULT '', domain TEXT DEFAULT '', webhook_secret TEXT NOT NULL,
	auto_deploy INTEGER NOT NULL DEFAULT 1, source_type TEXT NOT NULL DEFAULT 'git', image TEXT DEFAULT '', container_port INTEGER,
	volumes TEXT DEFAULT '', git_token TEXT DEFAULT '', build_path TEXT NOT NULL DEFAULT '')`

const v1Hosts = `CREATE TABLE proxy_hosts (id INTEGER PRIMARY KEY AUTOINCREMENT, domain TEXT NOT NULL UNIQUE, target_scheme TEXT NOT NULL DEFAULT 'http',
	target_host TEXT NOT NULL, target_port INTEGER NOT NULL, ssl_enabled INTEGER NOT NULL DEFAULT 0, force_ssl INTEGER NOT NULL DEFAULT 0,
	enabled INTEGER NOT NULL DEFAULT 1, description TEXT DEFAULT '', hsts_enabled INTEGER NOT NULL DEFAULT 0)`

func TestAppsAndHostsBecomeServices(t *testing.T) {
	dir := t.TempDir()
	writeV1(t, dir, "deploy.db", v1Projects,
		`INSERT INTO projects (id, name, repo_url, webhook_secret, container_port, build_path, env, git_token) VALUES (3, 'blog', 'https://github.com/me/blog', 'whsec', 3000, '', 'A=1', 'ghp_x')`,
		`INSERT INTO projects (id, name, repo_url, webhook_secret, source_type, image, container_port, auto_deploy) VALUES (6, 'api', '', 'whsec2', 'image', 'ghcr.io/me/api:latest', 8080, 0)`,
		`CREATE TABLE deployments (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL, commit_hash TEXT DEFAULT '', commit_message TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending', trigger_type TEXT NOT NULL DEFAULT 'manual', log TEXT DEFAULT '', duration_ms INTEGER DEFAULT 0, created_at INTEGER)`,
		`INSERT INTO deployments (project_id, commit_hash, commit_message, status, trigger_type, log, duration_ms, created_at) VALUES (3, 'abc1234', 'first', 'success', 'webhook', 'built', 38000, 1790000000)`,
		`INSERT INTO deployments (project_id, status, created_at) VALUES (3, 'failed', 1790000100)`,
		`INSERT INTO deployments (project_id, status, created_at) VALUES (99, 'success', 1790000200)`,
	)
	writeV1(t, dir, "proxy.db", v1Hosts,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, ssl_enabled, hsts_enabled, description) VALUES ('blog.example.com', 'shelf-blog', 3000, 1, 1, 'Auto-created by app blog')`,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, description) VALUES ('nas.example.com', '127.0.0.1', 5000, 'NAS')`,
		`INSERT INTO proxy_hosts (domain, target_scheme, target_host, target_port, description) VALUES ('router.example.com', 'https', '192.168.0.1', 443, '')`,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, enabled) VALUES ('off.example.com', '10.0.0.9', 80, 0)`,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, description) VALUES ('shelf.example.com', '127.0.0.1', 81, 'Shelf admin (auto-registered via ADMIN_DOMAIN)')`,
	)
	db := open(t, dir)
	r, err := ImportV1(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Apps != 2 || r.External != 2 || r.Domains != 3 || r.History != 2 {
		t.Fatalf("%+v", r)
	}

	repo := NewServices(db)
	blog, err := repo.Get(ctx, 3)
	if err != nil {
		t.Fatal("app ids are kept so webhook URLs survive:", err)
	}
	if blog.Kind != model.KindRepo || blog.Live.Alias != "shelf-blog" || blog.Port != 3000 || blog.Branch != "main" {
		t.Fatalf("%+v", blog)
	}
	if len(blog.Domains) != 1 || !blog.Domains[0].HTTPS || !blog.Domains[0].HSTS {
		t.Fatalf("the app's own host becomes its domain: %+v", blog.Domains)
	}
	if sec, _ := NewSecrets(db).Get(ctx, 3); sec.GitToken != "ghp_x" || sec.WebhookSecret != "whsec" || sec.Env.Text() != "A=1" {
		t.Fatalf("secrets come along: %+v", sec)
	}

	api, _ := repo.Get(ctx, 6)
	if api.Kind != model.KindImage || api.Source != "ghcr.io/me/api:latest" || api.AutoDeploy || api.Branch != "" {
		t.Fatalf("%+v", api)
	}

	all, _ := repo.List(ctx)
	external := map[string]string{}
	for _, s := range all {
		external[s.PrimaryDomain()] = s.External
	}
	if external["nas.example.com"] != "host.docker.internal:5000" {
		t.Fatalf("v1's 127.0.0.1 meant this server: %q", external["nas.example.com"])
	}
	if external["router.example.com"] != "https://192.168.0.1:443" {
		t.Fatalf("https upstreams keep their scheme: %q", external["router.example.com"])
	}
	if _, ok := external["shelf.example.com"]; ok {
		t.Fatal("the admin host is a setting, not a service")
	}
	if _, ok := external["off.example.com"]; ok {
		t.Fatal("disabled hosts are not imported")
	}
	if again, _ := ImportV1(ctx, db, dir); again.Apps != 0 {
		t.Fatal("runs once")
	}
}

// 아주 오래된 v1(빌드 경로·이미지 칸이 생기기 전)도 읽는다.
func TestOldV1SchemaStillImports(t *testing.T) {
	dir := t.TempDir()
	writeV1(t, dir, "deploy.db",
		`CREATE TABLE projects (id INTEGER PRIMARY KEY, name TEXT, repo_url TEXT, branch TEXT, port INTEGER, webhook_secret TEXT, auto_deploy INTEGER)`,
		`INSERT INTO projects VALUES (1, 'old', 'https://github.com/me/old', 'main', 4000, 's', 1)`,
	)
	db := open(t, dir)
	if r, err := ImportV1(ctx, db, dir); err != nil || r.Apps != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	old, _ := NewServices(db).Get(ctx, 1)
	if old.Port != 4000 || old.Kind != model.KindRepo {
		t.Fatalf("%+v", old)
	}
}

func TestNameCollisionsGetASuffix(t *testing.T) {
	dir := t.TempDir()
	writeV1(t, dir, "deploy.db", v1Projects,
		`INSERT INTO projects (id, name, repo_url, webhook_secret, container_port) VALUES (1, 'nas', 'https://github.com/me/nas', 's', 80)`)
	writeV1(t, dir, "proxy.db", v1Hosts,
		`INSERT INTO proxy_hosts (domain, target_host, target_port) VALUES ('nas.example.com', '192.168.0.20', 5000)`)
	db := open(t, dir)
	if _, err := ImportV1(ctx, db, dir); err != nil {
		t.Fatal(err)
	}
	all, _ := NewServices(db).List(ctx)
	if len(all) != 2 || all[1].Name.String() != "nas-2" {
		t.Fatalf("%+v", all)
	}
}

func TestWebSettingsStorage(t *testing.T) {
	db := open(t, t.TempDir())
	id, _ := NewServices(db).Create(ctx, model.NewService{Name: name("shop"), Kind: model.KindImage})
	w := NewWebSettings(db)
	if got, err := w.Get(ctx, id); err != nil || got.Maintenance || len(got.Headers) != 0 {
		t.Fatalf("no settings yet is the empty setting: %+v %v", got, err)
	}
	prefix, _ := model.ParsePathPrefix("/api")
	in := model.WebSettings{Maintenance: true, Headers: []model.HeaderRule{{Name: "X", Value: "1"}},
		Login: model.BasicLogin{User: "me", Hash: "$2a$10$abc"}, Paths: []model.PathRoute{{Prefix: prefix, Service: 2}}}
	if err := w.Set(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	in.Maintenance = false
	w.Set(ctx, id, in)
	got, _ := w.Get(ctx, id)
	if got.Maintenance || got.Login.Hash != "$2a$10$abc" || got.Paths[0].Prefix.String() != "/api" {
		t.Fatalf("%+v", got)
	}
	NewServices(db).Delete(ctx, id)
	if got, _ := w.Get(ctx, id); got.Login.User != "" {
		t.Fatal("settings go with their service")
	}
}
