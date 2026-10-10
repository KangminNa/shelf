package files

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/KangminNa/naru/internal/model"
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

func folder(s string) model.FolderPath {
	p, err := model.ParseFolderPath(s)
	if err != nil {
		panic(err)
	}
	return p
}

func TestInside(t *testing.T) {
	base := t.TempDir()
	os.MkdirAll(filepath.Join(base, "site", "dist"), 0o755)
	w := NewTempFolders(t.TempDir())
	if p, err := w.Inside(base, folder("/site/dist/")); err != nil || p != filepath.Join(base, "site", "dist") {
		t.Fatalf("%q %v", p, err)
	}
	if p, err := w.Inside(base, folder("")); err != nil || p != base {
		t.Fatalf("empty means the root: %q %v", p, err)
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(base, "sneaky"))
	if _, err := w.Inside(base, folder("sneaky")); err == nil {
		t.Error("a symlink pointing outside must not be followed")
	}
	if _, err := w.Inside(base, folder("missing")); err == nil {
		t.Error("a missing folder is an error")
	}
}

func TestBorrowAndClear(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	w := NewTempFolders(root)
	dir, giveBack, err := w.Borrow("../../escape")
	if err != nil || filepath.Dir(dir) != root {
		t.Fatalf("a borrowed folder stays under the root: %q %v", dir, err)
	}
	os.MkdirAll(dir, 0o755)
	giveBack()
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("giving back removes it")
	}
	os.MkdirAll(filepath.Join(root, "left-over"), 0o755)
	w.Clear()
	if _, err := os.Stat(root); err == nil {
		t.Fatal("Clear removes what a previous run left")
	}
}

func TestDockerfilePorts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"Dockerfile": "FROM node:20\nEXPOSE 3000\nexpose 9229/tcp 53/udp\n# EXPOSE 1\n"})
	got, found := DockerfileReader{}.Ports(dir)
	if !found || len(got) != 2 || got[0] != 3000 || got[1] != 9229 {
		t.Fatalf("%v %v", got, found)
	}
	if ports, found := (DockerfileReader{}).Ports(t.TempDir()); found || ports != nil {
		t.Fatal("no Dockerfile, no ports")
	}
}

func tarNames(t *testing.T, dir string) []string {
	t.Helper()
	rc := TarPacker{}.Pack(dir)
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

func TestPackHonoursDockerignore(t *testing.T) {
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

func TestPublishSkipsHiddenFilesAndLinks(t *testing.T) {
	src, root := t.TempDir(), t.TempDir()
	write(t, src, map[string]string{"index.html": "<h1>hi</h1>", "assets/app.css": "body{}", ".git/HEAD": "ref",
		".env": "SECRET=1", ".github/workflows/ci.yml": "x", "assets/.DS_Store": "x", ".well-known/security.txt": "contact"})
	os.Symlink("/etc/passwd", filepath.Join(src, "passwd"))
	sites := NewSiteFolders(root)
	name, _ := model.ParseServiceName("landing")
	to := model.SiteFolder{Site: name, Release: 4}
	n, err := sites.Publish(src, to)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	dst := filepath.Join(root, "landing", "4")
	for _, gone := range []string{".env", ".github", "assets/.DS_Store", ".git"} {
		if _, err := os.Stat(filepath.Join(dst, gone)); err == nil {
			t.Errorf("%s must not be published", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, ".well-known", "security.txt")); err != nil {
		t.Error(".well-known is a web standard and stays")
	}
	if _, err := os.Lstat(filepath.Join(dst, "passwd")); err == nil {
		t.Error("symlinks are not published")
	}

	if _, err := sites.Publish(t.TempDir(), model.SiteFolder{Site: name, Release: 5}); err != model.ErrNoIndexHTML {
		t.Fatal("a folder without index.html is not published")
	}
	if err := sites.Copy(to, model.SiteFolder{Site: name, Release: 6}); err != nil || !sites.Exists(model.SiteFolder{Site: name, Release: 6}) {
		t.Fatal("copy to a new release")
	}
	for i := 7; i < 14; i++ {
		sites.Copy(to, model.SiteFolder{Site: name, Release: model.DeploymentID(i)})
	}
	sites.Prune(name, 3, 4)
	if !sites.Exists(to) || !sites.Exists(model.SiteFolder{Site: name, Release: 13}) || sites.Exists(model.SiteFolder{Site: name, Release: 9}) {
		t.Fatal("prune keeps the newest and the live one")
	}
	sites.RemoveSite(name)
	if sites.Exists(to) {
		t.Fatal("removed")
	}
}
