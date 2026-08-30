import { AuthSystem } from '../system/auth/index.js'

const USAGE = `
Shelf 관리자 계정 복구

  npm run admin list                       계정 목록
  npm run admin passwd <username> <새 비밀번호>   비밀번호 교체 (해당 계정의 모든 세션 폐기)
  npm run admin reset                      모든 계정 삭제 → 다음 접속에서 /setup 다시 열림

도커에서:
  docker compose exec shelf npm run admin passwd admin '새비밀번호'
`

function main(): void {
  const [command, ...args] = process.argv.slice(2)
  const auth = new AuthSystem()

  if (command === 'list') {
    const accounts = auth.accounts
    console.log(accounts.length ? accounts.join('\n') : '(계정 없음 — /setup 에서 만드세요)')
    return
  }

  if (command === 'passwd') {
    const [username, password] = args
    if (!username || !password) return fail('사용법: npm run admin passwd <username> <새 비밀번호>')
    if (password.length < 8) return fail('비밀번호는 8자 이상이어야 합니다')
    if (!auth.setPassword(username, password)) return fail(`"${username}" 계정이 없습니다. 목록: ${auth.accounts.join(', ') || '(없음)'}`)
    console.log(`"${username}" 비밀번호를 바꿨습니다. 기존 로그인은 모두 무효화됐습니다.`)
    return
  }

  if (command === 'reset') {
    auth.forgetEveryone()
    console.log('모든 계정을 삭제했습니다. 브라우저로 접속하면 /setup 에서 다시 만들 수 있습니다.')
    return
  }

  console.log(USAGE)
}

function fail(message: string): void {
  console.error(message)
  process.exitCode = 1
}

main()
