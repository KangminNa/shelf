package source

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidation(t *testing.T) {
	for _, ok := range []string{"https://github.com/me/blog", "https://github.com/me/blog.git", "http://git.lan/x"} {
		if ValidRepoURL(ok) != nil {
			t.Errorf("%s should pass", ok)
		}
	}
	for _, bad := range []string{"", "-uhttps://x", "file:///etc", "ssh://git@github.com/x", "https://user:pw@github.com/x", "https://github.com/a b", "ext::sh -c id"} {
		if ValidRepoURL(bad) == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	for _, bad := range []string{"", "-x", "--upload-pack=x", "a..b", "feat x"} {
		if ValidBranch(bad) == nil {
			t.Errorf("branch %q should be refused", bad)
		}
	}
	if ValidBranch("feature/new-ui") != nil {
		t.Error("slashes in branches are normal")
	}
}

func TestWithin(t *testing.T) {
	base := t.TempDir()
	os.MkdirAll(filepath.Join(base, "site", "dist"), 0o755)
	if p, err := Within(base, "/site/dist/"); err != nil || p != filepath.Join(base, "site", "dist") {
		t.Fatalf("%q %v", p, err)
	}
	if p, err := Within(base, ""); err != nil || p != base {
		t.Fatalf("empty means the root: %q %v", p, err)
	}
	for _, bad := range []string{"../etc", "site/../../x", ".."} {
		if _, err := Within(base, bad); err == nil {
			t.Errorf("%q must not leave the repository", bad)
		}
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(base, "sneaky"))
	if _, err := Within(base, "sneaky"); err == nil {
		t.Error("a symlink pointing outside must not be followed")
	}
	if _, err := Within(base, "missing"); err == nil {
		t.Error("a missing folder is an error")
	}
}

func TestExposedPorts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"Dockerfile": "FROM node:20\nEXPOSE 3000\nexpose 9229/tcp 53/udp\n# EXPOSE 1\n"})
	got := ExposedPorts(filepath.Join(dir, "Dockerfile"))
	if len(got) != 2 || got[0] != 3000 || got[1] != 9229 {
		t.Fatalf("%v", got)
	}
	if ExposedPorts(filepath.Join(dir, "nope")) != nil {
		t.Fatal("no Dockerfile, no ports")
	}
}

func tarNames(t *testing.T, dir string) []string {
	t.Helper()
	rc := Tar(dir)
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(raw))
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	sort.Strings(names)
	return names
}

func TestTarHonoursDockerignore(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"Dockerfile":              "FROM scratch",
		".dockerignore":           "node_modules\n*.log\nsecrets/\n!keep.log\n# comment\n",
		"app.js":                  "x",
		"debug.log":               "x",
		"keep.log":                "x",
		"node_modules/a/index.js": "x",
		"secrets/key":             "x",
		"src/lib.js":              "x",
		".git/config":             "x",
	})
	got := strings.Join(tarNames(t, dir), " ")
	for _, want := range []string{"Dockerfile", ".dockerignore", "app.js", "keep.log", "src/", "src/lib.js"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	for _, gone := range []string{"debug.log", "node_modules", "secrets", ".git"} {
		if strings.Contains(got, gone) {
			t.Errorf("%s must be excluded: %s", gone, got)
		}
	}
}

func TestCopyTreeSkipsGitAndLinks(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, src, map[string]string{"index.html": "<h1>hi</h1>", "assets/app.css": "body{}", ".git/HEAD": "ref",
		".env": "SECRET=1", ".github/workflows/ci.yml": "x", "assets/.DS_Store": "x", ".well-known/security.txt": "contact"})
	os.Symlink("/etc/passwd", filepath.Join(src, "passwd"))
	n, err := CopyTree(src, dst)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	for _, gone := range []string{".env", ".github", "assets/.DS_Store"} {
		if _, err := os.Stat(filepath.Join(dst, gone)); err == nil {
			t.Errorf("%s must not be published", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, ".well-known", "security.txt")); err != nil {
		t.Error(".well-known is a web standard and stays")
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		t.Error(".git is not published")
	}
	if _, err := os.Lstat(filepath.Join(dst, "passwd")); err == nil {
		t.Error("symlinks are not published")
	}
}

// 진짜 git으로 clone해 본다 (git이 없는 곳에서는 건너뛴다).
func TestCloneFromALocalRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	origin := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = origin
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("init", "-q", "-b", "main")
	write(t, origin, map[string]string{"index.html": "hi"})
	run("add", ".")
	run("commit", "-q", "-m", "first page")
	// file:// 은 ValidRepoURL이 막으므로 내부 함수로 직접 시험하지 않고, 거부되는지만 본다
	if _, err := Clone(context.Background(), "file://"+origin, "main", "", filepath.Join(t.TempDir(), "c"), io.Discard); err == nil {
		t.Fatal("local file URLs are refused")
	}
}
