package model

import (
	"testing"
	"time"
)

func TestDomainNames(t *testing.T) {
	cases := map[string]string{
		" HTTPS://Naru.Example.com/admin ": "naru.example.com",
		"naru.example.com.":                "naru.example.com",
		"http://a.b.co":                    "a.b.co",
	}
	for in, want := range cases {
		if got, err := ParseDomainName(in); err != nil || got.String() != want {
			t.Errorf("ParseDomainName(%q) = %q %v, want %q", in, got, err, want)
		}
	}
	for _, ok := range []string{"naru.example.com", "a.co", "my-app.nakangmin.duckdns.org"} {
		if _, err := ParseDomainName(ok); err != nil {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "localhost", "198.51.100.24", "-a.example.com", "a_b.example.com", "naru.example.com:8080", "a..b.com"} {
		if _, err := ParseDomainName(bad); err == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if d, err := ParseOptionalDomainName("  "); err != nil || !d.IsZero() {
		t.Fatal("an optional domain may be empty")
	}
	if d, _ := ParseDomainName("blog.example.com"); d.FirstLabel() != "blog" {
		t.Fatal(d.FirstLabel())
	}
}

func TestRepoURLsAndBranches(t *testing.T) {
	for _, ok := range []string{"https://github.com/me/blog", "https://github.com/me/blog.git", "http://git.lan/x"} {
		if _, err := ParseRepoURL(ok); err != nil {
			t.Errorf("%s should pass", ok)
		}
	}
	for _, bad := range []string{"", "-uhttps://x", "file:///etc", "ssh://git@github.com/x", "https://user:pw@github.com/x", "https://github.com/a b", "ext::sh -c id"} {
		if _, err := ParseRepoURL(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	for _, bad := range []string{"-x", "--upload-pack=x", "a..b", "feat x"} {
		if _, err := ParseBranch(bad); err == nil {
			t.Errorf("branch %q should be refused", bad)
		}
	}
	if b, err := ParseBranch("feature/new-ui"); err != nil || b.String() != "feature/new-ui" {
		t.Error("slashes in branches are normal")
	}
	if b, err := ParseBranch(""); err != nil || !b.IsZero() {
		t.Error("an empty branch means the default")
	}
	if r, _ := ParseRepoURL("https://github.com/me/Blog.git"); r.Name() != "Blog" {
		t.Error(r.Name())
	}
}

func TestSuggestedNames(t *testing.T) {
	img, _ := ParseImageRef("ghcr.io/me/api:latest")
	ext, _ := ParseExternalAddress("https://router.lan:443")
	repo, _ := ParseRepoURL("https://github.com/me/Blog.git")
	cases := map[string]string{
		"blog":    SuggestServiceName(repo.Name()).String(),
		"api":     SuggestServiceName(img.Name()).String(),
		"router":  SuggestServiceName(ext.Host()).String(),
		"service": SuggestServiceName("!!!").String(),
	}
	for want, got := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	long, _ := ParseServiceName("abcdefghijklmnopqrstuvwxyz01234")
	if s := long.WithSuffix(12).String(); len(s) > 31 || s[len(s)-3:] != "-12" {
		t.Errorf("suffixes keep names within 31: %q", s)
	}
}

func TestExternalAddresses(t *testing.T) {
	cases := map[string]string{
		"localhost:5000":         "host.docker.internal:5000",
		"127.0.0.1:81":           "host.docker.internal:81",
		"https://router.lan:443": "https://router.lan:443",
		"http://192.168.0.20:80": "192.168.0.20:80",
	}
	for in, want := range cases {
		if got, err := ParseExternalAddress(in); err != nil || got.String() != want {
			t.Errorf("%q → %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"nope", "x:0", "x:99999", "a b:80"} {
		if _, err := ParseExternalAddress(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestFolderPaths(t *testing.T) {
	for in, want := range map[string]string{"/site/dist/": "site/dist", "": "", "a\\b": "a/b"} {
		if got, err := ParseFolderPath(in); err != nil || got.String() != want {
			t.Errorf("%q → %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"../etc", "site/../../x", ".."} {
		if _, err := ParseFolderPath(bad); err == nil {
			t.Errorf("%q must not leave the repository", bad)
		}
	}
}

func TestKindNames(t *testing.T) {
	for _, k := range AllKinds() {
		if got, err := ParseKindName(string(k)); err != nil || got != k {
			t.Error(k)
		}
	}
	if _, err := ParseKindName("nonsense"); err == nil {
		t.Fatal("unknown kinds are refused")
	}
}

func TestLiveDeployment(t *testing.T) {
	cases := []struct {
		l    LiveState
		want DeploymentID
	}{
		{LiveState{Alias: "naru-app", Instance: "naru-app-12"}, 12},
		{LiveState{Alias: "shelf-blog"}, 0},
		{LiveState{Release: "7"}, 7},
	}
	for _, c := range cases {
		if got := c.l.LiveDeployment(); got != c.want {
			t.Errorf("%+v → %d", c.l, got)
		}
	}
}

func TestCertificatesCoverDomains(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	certs := Certificates{
		{Names: []string{"blog.example.com"}, Issuer: "Let's Encrypt", NotBefore: now.Add(-day), NotAfter: now.Add(60 * day)},
		{Names: []string{"blog.example.com"}, Issuer: "ZeroSSL", NotBefore: now.Add(-day), NotAfter: now.Add(80 * day)},
		{Names: []string{"*.apps.example.com"}, Issuer: "Let's Encrypt", NotBefore: now.Add(-day), NotAfter: now.Add(30 * day)},
		{Names: []string{"old.example.com"}, NotBefore: now.Add(-90 * day), NotAfter: now.Add(-day)},
		{Names: []string{"soon.example.com"}, NotBefore: now.Add(-80 * day), NotAfter: now.Add(5 * day)},
		{Names: []string{"future.example.com"}, NotBefore: now.Add(day), NotAfter: now.Add(90 * day)},
	}
	d := func(s string) DomainName { v, _ := ParseDomainName(s); return v }

	if st := certs.For(d("Blog.Example.com"), now); !st.Usable(now) || st.Issuer != "ZeroSSL" {
		t.Fatalf("the longest-lasting certificate wins: %+v", st)
	}
	if !certs.For(d("a.apps.example.com"), now).Usable(now) {
		t.Fatal("a wildcard covers one level")
	}
	for _, no := range []string{"a.b.apps.example.com", "apps.example.com", "old.example.com", "future.example.com", "nothing.example.com"} {
		if certs.For(d(no), now).Usable(now) {
			t.Errorf("%s has no usable certificate", no)
		}
	}
	if st := certs.For(d("soon.example.com"), now); !st.EndsSoon(now) {
		t.Fatal("five days left is soon")
	}
	if certs.For(d("blog.example.com"), now).EndsSoon(now) {
		t.Fatal("eighty days left is not soon")
	}
	if (CertificateState{}).Usable(now) {
		t.Fatal("no certificate is not usable")
	}
}
