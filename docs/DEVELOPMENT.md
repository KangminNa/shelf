# Shelf 개발 가이드

> 셀프호스팅 앱 플랫폼 — Git 저장소나 Docker 이미지를 컨테이너로 배포하고, 빌드·CI/CD·도메인·SSL을 코어가 처리한다.

## 1. 큰 그림

Shelf는 **오케스트레이터**다. 앱을 직접 실행하지 않고 Docker에 맡기며, 코어는 세 가지만 책임진다.

```
                    ┌──────────────────────────────────────────────┐
   :80/:443 ──────▶ │  ProxySystem   도메인 라우팅, SSL(Let's Encrypt) │
                    ├──────────────────────────────────────────────┤
   :9100    ──────▶ │  DeploySystem  git→build→run, webhook CI/CD   │
                    ├──────────────────────────────────────────────┤
   :81      ──────▶ │  Admin UI      대시보드/앱/프록시 관리            │
                    └──────────────┬───────────────────────────────┘
                                   │ docker CLI
                    ┌──────────────▼───────────────┐
                    │  shelf-{app} 컨테이너들         │  ← 앱 (언어/DB 자유)
                    └──────────────────────────────┘
```

**앱의 계약은 하나뿐**: 저장소 루트에 `Dockerfile`이 있고, 컨테이너가 포트 하나로 HTTP를 서빙한다.

배포 흐름:

```
git push → GitHub webhook(:9100) → 서명 검증(HMAC)
  → git pull → docker build → 컨테이너 재생성(shelf-{app})
  → 도메인 설정 시 프록시 자동 등록 → 80/443으로 서비스
```

## 2. 디렉토리 구조

```
shelf/
├── core/
│   ├── migrations/            # 시스템 DB 마이그레이션 (proxy/, deploy/)
│   └── src/
│       ├── index.ts           # 엔트리 — ShelfApplication.instance.start()
│       ├── config.ts          # 서버 설정 (환경변수 기반)
│       ├── kernel/
│       │   └── application.ts # ShelfApplication — 전역 인스턴스, 부팅/라우팅/종료
│       ├── system/            # 코어 내장 시스템
│       │   ├── docker.ts      # DockerService — docker CLI 래퍼
│       │   ├── deploy/        # 앱 배포 시스템
│       │   └── proxy/         # 리버스 프록시 시스템
│       ├── db/                # 데이터 계층 (날 SQL 금지)
│       │   ├── database.ts    # AppDatabase — 연결 + 마이그레이션 + repo()
│       │   ├── repository.ts  # Repository<T> — CRUD 베이스 클래스
│       │   └── query-builder.ts # QueryBuilder<T> — 체이닝 쿼리
│       ├── services/          # 공용 서비스 (클래스)
│       │   ├── events.ts      # EventBus — 시스템 간 통신
│       │   ├── log.ts         # Logger — 스코프드 로거
│       │   └── scheduler.ts   # Scheduler — cron 예약 작업
│       ├── middleware/        # error-boundary, request-logger, shell-wrap
│       ├── admin/             # 관리 화면 (dashboard, system, guide, settings)
│       └── ui/                # 디자인 시스템 (shell, components, icons, styles)
├── examples/hello-app/        # 배포 가능한 최소 예제 앱
├── data/                      # 런타임 데이터 (gitignore) — {scope}.db, ssl/, deploy/repos/
├── Dockerfile · docker-compose.yml
└── docs/DEVELOPMENT.md        # 이 문서
```

## 3. 코어 클래스 지도

### kernel — ShelfApplication (전역 인스턴스)

`ShelfApplication.instance` 싱글턴이 전체를 소유한다.

```
ShelfApplication
├── hono: Hono            # HTTP 앱 (:81)
├── events: EventBus      # 시스템 간 통신
├── proxy: ProxySystem
├── deploy: DeploySystem
├── start(port)           # 시스템 생성 → 라우트 등록 → serve
└── shutdown()
```

라우트 등록도 여기서만 한다:

| 경로 | 담당 |
|---|---|
| `/api/deploy/*` | DeployController.api |
| `/api/proxy/*` | ProxyController.api |
| `/admin/deploy/*` | DeployController.pages (셸 래핑) |
| `/admin/proxy/*` | ProxyController.pages (셸 래핑) |
| `/admin/*` | createAdminRoutes (dashboard/system/guide/settings) |

### system/deploy — 앱 배포

```
DeploySystem (조립 루트)
├── ProjectRepository / DeploymentRepository   # 데이터 (data/deploy.db)
├── DockerService                              # docker CLI 래퍼
├── ContainerManager                           # 컨테이너 생명주기 (shelf-{app})
├── DeployPipeline                             # clone/pull → build → recreate
├── WebhookServer                              # :9100, HMAC 검증 → 자동 배포
└── DeployController                           # api + pages (views.ts에 HTML)
```

- **Project**(= 앱): `source_type`(git|image), repo_url/branch 또는 image, `port`(호스트) → `container_port`(컨테이너), env, volumes, domain, webhook_secret
- **Deployment**: 배포 이력 — 커밋, 상태, 전체 빌드 로그, 소요시간, 트리거(manual|webhook)
- 컨테이너는 `--restart unless-stopped`로 실행 → 서버 재부팅에도 자동 복구

### system/proxy — 리버스 프록시

```
ProxySystem (조립 루트)
├── ProxyHostRepository / SslCertRepository / AccessLogRepository  # data/proxy.db
├── ProxyServer      # 80/443 리스너, SNI 인증서, WebSocket, ACME 챌린지 응답
├── SslManager       # Let's Encrypt 발급/자동갱신(만료 30일 전), 수동 PEM 업로드
└── ProxyController  # api + pages
```

### db — 데이터 계층 (날 SQL 금지)

비즈니스 로직에서 SQL 문자열을 쓰지 않는다. 3계층:

```ts
// 1. AppDatabase — 연결 + 마이그레이션
const db = new AppDatabase('deploy', migrationsDir)   // → data/deploy.db (WAL)

// 2. Repository<T> — 상속해서 도메인 메서드 추가
class ProjectRepository extends Repository<Project> {
  constructor(db: Database) { super(db, 'projects') }
  findByName(name: string) { return this.findBy({ name }) }
}
// 기본 제공: all / find / findBy / findAllBy / create / update / delete / count
// updated_at 컬럼이 있으면 update() 시 자동 갱신

// 3. QueryBuilder<T> — 복잡한 조회는 체이닝으로
repo.query().where('status', 'running').where('expires_at', '<', deadline)
    .orderBy('created_at', 'desc').limit(20).all()
```

스키마 변경은 `core/migrations/{scope}/NNN_name.sql` — 파일명 순서로 1회씩 자동 적용.

### services — 공용 서비스

| 클래스 | 역할 |
|---|---|
| `EventBus` | `emit/on/off`. 시스템 간 직접 참조 대신 이벤트로 통신 |
| `Logger` | `new Logger('deploy')`, `.scope('webhook')` → `[deploy:webhook]` |
| `Scheduler` | `register(cron, name, fn)` — SSL 자동갱신 등 예약 작업 |

주요 이벤트: `proxy:register-host`(도메인 자동 등록 요청), `deploy:started/succeeded/failed`, `proxy:cert-issued/renewed`

### ui + middleware — 관리 화면

- `renderShell({title, activePath, content, apps})` — 사이드바+탑바 셸. 사이드바에 앱 목록(실행 상태 점) 표시
- `createShellWrap(title, getApps)` — 컨트롤러 pages가 반환한 HTML **조각**을 셸로 자동 래핑하는 미들웨어
- `ui/components.ts` — statCard/badge/button/table 등 `shelf-*` 디자인 시스템

## 4. 컨벤션

- **컨트롤러/뷰 분리**: HTTP 핸들러는 `controller.ts`, HTML은 `views.ts`의 순수 함수
- **날 SQL 금지**: 리포지토리 메서드로만. 새 쿼리가 필요하면 리포지토리에 메서드 추가
- **시스템 간 통신은 EventBus**: proxy를 import하지 말고 `events.emit('proxy:register-host', ...)`
- **에러 응답 포맷**: `{ ok: false, error: { code, message } }` / 성공은 `{ ok: true, data }`
- **컨테이너 네이밍**: 컨테이너 `shelf-{app}`, git 빌드 이미지 `shelf-app-{app}`

## 5. 개발 워크플로

```bash
npm install
npm run dev            # tsx watch, http://localhost:9666/admin

# 포트가 겹칠 때 (기본값: 81/80/443/9100)
PORT=9667 PROXY_HTTP_PORT=8087 PROXY_HTTPS_PORT=8447 WEBHOOK_PORT=9100 \
  npx tsx core/src/index.ts

npx tsc --noEmit --project core   # 타입체크
```

| 환경변수 | 기본값 | 설명 |
|---|---|---|
| `PORT` | 9666 | 관리 UI 포트 (운영: 81) |
| `PROXY_HTTP_PORT` / `PROXY_HTTPS_PORT` | 80 / 443 | 프록시 리스너 |
| `WEBHOOK_PORT` | 9100 | CI/CD webhook 서버 |
| `ACME_EMAIL` | — | Let's Encrypt 기본 이메일 |

운영 배포: `docker compose up -d` (Shelf 자체도 컨테이너, docker.sock 마운트 필요)

## 6. 새 시스템 기능 추가하는 법

`system/{name}/`을 만들고 deploy/proxy 패턴을 따른다:

1. `core/migrations/{name}/001_init.sql` — 스키마
2. `repositories.ts` — `Repository<T>` 상속
3. 도메인 로직 클래스 (예: `pipeline.ts`)
4. `views.ts` — HTML 뷰 함수
5. `controller.ts` — `readonly api/pages = new Hono()`
6. `index.ts` — `{Name}System` 조립 루트 (생성자에서 의존성 wiring)
7. `kernel/application.ts`에 시스템 생성 + 라우트 등록

## 7. 앱 만들기 (플랫폼 사용자 관점)

```
my-app/
└── Dockerfile      # 이것만 있으면 됨. 나머지는 자유
```

1. **Admin → Apps → New app** — Git URL(+브랜치) 또는 Docker 이미지명, 포트 매핑(host→container), 환경변수, 볼륨, 도메인 입력
2. **Deploy** — clone → build → run. 로그는 배포 이력에서 확인
3. **CI/CD** — 앱 상세의 webhook URL+secret을 GitHub Settings→Webhooks에 등록하면 push마다 자동 배포
4. **도메인/SSL** — 도메인 입력 시 프록시 자동 등록, SSL은 Proxy 화면에서 원클릭 발급(자동 갱신)

전체 예제: `examples/hello-app/`
