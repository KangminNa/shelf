package model

import "errors"

// InputError는 어느 칸이 왜 틀렸는지다. 문구는 만들지 않는다 — 화면이 Code로 한국어·영어 문구를 고른다.
type InputError struct {
	Field string // 어느 칸 ("domain", "repo" …)
	Code  string // 무엇이 문제인가 ("domain", "domaintaken", "admindomain" …)
}

func (e InputError) Error() string { return "invalid " + e.Field + ": " + e.Code }

// AsInputError는 err가 InputError면 꺼낸다.
func AsInputError(err error) (InputError, bool) {
	var ie InputError
	ok := errors.As(err, &ie)
	return ie, ok
}

// 객체들이 주고받는 실패. 화면은 이것으로 알맞은 문구를 고른다.
var (
	ErrNotFound        = errors.New("not found")
	ErrSetByEnv        = errors.New("set by an environment variable")
	ErrQueued          = errors.New("a deploy is running — another will follow")
	ErrBusy            = errors.New("a deploy is running")
	ErrNothingToDeploy = errors.New("this service has nothing to deploy")
	ErrCannotRollBack  = errors.New("that deployment cannot be restored")
	ErrSetupClosed     = errors.New("setup is already complete")
	ErrBadSetupKey     = errors.New("setup key is missing or wrong")
	ErrWeakPassword    = errors.New("password too short")
	ErrBadLogin        = errors.New("invalid username or password")
	ErrWrongPassword   = errors.New("the current password is wrong")
	ErrLocked          = errors.New("too many failed attempts")
	ErrNoIndexHTML     = errors.New("no index.html in that folder")
	ErrNoDockerfile    = errors.New("no Dockerfile")
	ErrUnknownPort     = errors.New("unknown app port")
)

// MinPasswordLength는 비밀번호의 최소 길이다.
const MinPasswordLength = 8
