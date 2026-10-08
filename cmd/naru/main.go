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

	switch args[0] {
	case "users":
		names, err := a.Auth.Usernames()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if len(names) == 0 {
			fmt.Println("(계정 없음 — 첫 설정이 열려 있습니다 / no accounts — setup is open)")
		}
		for _, n := range names {
			fmt.Println(n)
		}
	case "passwd":
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: naru passwd <username> <new-password>")
			return 2
		}
		if err := a.Auth.SetPassword(args[1], args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("%s: 비밀번호를 바꿨습니다. 모든 로그인이 끊겼습니다. / password changed, all sessions revoked\n", args[1])
	case "reset":
		if err := a.Auth.Reset(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("계정을 모두 지웠습니다. 다시 시작하면 첫 설정 주소가 로그에 찍힙니다. / all accounts removed; restart to get a new setup link")
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (users | passwd | reset | version)\n", args[0])
		return 2
	}
	return 0
}
