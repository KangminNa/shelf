# 웹서버 엔진을 Caddy로 — 설계

상태: **제안** · 2026-10-03
관련: [Caddy 사용성 분석](caddy-usability.md) · [v2 객체 설계](v2-objects.md)

> v1(Node) 시절에 쓴 설계다. 코드 위치(`core/src/…`)와 단계(P0~P4)는 v1 기준이고, v2 구현은 [객체 설계](v2-objects.md)를 따른다.
> 이 문서에서 지금도 유효한 것은 **불변식(§4)과 인증서(§5)** 다.

---

## 0. 한 문장

> Shelf는 **내 서버의 웹서버**다. 배포부터 도메인·인증서·감시까지 서버 위의 서비스를 관리한다.
> 그 안에서 실제 트래픽을 처리하는 엔진을 Caddy로 바꿔, **서비스마다의 웹서버를 Caddy 수준으로 끌어올린다.**

안을 들여다보면 역할이 둘로 나뉜다.

| | 맡는 것 |
|---|---|
| **관제** (Shelf) | 무엇을 어떻게 서빙할지 정한다 — 앱 배포, 서비스별 설정, 감시, 알림 |
| **엔진** (Caddy) | 정한 대로 실제로 서빙한다 — TLS, HTTP/2·3, 압축, 헤더, 프록시 |

사용자에게는 둘이 합쳐진 하나의 웹서버로 보인다. 나눠두는 이유는 하나다 — **관제가 재시작하거나 죽어도 서빙은 멈추지 않게.**

### 서비스 입장에서 달라지는 것

앱은 그대로 두고, 앞단의 Caddy가 서비스마다 웹서버 기능을 입힌다.

```
지금    요청 → [Shelf 프록시: HTTP/1.1, 전달만] → blog 컨테이너
앞으로  요청 → [Caddy: blog의 설정 적용]       → blog 컨테이너  (코드·Dockerfile 그대로)
                 HTTPS 자동 · HTTP/2·3 · 압축 · 보안 헤더 · 캐시 · 접근 제어 · 에러 페이지
```

앱이 직접 구현하던(혹은 안 하던) 압축·HTTPS·보안 헤더를 앱이 몰라도 되게 된다.

## 1. 왜 바꾸나

| | 지금 (자체 프록시) | Caddy 엔진 |
|---|---|---|
| 프로토콜 | HTTP/1.1만 | HTTP/1.1 · **HTTP/2** · **HTTP/3** |
| 라우팅 | 호스트 이름 정확히 일치만 | 호스트 · **경로** · 헤더 · IP |
| 헤더 | 고정 (HSTS만) | 요청·응답 헤더 추가/삭제 자유 |
| 압축·캐시 | 없음 | gzip · zstd, Cache-Control |
| 접근 제어 | 없음 | IP 허용/차단, Basic Auth |
| 업스트림 | 하나 | 여러 개 · 로드밸런싱 · 헬스체크 |
| 인증서 | **발급은 화면에서 클릭** | 도메인이 생기는 순간 자동 발급 |
| Shelf가 죽으면 | **모든 사이트가 같이 죽음** | 사이트는 계속 산다 |

마지막 줄이 특히 중요하다. 지금은 관리 화면 버그 하나가 서버의 모든 서비스를 내린다.

---

## 2. 구조

```
인터넷 ──▶ :80 / :443 / :443(udp) ──▶ [ caddy ] ──── shelf-net ────▶ shelf-blog:3000
                                          ▲  │                      ▶ shelf-landing:4023
                          관리 소켓(unix) │  │                      ▶ shelf:81  (관리 화면)
                                          │  └─ 인증서·접근 로그 파일 (공유 볼륨, Shelf는 읽기만)
                                          │
                                     [ shelf ]   DB(의도) ──렌더──▶ POST /load
```

| 누가 | 하는 일 |
|---|---|
| **Caddy** | 트래픽 처리, TLS 발급·갱신, 압축, 헤더, 리다이렉트, 접근 로그 |
| **Shelf** | 설정의 **원본**(DB), 화면, 앱 빌드·배포, 감시·알림, 설정 렌더링 |

두 가지 원칙:

1. **설정의 원본은 Shelf DB 하나.** Caddy 설정은 매번 **통째로 그려서 교체**한다. 부분 수정(PATCH)은 쓰지 않는다 — 어긋날 수 있는 상태를 만들지 않기 위해서다.
2. **Shelf가 죽어도 사이트는 산다.** Caddy는 `--resume`으로 마지막 설정을 유지한다. Shelf는 다시 뜨면 그려서 맞춘다.

---

## 3. 사이트 설정 — 이 설계의 핵심

### 3.1 화면에서 다루는 것

서비스(프록시 호스트) 하나마다 아래를 켜고 끈다. 사람이 생각하는 순서대로 묶었다.

**연결**

| 설정 | 화면 | Caddy가 하는 일 |
|---|---|---|
| 도메인 | 여러 개 입력 (별칭) | `host` 매처 |
| 보낼 곳 | 앱 선택 / `컨테이너:포트` / 외부 URL | `reverse_proxy` |
| 여러 곳으로 나누기 | 업스트림 추가 + 방식(라운드로빈·최소연결) | `load_balancing` |
| 헬스체크 | 경로 · 주기 | `health_checks.active` |
| 경로별로 다른 곳 | `/api/*` → api 앱, `/` → web 앱 | 경로 `subroute` |
| www ↔ 루트 도메인 | 한쪽으로 리다이렉트 | `static_response` 301 |

**보안**

| 설정 | 화면 | Caddy가 하는 일 |
|---|---|---|
| HTTPS | 자동 · DNS · 내부 · 업로드 · 끔 | `tls.automation` / `load_files` |
| HTTP → HTTPS | 켜기/끄기 | `:80` 서버의 308 |
| HSTS | 켜기 · 하위 도메인 · preload | `Strict-Transport-Security` |
| 보안 헤더 | 프리셋 한 번 클릭 | `X-Content-Type-Options` · `X-Frame-Options` · `Referrer-Policy` · `Permissions-Policy` |
| CSP | 직접 입력 | `Content-Security-Policy` |
| IP 제한 | 허용 목록 / 차단 목록 | `remote_ip` 매처 |
| 비밀번호 | 아이디·비밀번호 (Shelf가 해시) | `authentication` (basic) |
| CORS | 허용 출처 · 메서드 | 응답 헤더 + `OPTIONS` 응답 |

**성능**

| 설정 | 화면 | Caddy가 하는 일 |
|---|---|---|
| 압축 | 켜기 (기본 켬) | `encode` zstd · gzip |
| 캐시 | 경로 패턴 + 기간 | `Cache-Control` |
| 타임아웃 | 연결 · 응답 | `transport.dial_timeout` 등 |
| 업로드 크기 제한 | MB | `request_body.max_size` |

**헤더** — Caddy가 유명한 이유

| 설정 | 화면 | Caddy가 하는 일 |
|---|---|---|
| 응답 헤더 | 추가 · 덮어쓰기 · 삭제 목록 | `headers.response` |
| 업스트림으로 보낼 헤더 | 추가 · 삭제 목록 | `headers.request` |
| Host 헤더 | 그대로 전달(기본) / 업스트림 주소로 | `header_up Host` |

**운영**

| 설정 | 화면 | Caddy가 하는 일 |
|---|---|---|
| 점검 모드 | 켜기 + 안내 문구 + 통과시킬 IP | `static_response` 503 |
| 에러 페이지 | 502/503/404 HTML | `errors` 라우트 |
| 리다이렉트 규칙 | 원래 경로 → 새 경로, 코드 | `static_response` 301/308 |
| 접근 로그 | 켜기/끄기 | `logs` |

### 3.2 기능 하나 = 파일 하나

렌더러는 거대한 if 문이 아니라 **기능 모듈의 조합**이다. 각 기능은 사이트 설정을 받아 자기 몫의 Caddy 조각만 낸다.

```ts
interface SiteFeature {
  readonly key: string
  contribute(site: Site): Contribution   // before 핸들러 · proxy 옵션 · after 핸들러 · TLS 정책
}

const FEATURES: SiteFeature[] = [
  maintenance, ipAccess, basicAuth,     // 먼저 막을 것
  redirects, securityHeaders, hsts, cors,
  customHeaders, cacheRules, compression,
  upstream,                             // 마지막에 실제로 보낸다
]
```

- **웹서버 기능 하나 추가 = 파일 하나 + 화면 칸 하나.** 다른 기능을 건드리지 않는다.
- 순서가 곧 의미다 — 점검 모드는 접근 제어보다 앞, 압축은 프록시 바로 앞.
- 기능마다 단위 테스트가 생긴다 (설정 → 기대하는 Caddy 조각).

이 방식은 클라이언트 런타임(`ui/runtime.ts`)의 프리미티브와 같은 생각이다 — 화면은 의도만 선언하고, 번역은 한 곳이 한다.

### 3.3 탈출구 — 고급 설정 (Caddyfile)

화면이 모든 것을 담을 수는 없다. Caddy의 "커스텀이 편하다"를 잃지 않도록 **사이트마다 Caddyfile 조각을 직접 넣는 칸**을 둔다.

```
header /assets/* Cache-Control "public, max-age=31536000"
@bot header User-Agent *bot*
respond @bot 403
```

- Caddy의 `POST /adapt`가 Caddyfile을 JSON으로 바꿔준다 — Shelf가 문법을 구현하지 않는다 ([사용성 분석](caddy-usability.md) 실측 5)
- 나온 라우트는 그 사이트의 `subroute` **안에만** 들어간다 — 다른 사이트나 전역 설정에는 손댈 수 없다
- `/adapt`가 거부하면 저장도 거부하고, Caddy의 오류(줄 번호 포함)를 그대로 보여준다
- 관리 소켓·TLS 전역 설정 같은 불변식 영역은 렌더러가 따로 그리므로 고급 칸으로 깰 수 없다

JSON이 아니라 Caddyfile인 이유: Caddy를 아는 사람의 지식과 인터넷의 예제를 **그대로** 쓸 수 있기 때문이다.

### 3.4 프리셋

새 서비스를 만들 때 고른다. 프리셋은 위 설정값의 묶음일 뿐, 별도 로직은 없다.

| 프리셋 | 켜지는 것 |
|---|---|
| 기본 | HTTPS 자동 · HTTP→HTTPS · 압축 |
| 보안 강화 | 기본 + HSTS · 보안 헤더 · CSP 기본값 |
| API | 기본 + CORS · 캐시 끔 · 업로드 크기 제한 |
| 정적 사이트 | 기본 + 정적 파일 장기 캐시 |
| 내부용 | 기본 + IP 허용 목록 · 비밀번호 |

---

## 4. 반드시 지킬 불변식

아래는 실제 Caddy(v2.11.6)로 확인한 것이다 (부록 참고).

1. **관리 API는 유닉스 소켓으로만 연다.**
   TCP(`:2019`)로 열면 같은 `shelf-net`에 있는 **모든 앱 컨테이너가 프록시 전체를 바꿀 수 있다.** 배포한 앱 하나가 다른 모든 도메인과 인증서를 가로챌 수 있다는 뜻이다.
   소켓은 Caddy와 Shelf만 마운트하는 볼륨에 둔다.

2. **렌더 결과에는 항상 관리 소켓 설정이 들어간다.**
   빠진 설정을 밀어넣으면 Caddy는 200으로 받아들이고, 관리 API가 Caddy 내부 `localhost:2019`로 옮겨간다. Shelf는 즉시 제어를 잃고,
   그 설정이 autosave에 저장되므로 **Caddy를 재시작해도 돌아오지 못한다.** 트래픽은 계속 흐르니 겉보기엔 멀쩡하다.
   → 렌더러가 항상 넣고, 전송 직전에 한 번 더 확인하고, 테스트로 고정한다.

3. **`:80`과 `:443`은 서버를 나누고, 리다이렉트는 Shelf가 직접 그린다.**
   둘을 한 서버에 묶으면 Caddy가 리다이렉트를 만들지 않고 HTTP로 그냥 서빙한다 (실측).
   나누고 `disable_redirects`를 켜면 사이트별 "HTTP→HTTPS" 설정이 정확히 그대로 동작한다.

4. **유효한 인증서가 있을 때만 HTTPS로 넘긴다.** (F-61의 일반화)
   발급이 아직이거나 실패한 도메인을 HTTPS로 넘기면 그 사이트는 열리지 않는다. 관리 도메인이라면 자기 서버에서 잠긴다.
   → (v2 M4-1) 판단은 `siteMapBuilder` 한 곳 — `CertificateReader`가 읽은 인증서가 지금 그 주소를 덮을 때만 `RedirectHTTP`.
   설정 쓰기는 지도가 넘기라고 한 주소에만 `:80`에서 `307`을 그리고, 인증서 확인 경로(`/.well-known/acme-challenge/*`)는 넘기지 않는다.
   인증서 저장소를 읽지 못하면 "없음"으로 본다 — 넘기기가 꺼질 뿐 HTTP로는 열린다.

5. **비밀은 Caddy 설정에 넣지 않는다.**
   설정은 `autosave.json`에 평문으로 남는다. 업로드 인증서는 파일 경로(`load_files`)로, DNS 토큰은 `{env.CF_API_TOKEN}` 같은 환경변수 자리표시자로 넘긴다.
   (덤으로 F-41 "시크릿 평문 저장"이 일부 풀린다.)
   → (v2 M5) **예외 하나:** 기본 인증(비밀번호 보호)은 Caddy가 직접 검사해야 해서 bcrypt **해시**를 설정에 넣는다. 원문은 어디에도 남지 않고, 화면·로그에는 해시도 나가지 않는다.

6. **Caddy 컨테이너 이름에 `shelf-`를 쓰지 않는다.**
   Shelf는 `shelf-`로 시작하는 컨테이너를 앱으로 본다 (`listContainers('shelf-')`). 이름은 `caddy`.

---

## 5. 인증서

### 5.1 모드

| 화면 | Caddy | 비고 |
|---|---|---|
| **자동** (기본) | 서버 `:443` 라우트에 호스트가 있으면 알아서 발급 | Let's Encrypt, 실패 시 ZeroSSL로 넘어감. **클릭이 필요 없다** |
| DNS | 정책에 `acme` + `dns` 챌린지 | 와일드카드용. 커스텀 이미지 필요 (§8) |
| 내부 | 정책에 `internal` 발급자 | 자체서명 대체. LAN·테스트용 |
| 업로드 | `tls.certificates.load_files` + 자동 발급 제외 | 파일만 넘긴다 (불변식 5) |
| 끔 | `:443`에 넣지 않음 | HTTP만 |

### 5.2 상태 읽기

Caddy 관리 API는 발급된 인증서 목록을 주지 않는다. 대신 저장소를 읽는다 (읽기 전용 마운트):

```
/data/caddy/certificates/<발급자>/<도메인>/<도메인>.crt
  예: certificates/acme-v02.api.letsencrypt.org-directory/blog.example.com/blog.example.com.crt
      certificates/local/site.test/site.test.crt                ← 내부 발급
      certificates/<발급자>/wildcard_.example.com/...             ← 와일드카드
```

`node:crypto`의 `X509Certificate`로 만료일·발급자·SAN을 읽는다. 지금의 `ssl.covers(domain)`은 **이것을 보도록 바뀌지만 의미는 같다** — 그래서 `PublicAddress`와 관리 도메인 정책은 손대지 않는다.

### 5.3 실패를 사람 말로

Caddy는 발급 진행과 실패를 로그(`tls.obtain`)에만 남긴다. Shelf는 그 오류를 읽어 도메인 옆에 원인을 적는다.

| Caddy 로그에서 보이는 것 | 화면에 쓰는 말 |
|---|---|
| DNS 조회 결과가 이 서버가 아님 | DNS가 아직 이 서버를 가리키지 않습니다 |
| 80 포트 연결 실패 | 바깥에서 80 포트로 들어오지 못합니다 (방화벽) |
| 요청 한도 초과 | 잠시 발급이 막혔습니다 — 자동으로 다시 시도합니다 |

실제 오류 문구는 실서버(staging)에서 실패 사례를 모아 채운다 ([사용성 분석](caddy-usability.md) §7).

### 5.4 이벤트

기존 이벤트를 그대로 쓴다. **발행 주체만** `SslManager`에서 인증서 감시자로 바뀐다.

| 이벤트 | 언제 |
|---|---|
| `proxy:cert-issued` | 도메인의 인증서가 처음 생김 |
| `proxy:cert-renewed` | 만료일이 앞으로 갱신됨 |
| `proxy:cert-renewal-failed` | 만료 7일 전인데 갱신 안 됨, 또는 자동 모드인데 일정 시간 지나도 발급 안 됨 |
| `proxy:cert-removed` | 인증서가 사라짐 |

알림 시스템과 관리 도메인 HTTPS 강제는 이 이벤트를 이미 듣고 있으므로 **바뀌는 게 없다.**

### 5.5 달라지는 점

- **"갱신" 버튼이 사라진다.** Caddy가 알아서 한다.
- **자체서명 → 내부 CA.** 지금은 10년짜리 인증서 하나지만, Caddy 내부 CA는 12시간짜리를 자동으로 계속 갱신한다 (실측: 발급 시각 + 12시간 만료). 브라우저 경고를 없애려면 루트 인증서를 한 번 신뢰시키면 된다.

---

## 6. 코드 — 무엇이 사라지고 생기나

**사라짐**

| 파일 | 이유 |
|---|---|
| `proxy/proxy-server.ts` (266줄) | 트래픽은 Caddy가 |
| `proxy/issuers/*`, `proxy/dns/cloudflare.ts` | 발급은 Caddy가 |
| `SslManager`의 발급·갱신 | 위와 같음. 업로드 파일 저장만 남음 |
| 접근 로그 기록·로테이션 | Caddy가 쓰고 굴린다 (`roll_size_mb`, `roll_keep`) |
| 의존성 `acme-client` | **런타임 의존성 4개 → 3개** |

**새로 생김** — Caddy를 아는 코드는 전부 한 디렉터리에

```
core/src/system/proxy/engine/
├── engine.ts              ProxyEngine 인터페이스 (Shelf가 엔진에 기대하는 것)
└── caddy/
    ├── config.ts          CaddyConfig.render(sites, certs) — 순수 함수
    ├── features/          기능 하나 = 파일 하나 (§3.2)
    ├── admin.ts           CaddyAdmin — 유닉스 소켓으로 load / get
    ├── ledger.ts          CertificateLedger — 저장소에서 인증서 상태 읽기
    ├── access-log.ts      AccessLogReader — JSON 로그 파일 읽기
    └── engine.ts          CaddyEngine — 맞추기(reconcile), 인증서 감시, 이벤트
caddy/
├── Dockerfile             caddy:2 + DNS 플러그인 (§8)
└── bootstrap.json         관리 소켓만 여는 최소 설정
```

**그대로**: 화면·API 경로(필드만 추가), 앱 ↔ 도메인 자동 연결(`proxy:register-host` / `release-target`), `PublicAddress`, 알림.

### OBJECTS.md에 들어갈 문장

- **ProxyEngine** — 도메인·업스트림·웹서버 설정의 의도를 실제 트래픽 처리기에 반영하고, 그 결과(인증서 상태·접근 기록)를 되읽는다.
- **CaddyConfig** — DB의 의도를 Caddy JSON 한 장으로 번역한다. 같은 입력이면 같은 출력. 관리 소켓 설정은 사용자가 건드릴 수 없는 자리에 항상 그린다.
- **SiteFeature 구현체** — 웹서버 기능 하나를 자기 몫의 Caddy 조각으로 바꾼다. 다른 기능을 모른다.
- **CaddyAdmin** — 관리 소켓으로 설정을 넣고 읽는다. 사이트를 모른다. 보내기 직전에 관리 소켓 설정이 들어 있는지 확인한다.
- **CertificateLedger** — Caddy 저장소를 읽어 "이 도메인에 쓸 수 있는 인증서가 있는가"에 답한다.
- **AccessLogReader** — Caddy가 남긴 접근 로그를 화면이 쓰는 모양으로 읽는다.
- **CaddyEngine** — 원하는 상태와 적용된 상태를 맞춘다. 바뀔 때마다, 그리고 주기적으로.

아키텍처 테스트에 하나 추가한다: **Caddy 설정 어휘(`"handler"`, `reverse_proxy` 등)는 `engine/caddy/` 밖에 나오면 실패.**

---

## 7. 데이터

전부 **추가만** 한다 (롤백 가능하도록).

| 변경 | 내용 |
|---|---|
| `proxy_hosts.tls_mode` | `auto` · `dns` · `internal` · `manual` · `off` |
| `proxy_hosts.settings` | §3의 사이트 설정 JSON (`{ "v": 1, ... }`). TS 타입 + 검증 함수 |
| 기존 `ssl_enabled` · `force_ssl` · `hsts_*` | `settings`로 옮겨 읽되 **컬럼은 남긴다** (이전 버전으로 되돌릴 때 필요) |
| 관리 도메인 타깃 | `127.0.0.1:81` → `shelf:81`. 부팅 때 upsert가 하므로 별도 이관 불필요 |
| `127.0.0.1:X` 타깃 | 렌더할 때 `host.docker.internal:X`로 번역. 사용자에게 `127.0.0.1`은 "이 서버"라는 뜻이므로 DB는 그대로 둔다 |
| `access_logs` 테이블 | 더 쓰지 않음. 지우지도 않음 |

---

## 8. 배치

```yaml
services:
  caddy:
    build: ./caddy                       # caddy:2 + cloudflare · duckdns 플러그인
    container_name: caddy                # shelf- 접두사 금지 (불변식 6)
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"                    # HTTP/3
    volumes:
      - caddy-admin:/run/caddy           # 관리 소켓 — Caddy와 Shelf만
      - ./data/caddy/data:/data          # 인증서
      - ./data/caddy/config:/config      # autosave.json (--resume 용)
      - ./data/caddy/logs:/var/log/caddy
      - ./data/caddy/manual:/etc/caddy/manual:ro   # 업로드 인증서 파일
    env_file: [{ path: .env, required: false }]    # DNS 토큰은 여기로
    extra_hosts: ["host.docker.internal:host-gateway"]
    command: caddy run --resume --config /etc/caddy/bootstrap.json
    networks: [shelf-net]

  shelf:
    ports:
      - "127.0.0.1:81:81"                # 80/443은 이제 Caddy 것
      - "127.0.0.1:9100:9100"
    volumes:
      - caddy-admin:/run/caddy
      - ./data/caddy/data:/caddy-data:ro
      - ./data/caddy/logs:/caddy-logs:ro
      # ... 기존 docker.sock, ./data, ./ 마운트

volumes:
  caddy-admin:
```

- Caddy는 `docker.sock`을 받지 않는다. 컨테이너를 몰라도 된다.
- 백업 단위는 여전히 `data/` 하나다.
- 기본 `caddy:2` 이미지에는 DNS 플러그인이 하나도 없다 (실측). 와일드카드가 필요 없다면 `image: caddy:2`로 충분하다.
- 플러그인이 필요하면 **화면에서 고르고 Shelf가 이미지를 빌드한다** (`caddy:2-builder` + `xcaddy build --with ...`). Shelf는 이미 `docker build`를 하는 도구이므로 새 능력이 필요 없다. 사용자가 xcaddy를 배울 필요가 없다.

---

## 9. 검증

| 무엇 | 어떻게 |
|---|---|
| 렌더러 | 골든 테스트 — 사이트 설정 픽스처 → 기대 JSON. 기능 모듈마다 단위 테스트 |
| 불변식 | 모든 픽스처 출력에 관리 소켓 설정 존재 · `:80`/`:443` 분리 · 인증서 없으면 리다이렉트 없음 · 비밀 문자열 없음 |
| Caddy 계약 | Docker가 있으면 실제 Caddy에 `POST /load` — 거부되면 실패. 라우팅·리다이렉트·HSTS·404·Host 보존을 curl로 확인 (부록의 스파이크를 자동화) |
| 인증서 장부 | `openssl`로 만든 인증서 픽스처 (와일드카드 이름 포함) |
| 실서버 | **Let's Encrypt staging으로 먼저.** 운영 CA는 마지막에 |

---

## 10. 단계

| 단계 | 내용 | 끝났다고 말하는 기준 |
|---|---|---|
| **P0 계약** | `ProxyEngine` 인터페이스. 기존 코드를 그 뒤로. 테스트용 가짜 엔진 | 동작 변화 없음, `npm test` 통과 |
| **P1 번역기** | `CaddyConfig.render` + 기본 기능(연결·HTTPS·HSTS·압축) | 모든 픽스처가 실제 Caddy에 적용됨 |
| **P2 연결** | `CaddyAdmin` · 맞추기 루프 · compose에 `caddy` 추가 · 내부 인증서 | 로컬에서 앱 배포 → 도메인 → Caddy 경유 응답, 불변식 테스트 통과 |
| **P3 인증서** | 자동 발급 · 장부 · 이벤트 · 관리 도메인 리다이렉트 조건 · 업로드 · DNS | 실서버(staging)에서 **새 앱 도메인이 클릭 없이** 인증서를 받음 |
| **P4 웹서버 설정** | §3 나머지 기능 모듈 + 화면 + 프리셋 + 고급 칸 | 각 설정을 켰을 때 실제 응답 헤더·동작이 바뀜 (curl로 확인) |
| **P5 전환** | 기존 데이터 이관 · 문서 · 옛 코드와 `acme-client` 제거 | 님 서버 전환 완료, 롤백 리허설 완료 |

P1~P3이 끝나면 지금과 같은 기능을 Caddy 위에서 하게 된다. **P4가 이 설계의 진짜 목적**이다.

---

## 11. 롤백

- 마이그레이션은 추가만 하므로 이전 버전이 같은 DB로 뜬다
- 기존 `data/ssl/` 인증서는 지우지 않는다
- 되돌리기: 이전 커밋으로 `git checkout` → `docker compose up -d --build`
- 관리 API를 잃었을 때 (불변식 2가 깨졌을 때):
  ```bash
  docker compose exec caddy rm /config/caddy/autosave.json && docker compose restart caddy
  ```
  부트스트랩 설정으로 소켓이 다시 열리고, Shelf가 1분 안에 설정을 다시 밀어넣는다.

### 하지 않기로 한 것

- **두 엔진을 플래그로 병행** — 화면의 SSL 흐름이 엔진마다 달라 두 벌을 유지해야 한다. 롤백은 이전 이미지로 충분하다.
- **Caddyfile을 화면에서 편집** — 화면의 설정과 텍스트 설정이 서로 덮어쓰게 된다. 원본은 하나여야 한다. 대신 사이트 단위 고급 칸(§3.3)을 둔다.
- 여러 서버 · Caddy 클러스터링 · on-demand TLS.

---

## 12. 정해야 할 것

1. **와일드카드가 필요한가?** 필요하면 DNS 플러그인 커스텀 이미지(duckdns·cloudflare). 아니면 `caddy:2` 그대로.
2. **HTTP/3를 켤 것인가?** 서버 방화벽(클라우드 보안 그룹 포함)에서 **UDP 443**을 열어야 한다.
3. **자체서명 → 내부 CA 변경을 받아들일 것인가?** (§5.4)
4. **DNS 토큰을 화면 입력 대신 `.env`로 옮기는 것.** 보안은 좋아지지만 화면에서 바꿀 수 없게 된다.
5. **P4 기능의 우선순위.** 헤더 · 접근 제어 · 경로 라우팅 · 로드밸런싱 중 무엇부터.

---

## 부록 — 실제 Caddy로 확인한 것

Caddy v2.11.6, 로컬 Docker. Shelf 역할을 하는 별도 컨테이너(`node:20-alpine`)에서 공유 볼륨의 유닉스 소켓으로 접근했다.

| 확인한 것 | 결과 |
|---|---|
| 관리 API를 유닉스 소켓으로만 열기 | 소켓 생성, TCP 2019 연결 거부 |
| 다른 컨테이너에서 소켓으로 `POST /load` | 200, `GET /config/admin` → 소켓 주소 유지 |
| 컨테이너 이름으로 프록시 | `spike-app:4023` 응답 |
| Host 헤더 보존 | 업스트림이 원래 Host 수신, `X-Forwarded-For` · `X-Forwarded-Proto: https` 설정됨 |
| HTTPS 프로토콜 | HTTP/2 |
| `:80` · `:443` 한 서버로 묶기 | **리다이렉트 안 됨** — HTTP로 그대로 200 |
| 서버 분리 + 직접 그린 308 | `308 → https://site.test/a?b=1` (경로·쿼리 보존) |
| SSL 끈 호스트 | HTTP로 그대로 프록시 |
| HSTS 헤더 핸들러 | `strict-transport-security: max-age=63072000` |
| 등록 안 된 호스트 | 지정한 404 본문 |
| 접근 로그 | JSON — `request.host` · `method` · `uri` · `remote_ip` · `headers.User-Agent` · `status` · `duration`(초) |
| 인증서 저장 위치 | `/data/caddy/certificates/local/site.test/site.test.crt` |
| 내부 CA 인증서 수명 | 12시간 |
| autosave 위치 | `/config/caddy/autosave.json` (`/data`가 아님 — 따로 마운트해야 함) |
| **관리 소켓 설정 없는 설정 적용** | 200으로 받아들임 → 즉시 제어 상실 → **재시작해도 회복 안 됨** (autosave에 저장됨) |

**아직 확인 못 한 것** (실서버에서 P3 때): Let's Encrypt HTTP-01 발급(분리된 `:80` 서버에서 챌린지 처리), HTTP/3(UDP 443), DNS 플러그인의 환경변수 자리표시자.
