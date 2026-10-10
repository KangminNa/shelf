package model

import "time"

// ── 로그 (M6-2) ─────────────────────────────

type LogSource string

const (
	LogApp     LogSource = "app"     // 앱이 찍은 것 (컨테이너 출력)
	LogRequest LogSource = "request" // 웹서버가 받은 요청
)

// LogLine은 로그 한 줄이다. 요청이면 Method·Path·Status·Took이 채워진다.
type LogLine struct {
	At     time.Time
	Source LogSource
	Stream string // 앱: "stdout" · "stderr"
	Text   string // 앱: 찍은 글자 그대로
	Method string
	Path   string // 쿼리 문자열 포함 — 내 서버의 로그다
	Status int
	Took   time.Duration
}

// LogFilter는 로그 화면에서 무엇을 볼지다 — 전체 · 앱 · 요청.
type LogFilter string

const (
	LogsAll      LogFilter = "all"
	LogsApp      LogFilter = "app"
	LogsRequests LogFilter = "request"
)

// ParseLogFilter는 모르는 값이면 전체다.
func ParseLogFilter(s string) LogFilter {
	switch LogFilter(s) {
	case LogsApp, LogsRequests:
		return LogFilter(s)
	}
	return LogsAll
}

// LogsView는 서비스 로그 화면이다. 줄은 시간순(오래된 것부터).
type LogsView struct {
	Service      Service
	Filter       LogFilter
	Lines        []LogLine
	HasApp       bool   // 앱 출력이 있는 종류인가 (컨테이너 서비스)
	HasRequests  bool   // 주소가 있어 요청 기록이 있을 수 있는가
	AppError     string // 앱 출력을 읽지 못한 이유 (그대로 보여준다)
	RequestError string // 요청 기록을 읽지 못한 이유
}
