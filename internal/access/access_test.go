package access

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
	"github.com/KangminNa/naru/internal/store"
	"github.com/KangminNa/naru/internal/system"
)

var ctx = context.Background()

// v1(Node)이 만든 해시. crypto.scryptSync('correct horse battery', salt, 64)
const v1Hash = "0123456789abcdef0123456789abcdef:6822e8648a09c289b3fb964753d46cc34c9b8fac18f3bd52e8513551b035ceb65985818b05aa03f43c1dd0e6d7b5cf496e9ba336bc9b599c4e76b57f03df2ba0"

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type rig struct {
	db       *store.DB
	key      RandomSetupKey
	login    contract.LoginManager
	accounts contract.AccountManager
	clock    *fakeClock
}

func newRig(t *testing.T) rig {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	clock := &fakeClock{time.Unix(1_800_000_000, 0)}
	acc, sessions := store.NewAccounts(db), store.NewSessions(db)
	key := NewRandomSetupKey(acc, system.Random{})
	return rig{
		db:       db,
		key:      key,
		clock:    clock,
		login:    NewLoginManager(acc, sessions, ScryptHasher{}, NewMemoryLoginLimiter(clock), system.Random{}, clock),
		accounts: NewAccountManager(acc, acc, sessions, ScryptHasher{}, key, system.Random{}, clock),
	}
}

func user(s string) model.Username {
	u, err := model.ParseUsername(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestV1HashVerifies(t *testing.T) {
	h := ScryptHasher{}
	if !h.Matches("correct horse battery", v1Hash) {
		t.Fatal("a password hashed by v1 must still verify")
	}
	if h.Matches("wrong", v1Hash) {
		t.Fatal("a wrong password must not verify")
	}
	for _, broken := range []model.PasswordHash{"", "nocolon", "salt:", ":abcd", "salt:zz"} {
		if h.Matches("x", broken) {
			t.Fatalf("malformed hash %q must not verify", broken)
		}
	}
}

func TestHashRoundTrip(t *testing.T) {
	h, err := ScryptHasher{}.Hash("hunter22hunter")
	if err != nil {
		t.Fatal(err)
	}
	if !(ScryptHasher{}).Matches("hunter22hunter", h) || (ScryptHasher{}).Matches("hunter22", h) {
		t.Fatal("round trip failed")
	}
}

func TestFirstAccountNeedsTheSetupKey(t *testing.T) {
	r := newRig(t)
	if _, err := r.accounts.CreateFirst(ctx, "wrong", user("admin"), "longenough"); !errors.Is(err, model.ErrBadSetupKey) {
		t.Fatalf("want ErrBadSetupKey, got %v", err)
	}
	if _, err := r.accounts.CreateFirst(ctx, "", user("admin"), "longenough"); !errors.Is(err, model.ErrBadSetupKey) {
		t.Fatalf("an empty key must be refused, got %v", err)
	}
	if _, err := r.accounts.CreateFirst(ctx, r.key.Value(), user("admin"), "short"); !errors.Is(err, model.ErrWeakPassword) {
		t.Fatalf("want ErrWeakPassword, got %v", err)
	}
	token, err := r.accounts.CreateFirst(ctx, r.key.Value(), user("admin"), "longenough")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if who, ok := r.login.WhoIs(ctx, token); !ok || who.Username.String() != "admin" {
		t.Fatal("the first account is signed in right away")
	}
	if r.key.Matches(ctx, r.key.Value()) {
		t.Fatal("once an account exists the key is dead")
	}
	if _, err := r.accounts.CreateFirst(ctx, r.key.Value(), user("second"), "longenough"); !errors.Is(err, model.ErrSetupClosed) {
		t.Fatalf("a second account must be refused, got %v", err)
	}
}

func TestOnlyOneOfConcurrentSetupsWins(t *testing.T) {
	r := newRig(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := r.accounts.CreateFirst(ctx, r.key.Value(), user("admin"+string(rune('a'+i))), "longenough"); err == nil {
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
	r := newRig(t)
	r.accounts.CreateFirst(ctx, r.key.Value(), user("admin"), "longenough")
	for i := 0; i < MaxLoginFailures; i++ {
		if _, err := r.login.LogIn(ctx, "198.51.100.7", "admin", "nope"); !errors.Is(err, model.ErrBadLogin) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := r.login.LogIn(ctx, "198.51.100.7", "admin", "longenough"); !errors.Is(err, model.ErrLocked) {
		t.Fatalf("the right password is refused while locked, got %v", err)
	}
	if _, err := r.login.LogIn(ctx, "203.0.113.1", "admin", "longenough"); err != nil {
		t.Fatalf("other addresses are not locked: %v", err)
	}
	r.clock.now = r.clock.now.Add(LockoutDuration + time.Second)
	if _, err := r.login.LogIn(ctx, "198.51.100.7", "admin", "longenough"); err != nil {
		t.Fatalf("the lock lifts after the lockout: %v", err)
	}
}

func TestSessionsExpireAndDieWithTheirUser(t *testing.T) {
	r := newRig(t)
	start := r.clock.now
	token, _ := r.accounts.CreateFirst(ctx, r.key.Value(), user("admin"), "longenough")
	if who, ok := r.login.WhoIs(ctx, token); !ok || who.Username.String() != "admin" {
		t.Fatal("a fresh session resolves to its user")
	}
	r.clock.now = start.Add(SessionTTL + time.Second)
	if _, ok := r.login.WhoIs(ctx, token); ok {
		t.Fatal("an expired session must not resolve")
	}
	r.clock.now = start
	token, _ = r.login.LogIn(ctx, "ip", "admin", "longenough")
	if err := r.accounts.ResetAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.login.WhoIs(ctx, token); ok {
		t.Fatal("a session whose user is gone must not resolve")
	}
	r.login.LogOut(ctx, token)
}

func TestChangePasswordKeepsOnlyTheCurrentSession(t *testing.T) {
	r := newRig(t)
	here, _ := r.accounts.CreateFirst(ctx, r.key.Value(), user("admin"), "longenough")
	elsewhere, _ := r.login.LogIn(ctx, "ip", "admin", "longenough")
	who, _ := r.login.WhoIs(ctx, here)

	if err := r.accounts.ChangePassword(ctx, who, "wrong-current", "brand-new-pass", here); !errors.Is(err, model.ErrWrongPassword) {
		t.Fatalf("the current password is checked, got %v", err)
	}
	if err := r.accounts.ChangePassword(ctx, who, "longenough", "short", here); !errors.Is(err, model.ErrWeakPassword) {
		t.Fatalf("the new password is checked, got %v", err)
	}
	if err := r.accounts.ChangePassword(ctx, who, "longenough", "brand-new-pass", here); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.login.WhoIs(ctx, here); !ok {
		t.Fatal("the device that changed the password stays signed in")
	}
	if _, ok := r.login.WhoIs(ctx, elsewhere); ok {
		t.Fatal("every other device is signed out")
	}
	if _, err := r.login.LogIn(ctx, "ip", "admin", "brand-new-pass"); err != nil {
		t.Fatal("the new password works")
	}
}

func TestImportedV1AccountCanSignIn(t *testing.T) {
	r := newRig(t)
	if _, err := store.NewAccounts(r.db).CreateFirst(ctx, user("kangmin"), v1Hash); err != nil {
		t.Fatal(err)
	}
	if _, err := r.login.LogIn(ctx, "ip", "kangmin", "correct horse battery"); err != nil {
		t.Fatalf("v1 password must work in v2: %v", err)
	}
	if r.key.Matches(ctx, r.key.Value()) {
		t.Fatal("an imported account closes setup")
	}
}

func TestShellRecovery(t *testing.T) {
	r := newRig(t)
	token, _ := r.accounts.CreateFirst(ctx, r.key.Value(), user("admin"), "longenough")
	if err := r.accounts.Recover(ctx, "nobody", "longenough"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := r.accounts.Recover(ctx, "admin", "recovered-pass"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.login.WhoIs(ctx, token); ok {
		t.Fatal("recovery signs every device out")
	}
	if _, err := r.login.LogIn(ctx, "ip", "admin", "recovered-pass"); err != nil {
		t.Fatal(err)
	}
	if names, _ := r.accounts.Names(ctx); len(names) != 1 || names[0] != "admin" {
		t.Fatal(names)
	}
}
