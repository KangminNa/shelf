package files

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// SiteFolders는 정적 사이트 배포본을 root/{서비스}/{배포번호}에 둔다. 웹서버는 같은 폴더를 읽기만 한다.
type SiteFolders struct{ root string }

func NewSiteFolders(root string) SiteFolders { return SiteFolders{root: root} }

func (s SiteFolders) dir(f model.SiteFolder) string {
	return filepath.Join(s.root, f.Site.String(), f.ReleaseName())
}

// Publish는 from의 파일을 배포본으로 올린다. index.html이 없으면 올리지 않는다.
func (s SiteFolders) Publish(from string, to model.SiteFolder) (int, error) {
	if _, err := os.Stat(filepath.Join(from, "index.html")); err != nil {
		return 0, model.ErrNoIndexHTML
	}
	dst := s.dir(to)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	n, err := copyTree(from, dst)
	if err != nil {
		os.RemoveAll(dst)
	}
	return n, err
}

// Copy는 예전 배포본을 새 번호로 복사한다 (되돌리기).
func (s SiteFolders) Copy(from, to model.SiteFolder) error {
	dst := s.dir(to)
	if _, err := copyTree(s.dir(from), dst); err != nil {
		os.RemoveAll(dst)
		return err
	}
	return nil
}

func (s SiteFolders) Exists(f model.SiteFolder) bool {
	info, err := os.Stat(s.dir(f))
	return err == nil && info.IsDir()
}

// Prune은 최근 keep개와 지금 서빙 중인 것만 남긴다.
func (s SiteFolders) Prune(site model.ServiceName, keep int, live model.DeploymentID) {
	siteDir := filepath.Join(s.root, site.String())
	entries, err := os.ReadDir(siteDir)
	if err != nil {
		return
	}
	var ids []int64
	for _, e := range entries {
		if n, err := strconv.ParseInt(e.Name(), 10, 64); err == nil && e.IsDir() {
			ids = append(ids, n)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	for i, n := range ids {
		if i >= keep && model.DeploymentID(n) != live {
			os.RemoveAll(filepath.Join(siteDir, strconv.FormatInt(n, 10)))
		}
	}
}

func (s SiteFolders) RemoveSite(site model.ServiceName) error {
	if site.IsZero() {
		return nil
	}
	return os.RemoveAll(filepath.Join(s.root, site.String()))
}

// CopyTree는 정적 사이트 파일을 복사한다.
// 숨김 파일·폴더(.git, .github, .env …)는 공개하지 않는다 — 비공개 저장소의 설정이 새지 않게. 단 .well-known은 웹 표준이라 둔다.
// 심볼릭 링크는 따라가지 않는다 (저장소 밖을 가리킬 수 있다).
func copyTree(src, dst string) (files int, err error) {
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
