// Package cli는 서버 셸에서 쓰는 복구 명령이다. 관리 화면에 들어갈 수 없을 때 계정을 되찾는다.
//
//	naru users                   계정 목록
//	naru passwd <아이디> <비밀번호>   비밀번호를 바꾼다 (그 계정의 모든 로그인이 끊긴다)
//	naru reset                   계정을 모두 지운다 → 다음 실행에서 첫 설정이 다시 열린다
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/KangminNa/naru/internal/contract"
)

// Run은 명령 하나를 하고 종료 코드를 돌려준다.
func Run(ctx context.Context, args []string, accounts contract.AccountManager, out, errOut io.Writer) int {
	switch args[0] {
	case "users":
		names, err := accounts.Names(ctx)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		if len(names) == 0 {
			fmt.Fprintln(out, "(계정 없음 — 첫 설정이 열려 있습니다 / no accounts — setup is open)")
		}
		for _, n := range names {
			fmt.Fprintln(out, n)
		}
	case "passwd":
		if len(args) != 3 {
			fmt.Fprintln(errOut, "usage: naru passwd <username> <new-password>")
			return 2
		}
		if err := accounts.Recover(ctx, args[1], args[2]); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		fmt.Fprintf(out, "%s: 비밀번호를 바꿨습니다. 모든 로그인이 끊겼습니다. / password changed, all sessions revoked\n", args[1])
	case "reset":
		if err := accounts.ResetAll(ctx); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		fmt.Fprintln(out, "계정을 모두 지웠습니다. 다시 시작하면 첫 설정 주소가 로그에 찍힙니다. / all accounts removed; restart to get a new setup link")
	default:
		fmt.Fprintf(errOut, "unknown command %q (users | passwd | reset | version)\n", args[0])
		return 2
	}
	return 0
}
