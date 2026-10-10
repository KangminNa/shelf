// Package files는 파일을 다룬다 — 작업 폴더, 빌드 폴더 묶기, Dockerfile 읽기, 정적 사이트 파일.
package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// ErrEscapes는 경로가 기준 폴더 밖으로 나갔다는 뜻이다.
var ErrEscapes = errors.New("path leaves the folder")

// TempFolders는 root 아래에 작업 폴더를 빌려준다. 돌려받으면 지운다.
type TempFolders struct{ root string }

func NewTempFolders(root string) TempFolders { return TempFolders{root: root} }

// Clear는 지난 실행이 남긴 작업 폴더를 지운다. 서버를 띄울 때만 부른다 — 셸 명령이 부르면 도는 배포의 폴더를 지운다.
func (t TempFolders) Clear() error { return os.RemoveAll(t.root) }

func (t TempFolders) Borrow(name string) (string, func(), error) {
	if err := os.MkdirAll(t.root, 0o700); err != nil {
		return "", nil, err
	}
	dir := filepath.Join(t.root, filepath.Base(name))
	os.RemoveAll(dir)
	return dir, func() { os.RemoveAll(dir) }, nil
}

// Inside는 root 안의 p다. 심볼릭 링크를 따라가 밖으로 나가거나, 없으면 실패한다.
func (TempFolders) Inside(root string, p model.FolderPath) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(p.String()))
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return "", ErrEscapes
	}
	return full, nil
}
