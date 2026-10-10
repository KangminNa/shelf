package websettings

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/KangminNa/naru/internal/model"
	"golang.org/x/crypto/bcrypt"
)

var ctx = context.Background()

type fakeServices struct{ all []model.Service }

func (f fakeServices) List(context.Context) ([]model.Service, error) { return f.all, nil }
func (f fakeServices) Get(_ context.Context, id model.ServiceID) (model.Service, error) {
	for _, s := range f.all {
		if s.ID == id {
			return s, nil
		}
	}
	return model.Service{}, model.ErrNotFound
}
func (f fakeServices) NameTaken(_ context.Context, n model.ServiceName) bool {
	for _, s := range f.all {
		if s.Name == n {
			return true
		}
	}
	return false
}

type memStore struct {
	m map[model.ServiceID]model.WebSettings
}

func (s *memStore) Get(_ context.Context, id model.ServiceID) (model.WebSettings, error) {
	return s.m[id], nil
}
func (s *memStore) Set(_ context.Context, id model.ServiceID, w model.WebSettings) error {
	s.m[id] = w
	return nil
}

type fakeCompiler struct {
	calls   int
	ignored []string
	err     error
}

func (c *fakeCompiler) Compile(_ context.Context, text string) ([]byte, []string, error) {
	c.calls++
	return []byte(`[{"compiled":"` + text + `"}]`), c.ignored, c.err
}

type fakeSync struct {
	calls  int
	refuse func(call int) error
}

func (s *fakeSync) SyncNow(context.Context) error {
	s.calls++
	if s.refuse != nil {
		return s.refuse(s.calls)
	}
	return nil
}
func (s *fakeSync) Status() model.WebServerStatus { return model.WebServerStatus{} }

func name(s string) model.ServiceName { n, _ := model.ParseServiceName(s); return n }

func newEditor() (*memStore, *fakeCompiler, *fakeSync, func(model.ServiceID, model.WebSettingsInput) ([]string, error)) {
	store := &memStore{m: map[model.ServiceID]model.WebSettings{}}
	compiler := &fakeCompiler{}
	sync := &fakeSync{}
	e := NewWebSettingsEditor(EditorParts{
		Services: fakeServices{all: []model.Service{{ID: 1, Name: name("blog")}, {ID: 2, Name: name("api")}}},
		Store:    store, Compiler: compiler, Hasher: BcryptHasher{Cost: bcrypt.MinCost}, Sync: sync,
	})
	return store, compiler, sync, func(id model.ServiceID, in model.WebSettingsInput) ([]string, error) {
		return e.Apply(ctx, id, in)
	}
}

func prefix(s string) model.PathPrefix { p, _ := model.ParsePathPrefix(s); return p }

func inputCode(err error) string {
	if ie, ok := model.AsInputError(err); ok {
		return ie.Code
	}
	return ""
}

func TestApplySavesAndSyncs(t *testing.T) {
	store, compiler, sync, apply := newEditor()
	compiler.ignored = []string{"tls internal"}
	warnings, err := apply(1, model.WebSettingsInput{
		Headers:   []model.HeaderRule{{Name: "X-A", Value: "1"}},
		AllowFrom: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		Paths: []model.PathRouteInput{
			{Prefix: prefix("/api"), Target: "api", StripPrefix: true},
			{Prefix: prefix("/nas"), Target: "localhost:5000"},
		},
		Advanced: "respond /health 200",
	})
	if err != nil || len(warnings) != 1 || sync.calls != 1 {
		t.Fatalf("%v %v sync=%d", warnings, err, sync.calls)
	}
	got := store.m[1]
	if got.Paths[0].Service != 2 || !got.Paths[0].StripPrefix || got.Paths[1].External.String() != "host.docker.internal:5000" {
		t.Fatalf("service names become service numbers, the rest addresses: %+v", got.Paths)
	}
	if got.Advanced != "respond /health 200" || len(got.Compiled) == 0 || len(got.Headers) != 1 || len(got.AllowFrom) != 1 {
		t.Fatalf("%+v", got)
	}

	compiler.calls = 0
	apply(1, model.WebSettingsInput{})
	if compiler.calls != 0 || store.m[1].Compiled != nil {
		t.Fatal("an empty advanced box compiles nothing")
	}
}

func TestApplyRefusesBadPaths(t *testing.T) {
	_, _, sync, apply := newEditor()
	cases := map[string]string{"nope": "pathtarget", "blog": "pathself"}
	for target, code := range cases {
		_, err := apply(1, model.WebSettingsInput{Paths: []model.PathRouteInput{{Prefix: prefix("/x"), Target: target}}})
		if inputCode(err) != code {
			t.Errorf("%s: %v, want %s", target, err, code)
		}
	}
	if sync.calls != 0 {
		t.Fatal("refused input never reaches the web server")
	}
	if _, err := apply(99, model.WebSettingsInput{}); !errors.Is(err, model.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPasswordsAreHashedAndKept(t *testing.T) {
	store, _, _, apply := newEditor()
	if _, err := apply(1, model.WebSettingsInput{LoginUser: "me"}); inputCode(err) != "weakpassword" {
		t.Fatalf("a new login needs a password: %v", err)
	}
	if _, err := apply(1, model.WebSettingsInput{LoginUser: "me", Password: "short"}); inputCode(err) != "weakpassword" {
		t.Fatalf("passwords need 8 characters: %v", err)
	}
	if _, err := apply(1, model.WebSettingsInput{LoginUser: "me", Password: "longenough"}); err != nil {
		t.Fatal(err)
	}
	first := store.m[1].Login
	if first.User != "me" || bcrypt.CompareHashAndPassword([]byte(first.Hash), []byte("longenough")) != nil {
		t.Fatalf("stored as bcrypt: %+v", first)
	}
	apply(1, model.WebSettingsInput{LoginUser: "me", Headers: []model.HeaderRule{{Name: "X", Value: "y"}}})
	if store.m[1].Login != first {
		t.Fatal("an empty password keeps the current one")
	}
	apply(1, model.WebSettingsInput{LoginUser: "me", RemoveLogin: true})
	if store.m[1].Login != (model.BasicLogin{}) {
		t.Fatal("protection removed")
	}
}

func TestARefusedConfigIsRolledBack(t *testing.T) {
	store, _, sync, apply := newEditor()
	apply(1, model.WebSettingsInput{Headers: []model.HeaderRule{{Name: "X-Old", Value: "1"}}})
	before := store.m[1]

	sync.refuse = func(call int) error {
		if call == 2 { // 새 설정 — 거절. 되돌린 뒤(3번째)는 받아들인다
			return errors.New(`caddy: POST /load: 400 {"error":"loading new config: bad directive"}`)
		}
		return nil
	}
	_, err := apply(1, model.WebSettingsInput{Advanced: "nonsense {"})
	var refused model.RefusedError
	if !errors.As(err, &refused) || refused.Reason == "" {
		t.Fatalf("the web server's reason comes back: %v", err)
	}
	if sync.calls != 3 || store.m[1].Headers[0].Name != "X-Old" || store.m[1].Advanced != "" {
		t.Fatalf("the old settings are restored and synced again: calls=%d %+v (before %+v)", sync.calls, store.m[1], before)
	}
}

func TestCompileErrorsAreShownWithoutTouchingAnything(t *testing.T) {
	store, compiler, sync, apply := newEditor()
	compiler.err = errors.New("Caddyfile:2: unrecognized directive: nonsense")
	_, err := apply(1, model.WebSettingsInput{Advanced: "nonsense"})
	var refused model.RefusedError
	if !errors.As(err, &refused) || sync.calls != 0 || len(store.m) != 0 {
		t.Fatalf("%v sync=%d", err, sync.calls)
	}
}
