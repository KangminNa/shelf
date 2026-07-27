# Shelf 설계 문서 — 디자인 패턴과 유지보수 규칙

> 이 문서는 "코드를 어디에, 어떤 모양으로 추가해야 하는가"의 기준이다.
> 배포 형태는 **Docker 단일 이미지** (Dockerfile → `docker compose up -d`)를 전제로 한다.

## 1. 계층과 의존 방향

```
                 ┌─────────────────────────────┐
                 │  kernel/ ShelfApplication    │  조립·라우팅·수명주기
                 └──────┬──────────────────────┘
                        ▼
   ┌────────────────────────────────────────────┐
   │  system/  auth · deploy · proxy · docker    │  도메인 기능 (서로 직접 참조 금지)
   └──────┬─────────────┬───────────────┬───────┘
          ▼             ▼               ▼
   ┌───────────┐ ┌────────────┐ ┌────────────────┐
   │ db/       │ │ services/  │ │ ui/ + admin/   │  공용 기반
   │ AppDatabase│ │ EventBus   │ │ shell·컴포넌트  │
   │ Repository │ │ Logger     │ │ 관리 페이지      │
   │ QueryBuilder│ │ Scheduler │ │                │
   └───────────┘ └────────────┘ └────────────────┘
```

**의존 방향 규칙** (위 → 아래로만):

1. `kernel`만 여러 system을 안다. **system끼리는 import 금지** — 통신은 EventBus로만
2. `db/`, `services/`, `ui/`는 어떤 system도 모른다 (역참조 금지)
3. HTML은 `views.ts`의 **순수 함수**에만 — 데이터를 인자로 받고 문자열을 반환, DB/네트워크 접근 금지
4. Hono(`c.req`, `c.json`)는 **controller와 kernel에서만** 등장
5. 날 SQL은 `db/` 디렉토리 안에서만 — 도메인 코드는 Repository 메서드로만 데이터 접근

## 2. 사용 중인 디자인 패턴 카탈로그

| 패턴 | 적용 위치 | 왜 |
|---|---|---|
| **Singleton** | `ShelfApplication.instance` | 전역 인스턴스 하나가 전체 수명주기 소유 |
| **Composition Root** (수동 DI) | 각 `system/{name}/index.ts` 생성자 | 의존성 wiring이 한 곳에 — 테스트 시 목 주입 지점 |
| **Facade** | `DeploySystem`, `ProxySystem`, `AuthSystem` | 서브시스템(5~6클래스)을 감추고 `api`/`pages`/`shutdown`만 노출 |
| **Repository** | `db/repository.ts` + 각 시스템 `repositories.ts` | 테이블당 1클래스, SQL 격리, 도메인 메서드로 의도 표현 |
| **Builder** | `db/query-builder.ts` | 복잡한 조회를 체이닝으로 (`where().orderBy().limit()`) |
| **MVC 변형** | `controller.ts` / `views.ts` / `repositories.ts` | HTTP 핸들링·화면·데이터의 분리 |
| **Observer (Pub/Sub)** | `services/events.ts` EventBus | 시스템 간 결합 제거 (`proxy:register-host` 등) |
| **Adapter** | `system/docker.ts` DockerService | 외부 CLI를 타입 있는 메서드로 감싸 도메인 코드에서 명령 문자열 제거 |
| **Chain of Responsibility** | Hono 미들웨어 파이프라인 | `auth → request-logger → handler → error-boundary` |
| **Template(마이그레이션)** | `core/migrations/{scope}/NNN_*.sql` | 스키마 변경을 순차 적용 파일로 표준화 |

**앞으로 권장** (지금은 미적용, 조건 충족 시 도입):
- **Strategy** — 배포 소스가 git/image 외로 늘어나면(예: tarball, registry webhook) `pipeline.ts`의 분기를 `DeploySource` 인터페이스로 추출
- **Health-check + Blue/Green** — 무중단 배포 도입 시 `ContainerManager.recreate()`를 "새 컨테이너 기동 → 헬스체크 → 트래픽 전환 → 구 컨테이너 제거"로 교체

## 3. 새 코드를 어디에 두는가 (레시피)

**A. 기존 시스템에 API/화면 추가**
1. 데이터가 필요하면 `repositories.ts`에 메서드 추가 (SQL은 QueryBuilder로)
2. 로직이 크면 도메인 클래스(예: `pipeline.ts`)에, 작으면 controller에
3. `controller.ts`에 라우트, HTML은 `views.ts`에 순수 함수
4. 응답 포맷: 성공 `{ ok: true, data }` / 실패 `{ ok: false, error: { code, message } }`

**B. 새 시스템 기능 (예: 알림, 백업)**
```
core/src/system/{name}/
├── index.ts         # {Name}System — Facade + Composition Root
├── repositories.ts  # Repository<T> 상속 (DB가 필요하면 AppDatabase('{name}'))
├── (도메인 클래스)    # 실제 로직
├── controller.ts    # api/pages Hono
└── views.ts         # HTML 순수 함수
```
+ `core/migrations/{name}/001_init.sql`
+ `kernel/application.ts`에서 생성·라우팅·shutdown 연결 (이 파일 외에는 수정할 곳 없음)

**C. 다른 시스템의 동작이 필요할 때**
직접 호출하지 말고 이벤트를 정의한다. 네이밍: `{시스템}:{동작}` (예: `deploy:succeeded`).
수신 측 시스템의 생성자에서 `events.on(...)`으로 구독.

**D. 스키마 변경**
기존 SQL 파일 수정 금지 — `NNN+1_설명.sql` 새 파일 추가 (ALTER TABLE).

## 4. 금지 목록 (리뷰 체크리스트)

- [ ] system A에서 system B를 import하지 않았는가 → EventBus
- [ ] controller/pipeline 등 도메인 코드에 `sqlite.prepare(...)`가 없는가 → Repository
- [ ] views 함수가 인자 외의 것(DB, env, fetch)에 접근하지 않는가
- [ ] 시크릿(토큰, webhook secret)이 API 응답·로그에 노출되지 않는가 → `sanitize()`/마스킹
- [ ] 적용된 마이그레이션 파일을 수정하지 않았는가
- [ ] 새 의존성을 추가했는가 → 꼭 필요한가? (코어 런타임 의존성 3개 유지: hono, @hono/node-server, better-sqlite3)

## 5. 배포 형태 (Docker)

Shelf 자체가 하나의 이미지다. 운영 = `docker compose up -d`.

```
이미지: node:20-alpine + docker-cli + git
볼륨:   /var/run/docker.sock  (호스트 데몬으로 앱 컨테이너 관리)
        ./data:/app/data      (DB·SSL·클론 저장소 — 백업 대상은 이 디렉토리 하나)
포트:   80/443(프록시) · 81(관리) · 9100(webhook)
헬스체크: GET /health (Dockerfile HEALTHCHECK + compose)
```

- 앱 컨테이너는 호스트 데몬에서 실행되므로 컨테이너 안의 프록시는 `APP_HOST=host.docker.internal`로 앱에 도달한다
- Shelf 재시작/업데이트가 앱 컨테이너에 영향 없음 (`--restart unless-stopped`는 데몬이 관리)
- 관리 UI의 restart 버튼은 `process.exit(0)` → compose restart 정책이 되살림

## 6. 코드 스타일

- 클래스: 생성자에서 의존성 주입, `readonly` 필드, private 메서드는 `// --- 내부 ---` 아래에
- 파일 상단에 역할을 설명하는 블록 주석 (한국어)
- 상수는 `static readonly` 또는 파일 상단 UPPER_SNAKE
- async 여부는 호출 체인에 맞춰 일관되게 (docker 관련은 전부 async)
