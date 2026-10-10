// naru — 서버 한 대를 위한 웹서버 관리 + push 배포.
//
//	naru                         관리 화면을 띄운다
//	naru users                   계정 목록
//	naru passwd <아이디> <비밀번호>   비밀번호를 바꾼다 (그 계정의 모든 로그인이 끊긴다)
//	naru reset                   계정을 모두 지운다 → 다음 실행에서 첫 설정이 다시 열린다
//	naru version
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/KangminNa/naru/internal/app"
	"github.com/KangminNa/naru/internal/cli"
)

// 빌드할 때 -ldflags "-X main.version=..." 으로 바꾼다.
var version = "2.0.0-dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := app.ConfigFromEnv(version)

	if len(os.Args) > 1 {
		os.Exit(runCommand(cfg, log, os.Args[1:]))
	}

	a, err := app.Open(cfg, log)
	if err != nil {
		log.Error("cannot start", "err", err)
		os.Exit(1)
	}
	defer a.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := a.Run(ctx); err != nil {
		log.Error("stopped", "err", err)
		os.Exit(1)
	}
}

func runCommand(cfg app.Config, log *slog.Logger, args []string) int {
	if args[0] == "version" {
		fmt.Println("naru", version)
		return 0
	}
	a, err := app.Open(cfg, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer a.Close()
	return cli.Run(context.Background(), args, a.Accounts, os.Stdout, os.Stderr)
}
