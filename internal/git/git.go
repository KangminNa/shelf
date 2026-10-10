// Package git은 저장소 코드를 내려받는다.
package git

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// Downloader는 git으로 branch의 최신 커밋 하나만 가져온다.
// 토큰은 명령줄 인자에도 .git/config에도 남지 않는다 — 환경 변수로 넘긴 일회성 설정으로만 쓴다.
// 주소와 브랜치는 model의 값 객체라 이미 검사됐다 (옵션으로 오해될 수 없다).
type Downloader struct{}

func (Downloader) Download(ctx context.Context, from model.CodeSource, into string, log io.Writer) (model.Commit, error) {
	branch := from.Branch
	if branch.IsZero() {
		branch = model.DefaultBranch()
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--single-branch", "--branch", branch.String(), "--", from.Repo.String(), into)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false")
	if from.Token != "" {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + from.Token))
		cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Basic "+basic)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	io.Copy(log, &out)
	if err != nil {
		return model.Commit{}, fmt.Errorf("git clone failed: %w", err)
	}
	raw, err := exec.CommandContext(ctx, "git", "-C", into, "log", "-1", "--format=%H%x00%s").Output()
	if err != nil {
		return model.Commit{}, fmt.Errorf("git log: %w", err)
	}
	hash, msg, _ := strings.Cut(strings.TrimSpace(string(raw)), "\x00")
	return model.Commit{Hash: hash, Message: msg}, nil
}
