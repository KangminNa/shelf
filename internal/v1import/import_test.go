package v1import

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/KangminNa/naru/internal/auth"
	"github.com/KangminNa/naru/internal/store"

	_ "modernc.org/sqlite"
)

const v1Hash = "0123456789abcdef0123456789abcdef:6822e8648a09c289b3fb964753d46cc34c9b8fac18f3bd52e8513551b035ceb65985818b05aa03f43c1dd0e6d7b5cf496e9ba336bc9b599c4e76b57f03df2ba0"

// writeV1는 v1과 같은 모양의 DB 파일을 만든다.
func writeV1(t *testing.T, dir string) {
	t.Helper()
	exec := func(file string, stmts ...string) {
		db, err := sql.Open("sqlite", filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		for _, s := range stmts {
			if _, err := db.Exec(s); err != nil {
				t.Fatal(err)
			}
		}
	}
	exec("auth.db",
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at INTEGER)`,
		`INSERT INTO users (username, password_hash) VALUES ('kangmin', '`+v1Hash+`')`,
	)
	exec("proxy.db",
		`CREATE TABLE proxy_hosts (id INTEGER PRIMARY KEY AUTOINCREMENT, domain TEXT NOT NULL UNIQUE, target_host TEXT, target_port INTEGER, description TEXT DEFAULT '')`,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, description) VALUES ('blog.example.com', 'shelf-blog', 3000, 'Auto-created by app blog')`,
		`INSERT INTO proxy_hosts (domain, target_host, target_port, description) VALUES ('shelf.example.com', '127.0.0.1', 81, 'Shelf admin (auto-registered via ADMIN_DOMAIN)')`,
	)
}

func setup(t *testing.T, dir string) (*store.Store, *auth.Service) {
	t.Helper()
	st, err := store.Open(filepath.Join(dir, "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a, err := auth.New(st.DB)
	if err != nil {
		t.Fatal(err)
	}
	return st, a
}

func TestImportCarriesAccountsAndAdminDomain(t *testing.T) {
	dir := t.TempDir()
	writeV1(t, dir)
	st, a := setup(t, dir)

	r, err := Run(dir, st, a)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Found || r.Users != 1 || r.AdminDomain != "shelf.example.com" {
		t.Fatalf("unexpected report %+v", r)
	}
	if _, err := a.Login("ip", "kangmin", "correct horse battery"); err != nil {
		t.Fatalf("the v1 password must work after import: %v", err)
	}
	if v, _ := st.Setting(store.KeySetupDone); v != "1" {
		t.Fatal("a server that ran v1 skips the setup wizard")
	}

	again, err := Run(dir, st, a)
	if err != nil || !again.Skipped {
		t.Fatalf("the import runs once: %+v %v", again, err)
	}
}

func TestImportLeavesV1Untouched(t *testing.T) {
	dir := t.TempDir()
	writeV1(t, dir)
	st, a := setup(t, dir)
	if _, err := Run(dir, st, a); err != nil {
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
	st, a := setup(t, dir)
	r, err := Run(dir, st, a)
	if err != nil || r.Found || r.Users != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if !a.NeedsSetup() {
		t.Fatal("a fresh install still needs setup")
	}
	if _, set := st.Setting(store.KeySetupDone); set {
		t.Fatal("setup is not marked done without accounts")
	}
}

func TestExistingAdminDomainWins(t *testing.T) {
	dir := t.TempDir()
	writeV1(t, dir)
	st, a := setup(t, dir)
	st.SetSetting(store.KeyAdminDomain, "naru.example.com")
	if _, err := Run(dir, st, a); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.Setting(store.KeyAdminDomain); v != "naru.example.com" {
		t.Fatalf("an admin domain already set must win, got %q", v)
	}
}
