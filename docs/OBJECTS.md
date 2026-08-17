# 객체 사전 — 누가 무엇을 담당하는가

클래스 하나당 한 문장. **이 문장으로 설명되지 않는 코드는 그 클래스에 있으면 안 된다.**
새 코드를 넣을 자리를 찾을 때 여기서 담당자를 먼저 고른다.

읽는 법: `클래스 — 책임 문장` / `소유: 누가 만드는가` / `금지: 하면 안 되는 일`

## kernel — 조립과 수명주기

**ShelfApplication** — 전역 인스턴스로서 시스템들을 만들고 라우트를 붙이고 서버를 띄우고 내린다.
- 소유: `index.ts`가 `.instance.start(port)` 한 번 호출
- 금지: 비즈니스 규칙(배포 절차, 인증 판단, HTML)을 직접 갖는 것. 조립과 위임만 한다.
- 여기만 여러 system을 안다. 라우트 등록은 **오직 여기서**.

## system/auth — 이 서버에 들어올 수 있는가

**AuthSystem** — 관리자 계정을 만들고 세션을 발급·검증하며, 보호된 경로의 출입을 판정한다.
- 소유: ShelfApplication
- 금지: 앱·프록시 도메인 지식. 인증 외 판단.
- `requireAuth()`가 유일한 출입구. 공개 경로는 `routes`(=`/login`, `/setup`, `/api/auth/*`)뿐.

**UserRepository / SessionRepository** (auth 내부) — 관리자 계정과 세션 토큰을 저장·조회하고 만료 세션을 지운다.
- auth 밖으로 노출하지 않는다 (다른 시스템은 사용자 테이블을 몰라야 한다)

**LoginPage / SetupPage** (`AuthPage`) — 셸 밖에서 자기 완결적인 인증 화면을 그린다.

## system/deploy — 앱을 실제로 돌게 만든다

**DeploySystem** — 배포에 필요한 부품들을 조립해 보유하고, 외부에는 `api`/`pages`/`webhook`만 노출한다.
- 소유: ShelfApplication
- 금지: HTTP 핸들링(→Controller), 컨테이너 명령(→ContainerManager)

**ProjectRepository / DeploymentRepository** — 앱 정의와 배포 이력을 저장·조회한다.
- 금지: 도커·git 호출. 데이터만.

**DockerService** (`system/docker.ts`) — docker CLI를 타입 있는 메서드로 감싼다.
- 금지: 앱이 뭔지 아는 것. `Project`를 받지 않는다. 이름·이미지·포트 같은 원시값만.

**ContainerManager** — 앱 하나를 컨테이너 하나로 대응시켜 생성·시작·중지·삭제하고, 이름과 프록시 타깃을 결정한다.
- 이름 규칙의 **단일 출처**: 컨테이너 `shelf-{앱}`, 빌드 이미지 `shelf-app-{앱}`, 타깃 `shelf-{앱}:{container_port}`
- 네트워크 `shelf-net` 확보와 기존 컨테이너 연결도 담당
- 금지: 빌드 절차(→Pipeline), DB 쓰기

**DeployPipeline** — 한 번의 배포를 처음부터 끝까지 진행하고 그 과정을 이력으로 남긴다.
- 순서: (image) pull · (git) clone/fetch → 커밋 기록 → Dockerfile 확인 → build → 컨테이너 재생성 → 도메인 발행
- 동시 배포를 막고, 성공/실패를 이력에 확정한다
- 금지: 프록시 DB 직접 수정 (이벤트로만)

**DeploymentJournal** (pipeline 내부) — 배포 과정의 출력을 모으면서 시크릿을 가리고 크기를 제한한다.

**WebhookServer** — 외부(GitHub 등)의 push 알림을 서명 검증해서 배포로 이어준다.
- 세션이 없는 공개 포트이므로 **HMAC이 유일한 방어**
- 금지: 배포 절차를 직접 실행 (→Pipeline에 위임)

**SelfDeployer** — Shelf 자신을 형제 컨테이너를 띄워 재빌드한다.
- 자기 컨테이너를 죽이는 명령을 자기가 실행할 수 없다는 제약을 해결하는 유일한 목적

**DeployController** — 앱에 대한 HTTP 요청을 받아 시스템에 위임하고 응답 형식을 맞춘다.
- 입력 검증(이름 규칙, 셸 메타문자)과 시크릿 제거(`sanitize`)가 여기 책임
- 금지: 도커·git 직접 호출, HTML 문자열 조립(→views)

**ProjectsPage / ProjectDetailPage / DeploymentsPage** (`DeployPage`) — 앱 상태와 배포 이력을 화면으로 그린다.

## system/proxy — 외부 요청을 올바른 앱에 넘긴다

**ProxySystem** — 프록시 부품을 조립하고, 다른 시스템의 호스트 등록/해제 요청을 이벤트로 받는다.
- 소유: ShelfApplication
- 구독: `proxy:register-host`, `proxy:release-target`

**ProxyHostRepository / SslCertRepository / AccessLogRepository** — 도메인 매핑, 인증서, 접근 기록을 저장·조회한다.

**ProxyServer** — 80/443에서 요청을 받아 원 요청을 보존한 채 대상 앱으로 넘긴다.
- 담당: 도메인 매칭, Host/X-Forwarded-* 전달, Force SSL 리다이렉트, HSTS 주입, WebSocket, SNI 인증서 선택(와일드카드 포함), ACME 챌린지 응답
- 금지: 인증서를 발급하는 일(→SslManager). 챌린지 **응답**만 한다.

**SslManager** — 인증서의 생애(발급·갱신·삭제)를 관리하고 발급 방식은 발급자에게 맡긴다.
- 발급 후 호스트의 SSL을 켜고 프록시를 리로드하는 것까지가 책임
- 금지: ACME 프로토콜·openssl·PEM 파싱을 직접 하는 것

**CertificateIssuer 구현체** — 각자 한 가지 방식으로 인증서 파일을 만들어 경로를 돌려준다.
- `LetsEncryptIssuer` (ACME, HTTP-01/DNS-01) · `SelfSignedIssuer` (openssl) · `ManualUploadIssuer` (PEM 검증)
- 금지: DB 쓰기. 파일만 만들고 반환.

**CertificateStore** — 인증서 파일의 저장 위치를 정하고 읽고 쓰며 만료일을 해석한다.

**CloudflareDns** (`DnsProvider`) — DNS-01 챌린지용 TXT 레코드를 만들고 지운다.
- 금지: 인증서 지식. DNS만.

**ProxyController** — 호스트·인증서·로그에 대한 HTTP 요청을 받아 위임한다.

**HostsPage / SslPage / AccessLogsPage** (`ProxyPage`) — 프록시 상태를 화면으로 그린다.

## admin — 코어 관리 화면

**DashboardPage** — 앱·프록시 규모와 서버 상태를 한눈에 보여주고 주요 작업으로 안내한다.
**SystemPage** — 메모리·환경·Docker 연결 상태와 앱 실행 현황을 보여준다.
**AppGuidePage** — 앱의 계약(Dockerfile)과 배포 방법을 문서 화면으로 제공한다.
**SettingsPage** — 환경변수로 정해진 서버 설정을 읽기 전용으로 보여준다.

## db — 데이터 접근

**AppDatabase** — 시스템별 SQLite 파일을 열고 마이그레이션을 적용하고 리포지토리를 나눠준다.

**Repository&lt;T&gt;** — 테이블 하나에 대한 CRUD를 제공하고 도메인 메서드의 상속 지점이 된다.
- `updated_at`이 있으면 자동 갱신

**QueryBuilder&lt;T&gt;** — 조건·정렬·범위를 체이닝으로 조립해 SQL을 만들고 값은 파라미터로 바인딩한다.
- **날 SQL이 존재해도 되는 유일한 계층**

## services — 공용 기반

**EventBus** — 시스템끼리 서로를 모르게 두면서 사실을 알린다.
- 네이밍 `{시스템}:{동작}`

현재 이벤트 계약 (요청형은 구독자가 반드시 있어야 하고, 통지형은 없어도 된다):

| 이벤트 | 종류 | 발행 | 구독 |
|---|---|---|---|
| `proxy:register-host` | 요청 | DeployPipeline, ShelfApplication | ProxySystem |
| `proxy:release-target` | 요청 | DeployPipeline, DeployController | ProxySystem |
| `deploy:started` | 통지 | DeployPipeline | — |
| `deploy:succeeded` | 통지 | DeployPipeline | — |
| `deploy:failed` | 통지 | DeployPipeline | — |
| `deploy:self-started` | 통지 | SelfDeployer | — |
| `deploy:project-created` | 통지 | DeployController | — |
| `deploy:project-deleted` | 통지 | DeployController | — |
| `deploy:container-started` | 통지 | ContainerManager | — |
| `proxy:host-created` | 통지 | ProxyController | — |
| `proxy:cert-issued` | 통지 | SslManager | — |
| `proxy:cert-renewed` | 통지 | SslManager | — |
| `proxy:cert-renewal-failed` | 통지 | SslManager | — |

통지형 구독자가 비어 있는 것은 알림 기능(F-16)이 아직 없기 때문이다. 그 기능은 이 이벤트들을 구독해서 만든다.

**Logger** — 어디서 난 로그인지 알 수 있게 스코프를 붙여 출력한다.

**Scheduler** — 주기 작업을 등록하고 종료 시 정리한다. (현재 사용: SSL 자동 갱신)

## ui — 화면 재료

**Page** — 모든 화면의 계약: `props` 하나를 받고 `render(): string`을 준다.
- 공통 조각(dialog·field·checkbox·tableCard·emptyState) 제공
- 화면의 부분은 **private 메서드**로. 모듈 전역 함수 금지.

**StringTemplatePage** — 문자열 템플릿으로 만들어진 기존 뷰가 이중 이스케이프되지 않게 하는 전환용 기반.
- 새 화면은 이걸 쓰지 않는다. `Page` + `el` 빌더로.

**el / Html / raw** (`ui/element.ts`) — 태그를 런타임에 조립하며 텍스트·속성을 기본 이스케이프한다.
- 신뢰된 마크업만 `raw()`로 명시적 예외

**renderShell** — 사이드바·탑바·프리뷰 패널로 관리 화면을 감싼다.

## middleware

**requireAuth**(AuthSystem 제공) — 보호 경로의 출입 판정
**createShellWrap** — 컨트롤러가 낸 HTML 조각을 셸로 감싼다
**errorBoundary** — 던져진 오류를 표준 응답으로 바꾼다
**requestLogger** — 요청 한 줄을 남긴다

## 판정 기준 (새 코드를 어디에 둘지 고를 때)

1. 데이터를 읽고 쓰는가 → **Repository**
2. 외부 명령·API를 호출하는가 → **Adapter**(DockerService, CloudflareDns)
3. 여러 단계를 순서대로 진행하는가 → **도메인 클래스**(Pipeline, SslManager)
4. HTTP 요청/응답을 다루는가 → **Controller**
5. 화면을 그리는가 → **Page**
6. 다른 시스템이 알아야 하는가 → **EventBus**
7. 위 어디에도 안 맞는가 → 새 클래스가 필요한 신호. 먼저 이 문서에 문장을 추가하고 만든다.
