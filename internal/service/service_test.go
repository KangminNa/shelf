package service

import (
	"path/filepath"
	"testing"

	"github.com/KangminNa/naru/internal/store"
)

func newRepo(t *testing.T) *Repo {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "naru.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewRepo(st.DB)
}

func TestCreateKeepsAnExplicitID(t *testing.T) {
	r := newRepo(t)
	id, err := r.Create(Service{ID: 6, Name: "landing", Kind: KindRepo, Container: "shelf-landing", Port: 4023}, Secrets{WebhookSecret: "s3cret"})
	if err != nil || id != 6 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	next, _ := r.Create(Service{Name: "nas", Kind: KindExternal, Upstream: "host.docker.internal:5000"}, Secrets{})
	if next != 7 {
		t.Fatalf("later services continue after the kept id, got %d", next)
	}
	if _, err := r.Create(Service{Name: "landing", Kind: KindImage}, Secrets{}); err == nil {
		t.Fatal("names are unique")
	}
	if _, err := r.Create(Service{Name: "bad", Kind: "nonsense"}, Secrets{}); err == nil {
		t.Fatal("unknown kinds are refused by the schema")
	}
}

func TestListCarriesDomainsInOrder(t *testing.T) {
	r := newRepo(t)
	blog, _ := r.Create(Service{Name: "blog", Kind: KindRepo, Container: "shelf-blog", Port: 3000}, Secrets{})
	r.Create(Service{Name: "api", Kind: KindImage, Container: "naru-api", Port: 8080}, Secrets{})
	if err := r.AddDomain(blog, Domain{Domain: "Blog.Example.com", HTTPS: true}); err != nil {
		t.Fatal(err)
	}
	r.AddDomain(blog, Domain{Domain: "www.blog.example.com", HTTPS: true, HSTS: true})
	if err := r.AddDomain(blog, Domain{Domain: "blog.example.com"}); err == nil {
		t.Fatal("a domain belongs to one service")
	}

	all, err := r.List()
	if err != nil || len(all) != 2 {
		t.Fatalf("%v %v", all, err)
	}
	if all[0].Name != "api" || len(all[0].Domains) != 0 {
		t.Fatal("sorted by name; api has no domains")
	}
	b := all[1]
	if b.PrimaryDomain() != "blog.example.com" || len(b.Domains) != 2 || !b.Domains[1].HSTS {
		t.Fatalf("%+v", b.Domains)
	}
	if b.Target() != "shelf-blog:3000" {
		t.Fatalf("target %q", b.Target())
	}
	if !r.DomainTaken("BLOG.example.com") || r.DomainTaken("other.example.com") {
		t.Fatal("DomainTaken is case-insensitive")
	}
}

func TestTargets(t *testing.T) {
	cases := []struct {
		s    Service
		want string
	}{
		{Service{Kind: KindImage, Container: "naru-api", Port: 8080}, "naru-api:8080"},
		{Service{Kind: KindRepo, Container: "shelf-x"}, ""},
		{Service{Kind: KindExternal, Upstream: "host.docker.internal:5000"}, "host.docker.internal:5000"},
		{Service{Kind: KindStatic}, ""},
	}
	for _, c := range cases {
		if got := c.s.Target(); got != c.want {
			t.Errorf("%+v → %q, want %q", c.s, got, c.want)
		}
	}
}

func TestGetUnknown(t *testing.T) {
	if _, err := newRepo(t).Get(42); err != ErrNotFound {
		t.Fatalf("got %v", err)
	}
}
