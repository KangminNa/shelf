package auth

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/store"
)

// v1(Node)이 만든 해시. crypto.scryptSync('correct horse battery', salt, 64)
const v1Hash = "0123456789abcdef0123456789abcdef:6822e8648a09c289b3fb964753d46cc34c9b8fac18f3bd52e8513551b035ceb65985818b05aa03f43c1dd0e6d7b5cf496e9ba336bc9b599c4e76b57f03df2ba0"

func newService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := New(st.DB)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestV1HashVerifies(t *testing.T) {
	if !VerifyPassword("correct horse battery", v1Hash) {
		t.Fatal("a password hashed by v1 must still verify")
	}
	if VerifyPassword("wrong", v1Hash) {
		t.Fatal("a wrong password must not verify")
	}
	for _, broken := range []string{"", "nocolon", "salt:", ":abcd", "salt:zz"} {
		if VerifyPassword("x", broken) {
			t.Fatalf("malformed hash %q must not verify", broken)
		}
	}
}

func TestHashRoundTrip(t *testing.T) {
	h, err := HashPassword("hunter22hunter")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("hunter22hunter", h) || VerifyPassword("hunter22", h) {
		t.Fatal("round trip failed")
	}
}

func TestFirstAccountNeedsTheSetupToken(t *testing.T) {
	s := newService(t)
	if !s.NeedsSetup() {
		t.Fatal("a fresh install needs setup")
	}
	if _, err := s.CreateFirstAccount("wrong", "admin", "longenough"); err != ErrBadSetupToken {
		t.Fatalf("want ErrBadSetupToken, got %v", err)
	}
	if _, err := s.CreateFirstAccount("", "admin", "longenough"); err != ErrBadSetupToken {
		t.Fatalf("an empty token must be refused, got %v", err)
	}
	if _, err := s.CreateFirstAccount(s.SetupToken(), "a", "longenough"); err != ErrInvalidUsername {
		t.Fatalf("want ErrInvalidUsername, got %v", err)
	}
	if _, err := s.CreateFirstAccount(s.SetupToken(), "admin", "short"); err != ErrWeakPassword {
		t.Fatalf("want ErrWeakPassword, got %v", err)
	}
	u, err := s.CreateFirstAccount(s.SetupToken(), "admin", "longenough")
	if err != nil || u.Username != "admin" {
		t.Fatalf("create: %v", err)
	}
	if s.NeedsSetup() || s.CheckSetupToken(s.SetupToken()) {
		t.Fatal("once an account exists the token is dead")
	}
	if _, err := s.CreateFirstAccount(s.SetupToken(), "second", "longenough"); err != ErrSetupClosed {
		t.Fatalf("a second account must be refused, got %v", err)
	}
}

func TestOnlyOneOfConcurrentSetupsWins(t *testing.T) {
	s := newService(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.CreateFirstAccount(s.SetupToken(), "admin"+string(rune('a'+i)), "longenough"); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("exactly one setup must win, got %d", wins)
	}
}

func TestLoginLocksAfterRepeatedFailures(t *testing.T) {
	s := newService(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	s.CreateFirstAccount(s.SetupToken(), "admin", "longenough")

	for i := 0; i < MaxLoginFailures; i++ {
		if _, err := s.Login("198.51.100.7", "admin", "nope"); err != ErrBadCredentials {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := s.Login("198.51.100.7", "admin", "longenough"); err != ErrLocked {
		t.Fatalf("the right password is refused while locked, got %v", err)
	}
	if _, err := s.Login("203.0.113.1", "admin", "longenough"); err != nil {
		t.Fatalf("other addresses are not locked: %v", err)
	}
	now = now.Add(LockoutDuration + time.Second)
	if _, err := s.Login("198.51.100.7", "admin", "longenough"); err != nil {
		t.Fatalf("the lock lifts after the lockout: %v", err)
	}
}

func TestSessionsExpireAndDieWithTheirUser(t *testing.T) {
	s := newService(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	u, _ := s.CreateFirstAccount(s.SetupToken(), "admin", "longenough")
	token, err := s.NewSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := s.UserFor(token); !ok || got.Username != "admin" {
		t.Fatal("a fresh session resolves to its user")
	}
	var stored string
	s.db.QueryRow(`SELECT token_hash FROM sessions`).Scan(&stored)
	if stored == token {
		t.Fatal("the raw token must never be stored")
	}
	now = now.Add(SessionTTL + time.Second)
	if _, ok := s.UserFor(token); ok {
		t.Fatal("an expired session must not resolve")
	}

	now = time.Unix(1_800_000_000, 0)
	token, _ = s.NewSession(u.ID)
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.UserFor(token); ok {
		t.Fatal("a session whose user is gone must not resolve")
	}
}

func TestChangePasswordKeepsOnlyTheCurrentSession(t *testing.T) {
	s := newService(t)
	u, _ := s.CreateFirstAccount(s.SetupToken(), "admin", "longenough")
	here, _ := s.NewSession(u.ID)
	elsewhere, _ := s.NewSession(u.ID)

	if err := s.ChangePassword(u.ID, "wrong-current", "brand-new-pass", here); err != ErrBadCredentials {
		t.Fatalf("the current password is checked, got %v", err)
	}
	if err := s.ChangePassword(u.ID, "longenough", "short", here); err != ErrWeakPassword {
		t.Fatalf("the new password is checked, got %v", err)
	}
	if err := s.ChangePassword(u.ID, "longenough", "brand-new-pass", here); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.UserFor(here); !ok {
		t.Fatal("the device that changed the password stays signed in")
	}
	if _, ok := s.UserFor(elsewhere); ok {
		t.Fatal("every other device is signed out")
	}
	if _, err := s.Login("ip", "admin", "brand-new-pass"); err != nil {
		t.Fatal("the new password works")
	}
}

func TestImportedV1AccountCanSignIn(t *testing.T) {
	s := newService(t)
	added, err := s.ImportUser("kangmin", v1Hash)
	if err != nil || !added {
		t.Fatalf("import: %v", err)
	}
	if again, _ := s.ImportUser("kangmin", "other:hash"); again {
		t.Fatal("importing twice must not overwrite")
	}
	if _, err := s.Login("ip", "kangmin", "correct horse battery"); err != nil {
		t.Fatalf("v1 password must work in v2: %v", err)
	}
	if s.NeedsSetup() {
		t.Fatal("an imported account closes setup")
	}
}

func TestShellRecovery(t *testing.T) {
	s := newService(t)
	u, _ := s.CreateFirstAccount(s.SetupToken(), "admin", "longenough")
	token, _ := s.NewSession(u.ID)
	if err := s.SetPassword("nobody", "longenough"); err != ErrNoSuchUser {
		t.Fatalf("want ErrNoSuchUser, got %v", err)
	}
	if err := s.SetPassword("admin", "recovered-pass"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.UserFor(token); ok {
		t.Fatal("recovery signs every device out")
	}
	if _, err := s.Login("ip", "admin", "recovered-pass"); err != nil {
		t.Fatal(err)
	}
}
