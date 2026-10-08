// Package source는 서비스의 소스를 가져오고 다룬다 — git clone, 빌드 컨텍스트 묶기, 정적 파일 복사.
package source

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrBadRepoURL = errors.New("repository URL must be http(s)://")
	ErrBadBranch  = errors.New("invalid branch name")
	ErrEscapes    = errors.New("path leaves the repository")
)

var branchPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)

// ValidRepoURL은 git이 옵션으로 오해하거나 로컬 파일을 읽게 만들 수 없는 주소만 받는다.
func ValidRepoURL(raw string) error {
	if strings.ContainsAny(raw, " \t\n") || strings.HasPrefix(raw, "-") {
		return ErrBadRepoURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return ErrBadRepoURL
	}
	return nil
}

func ValidBranch(b string) error {
	if !branchPattern.MatchString(b) || strings.HasPrefix(b, "-") || strings.Contains(b, "..") {
		return ErrBadBranch
	}
	return nil
}

// CleanRel은 저장소 안의 상대 경로로 정리한다. ".."가 들어 있으면 받지 않는다.
func CleanRel(rel string) (string, error) {
	rel = strings.Trim(filepath.ToSlash(strings.TrimSpace(rel)), "/")
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", ErrEscapes
		}
	}
	return path.Clean("/" + rel)[1:], nil
}

// Within은 base 안의 rel 경로다. 심볼릭 링크를 따라가 밖으로 나가도 ErrEscapes.
func Within(base, rel string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(base, filepath.FromSlash(clean))
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err // 없는 폴더
	}
	if real != realBase && !strings.HasPrefix(real, realBase+string(filepath.Separator)) {
		return "", ErrEscapes
	}
	return full, nil
}

// Commit은 가져온 커밋이다.
type Commit struct {
	Hash    string
	Message string
}

// Clone은 branch의 최신 커밋 하나만 가져온다.
// 토큰은 명령줄 인자에도 .git/config에도 남지 않는다 — 환경 변수로 넘긴 일회성 설정으로만 쓴다.
func Clone(ctx context.Context, repoURL, branch, token, dir string, log io.Writer) (Commit, error) {
	if err := ValidRepoURL(repoURL); err != nil {
		return Commit{}, err
	}
	if err := ValidBranch(branch); err != nil {
		return Commit{}, err
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--single-branch", "--branch", branch, "--", repoURL, dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false")
	if token != "" {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Basic "+basic)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	io.Copy(log, &out)
	if err != nil {
		return Commit{}, fmt.Errorf("git clone failed: %w", err)
	}
	show := exec.CommandContext(ctx, "git", "-C", dir, "log", "-1", "--format=%H%x00%s")
	raw, err := show.Output()
	if err != nil {
		return Commit{}, fmt.Errorf("git log: %w", err)
	}
	hash, msg, _ := strings.Cut(strings.TrimSpace(string(raw)), "\x00")
	return Commit{Hash: hash, Message: msg}, nil
}

var exposePattern = regexp.MustCompile(`(?i)^\s*EXPOSE\s+(.+)$`)

// ExposedPorts는 Dockerfile의 EXPOSE에서 TCP 포트를 읽는다.
func ExposedPorts(dockerfile string) []int {
	f, err := os.Open(dockerfile)
	if err != nil {
		return nil
	}
	defer f.Close()
	var ports []int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := exposePattern.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		for _, field := range strings.Fields(m[1]) {
			num, proto, _ := strings.Cut(field, "/")
			if proto != "" && !strings.EqualFold(proto, "tcp") {
				continue
			}
			if n, err := strconv.Atoi(num); err == nil && n > 0 && n < 65536 {
				ports = append(ports, n)
			}
		}
	}
	return ports
}

// ignoreRules는 .dockerignore다. 마지막으로 맞은 규칙이 이긴다 (!는 다시 넣기).
type ignoreRules []struct {
	pattern string
	negate  bool
}

func readIgnore(dir string) ignoreRules {
	raw, err := os.ReadFile(filepath.Join(dir, ".dockerignore"))
	if err != nil {
		return nil
	}
	var rules ignoreRules
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg := strings.HasPrefix(line, "!")
		line = strings.TrimPrefix(line, "!")
		line = strings.Trim(path.Clean("/"+line), "/")
		rules = append(rules, struct {
			pattern string
			negate  bool
		}{line, neg})
	}
	return rules
}

// ignored는 rel(슬래시 구분)이 빠져야 하는가. 패턴이 경로나 그 상위 폴더에 맞으면 빠진다.
func (r ignoreRules) ignored(rel string) bool {
	out := false
	for _, rule := range r {
		if matchPathOrParent(rule.pattern, rel) {
			out = !rule.negate
		}
	}
	return out
}

func matchPathOrParent(pattern, rel string) bool {
	for p := rel; p != "." && p != ""; p = path.Dir(p) {
		if ok, _ := path.Match(pattern, p); ok {
			return true
		}
		if strings.HasPrefix(pattern, "**/") {
			if ok, _ := path.Match(strings.TrimPrefix(pattern, "**/"), path.Base(p)); ok {
				return true
			}
		}
	}
	return false
}

// Tar는 dir을 빌드 컨텍스트로 묶는다. .git과 .dockerignore에 맞는 것은 빠진다.
func Tar(dir string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(writeTar(dir, pw))
	}()
	return pr
}

func writeTar(dir string, w io.Writer) error {
	rules := readIgnore(dir)
	tw := tar.NewWriter(w)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git/") || (rules.ignored(rel) && rel != "Dockerfile" && rel != ".dockerignore") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = rel
		if d.IsDir() {
			hdr.Name += "/"
		}
		hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// CopyTree는 정적 사이트 파일을 복사한다.
// 숨김 파일·폴더(.git, .github, .env …)는 공개하지 않는다 — 비공개 저장소의 설정이 새지 않게. 단 .well-known은 웹 표준이라 둔다.
// 심볼릭 링크는 따라가지 않는다 (저장소 밖을 가리킬 수 있다).
func CopyTree(src, dst string) (files int, err error) {
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if hidden(filepath.ToSlash(rel)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			files++
			return out.Close()
		}
		return nil // 심볼릭 링크·특수 파일은 건너뛴다
	})
	return files, err
}

// hidden은 경로의 어느 부분이 .으로 시작하는가 (.well-known은 빼고).
func hidden(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") && seg != "." && seg != ".well-known" {
			return true
		}
	}
	return false
}
