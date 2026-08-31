# Shelf

> 서버 한 대의 프록시를 편하게 쓰려고 만들었습니다.

[English](README.en.md) · [소개 페이지](https://kangminna.github.io/shelf/) · MIT

도메인 하나 붙이려고 nginx 설정 파일을 고치고, certbot 크론을 걸고, 포트가 겹치지 않게 표를 그려 관리하는 일 —
그게 싫어서 만든 도구입니다. Shelf는 **80/443을 물고 있는 리버스 프록시**이고,
도메인·SSL·업스트림을 화면에서 관리합니다.

앱 배포는 그 위에 얹힌 기능입니다. 프록시가 어차피 컨테이너를 알아야 하니, 이왕이면 컨테이너를 띄우는 것까지 맡긴 것입니다.

---

## 프록시

![프록시 호스트 목록](site/screenshots/proxy.png)

도메인을 적고 어디로 보낼지 고르면 끝입니다.

- **컨테이너 이름으로 직접 연결** — 앱과 프록시가 같은 Docker 네트워크에 있어서 `shelf-blog:80`처럼 바로 갑니다.
  호스트 포트를 열 필요도, 포트 번호를 외울 필요도 없습니다.
- **Let's Encrypt** — HTTP-01 또는 Cloudflare DNS-01. 와일드카드도 되고 갱신은 매일 확인합니다.
- **인증서가 생기면 HTTPS 강제** — 80은 301로 넘기고 HSTS를 켭니다. 인증서를 지우면 되돌아가므로 자기 서버에서 잠기지 않습니다.
- **WebSocket 그대로 통과** — 업그레이드 이후로는 바이트를 해석하지 않고 흘립니다.
- **접근 로그** — 도메인별 상태코드·응답시간. 기본 14일 보관 후 자동 정리.
- **외부에 여는 포트는 80·443뿐** — 관리 화면도 프록시 뒤에 있습니다.

![SSL 인증서](site/screenshots/ssl.png)

---

## 앱 배포

![앱 상세](site/screenshots/appdetail.png)

앱의 계약은 하나입니다 — **저장소 루트에 `Dockerfile`이 있고, 컨테이너가 포트 하나로 HTTP를 서빙하면 됩니다.**
언어도 프레임워크도 DB도 앱이 알아서 하면 됩니다.

- **Git이든 이미지든** — 저장소를 clone해 `docker build`하거나, Docker Hub·GHCR 이미지를 그대로 pull합니다.
- **모노레포** — 빌드 경로에 `site`나 `apps/web`을 적으면 그 폴더에서 빌드합니다. 저장소 밖으로 나가는 경로는 거부합니다.
- **push하면 배포** — 웹훅 주소와 시크릿을 발급해주니 GitHub 설정에 붙여넣기만 하면 됩니다. HMAC으로 검증합니다.
- **배포 이력과 롤백** — 커밋·시각·결과·전체 빌드 로그가 남고, 예전 커밋으로 다시 빌드합니다.
- **도메인 자동 등록** — 앱에 도메인을 적으면 프록시 항목이 함께 생기고, 앱을 지우면 함께 사라집니다.

---

## 감시와 알림

![알림](site/screenshots/notify.png)

- **호스트와 앱의 지표** — CPU·로드·메모리·디스크 여유와 앱별 CPU·메모리. 백그라운드에서 표본을 뜨므로 화면이 기다리지 않습니다.
- **상태가 바뀔 때만 알림** — 앱이 죽으면 한 번, 돌아오면 한 번. 직접 Stop한 앱은 장애로 보지 않습니다.
- **웹훅으로 발송** — JSON POST. Discord·Slack의 incoming webhook URL도 그대로 씁니다.
  시크릿을 넣으면 `x-shelf-signature-256`에 HMAC-SHA256 서명이 붙고, 보낸 결과가 기록됩니다.

![대시보드](site/screenshots/dashboard.png)

---

## 설치

Docker가 있는 리눅스 서버에서 두 줄입니다.

```bash
git clone https://github.com/KangminNa/shelf && cd shelf
docker compose up -d --build
```

관리 화면은 첫 접속에서 계정을 만듭니다. `.env`에 도메인을 적으면 부팅할 때 프록시에 자동 등록됩니다.

```bash
ADMIN_DOMAIN=shelf.example.com
ACME_EMAIL=you@example.com
```

개발용으로 돌릴 때:

```bash
npm install
npm run dev          # http://localhost:9666/admin
npm test             # 96개
```

계정을 잊었다면 서버 셸에서:

```bash
docker compose exec shelf npm run admin passwd admin '새 비밀번호'
docker compose exec shelf npm run admin reset     # 계정 전체 삭제 → /setup 다시 열림
```

---

## 첫 앱 올리기

1. **Apps → New app** — Git URL(저장소에 `Dockerfile` 필요) 또는 Docker 이미지 이름, 컨테이너 포트, 도메인(선택)
2. **Deploy** — clone → build → 컨테이너 실행 (`shelf-{이름}`, `--restart unless-stopped`)
3. **웹훅** — 앱 상세의 Payload URL과 Secret을 GitHub → Settings → Webhooks에 붙여넣기. 이후 push마다 자동 배포
4. **SSL** — Proxy → SSL에서 그 도메인 인증서를 발급하면 https로 전환됩니다

최소 예제는 [`examples/hello-app/`](examples/hello-app/)에 있습니다.

---

## 소개 페이지

[kangminna.github.io/shelf](https://kangminna.github.io/shelf/) — 이 저장소의 `site/` 를 GitHub Pages 로 올린 것입니다.

`site/index.html` 은 스크린샷까지 data URI 로 품은 **단일 파일**이라 어디에 두든 그대로 열립니다.
스크린샷을 다시 찍었다면:

```bash
python3 site/build.py <스크린샷 디렉터리>   # template.html + 스크린샷 → index.html
```

이 페이지를 Shelf 자신으로 배포할 수도 있습니다. compose가 저장소를 `/shelf` 로 마운트하므로 GitHub을 거치지 않아도 됩니다.

**Apps → New app** 에서:

| 칸 | 값 |
|---|---|
| Git repository URL | `/shelf` |
| Branch | `main` |
| 빌드 경로 | `site` |
| Container port | `80` |
| Domain | `www.내도메인` |

Deploy를 누르면 `site/Dockerfile` 로 빌드되고, 도메인은 프록시에 자동 등록됩니다.
부팅할 때 자동으로 만들어지지는 않습니다 — 남의 서버에 소개 페이지를 멋대로 띄우지 않기 위해서입니다.

---

## 마음대로 바꿔 쓰세요

MIT입니다. fork해서 고치든, 필요한 부분만 떼어 쓰든, 사내 도구로 만들든 상관없습니다.
프로젝트 자체가 "내 서버에 맞게 내가 고치는 것"을 전제로 만들어져 있습니다.

고치기 쉽게 하려고 지킨 것들:

- **런타임 의존성 4개** — `hono`, `@hono/node-server`, `better-sqlite3`, `acme-client`. 빌드 도구도, 프론트엔드 프레임워크도 없습니다.
- **HTML은 서버가 만듭니다** — React도 번들러도 없습니다. 화면은 `views.ts`의 클래스 하나고, 브라우저 동작은 `ui/runtime.ts`가 전부 담당합니다.
- **날 SQL은 `db/`에만** — 도메인 코드는 `Repository<T>`만 씁니다.
- **시스템끼리 import하지 않습니다** — auth·deploy·proxy·notify는 EventBus로만 이야기합니다.
- **문서가 코드를 강제합니다** — [OBJECTS.md](docs/OBJECTS.md)에 클래스마다 역할 한 문장이 있고, 문서에 없는 클래스가 생기면 `npm test`가 실패합니다.

읽는 순서: [SPEC](docs/SPEC.md) 무엇을 제공하는가 · [OBJECTS](docs/OBJECTS.md) 누가 무엇을 하는가 ·
[ARCHITECTURE](docs/ARCHITECTURE.md) 어떤 모양인가 · [PROCESS](docs/PROCESS.md) 어떻게 만드는가 ·
[HISTORY](docs/HISTORY.md) 왜 이렇게 생겼는가 · [DEVELOPMENT](docs/DEVELOPMENT.md) 클래스별 안내

```
core/src/
├── kernel/          ShelfApplication — 조립·라우팅 · Controller — 응답 규약
├── system/
│   ├── proxy/       프록시 서버(SNI·ACME), SSL 발급자, 컨트롤러
│   ├── deploy/      앱 저장소, 빌드 파이프라인, 컨테이너, 웹훅, 감시자
│   ├── notify/      알림 채널과 발송 이력
│   └── auth/        세션과 계정
├── db/              AppDatabase → Repository<T> → QueryBuilder<T>
├── services/        EventBus · Logger · Scheduler · HostMetrics
├── ui/              엘리먼트 빌더 · 페이지 · 클라이언트 런타임
└── admin/           대시보드·시스템·설정 화면
```

---

## 운영할 때 알아둘 것

- Shelf는 `/var/run/docker.sock`을 마운트합니다. 호스트 Docker를 전부 제어할 수 있다는 뜻이고, 따라서 **관리자 계정은 설계상 root와 동급**입니다.
  본인 서버에서만 쓰고, 서버를 맡길 만한 사람에게만 계정을 주세요.
- 앱을 배포한다는 건 남의 코드를 내 서버에서 실행한다는 뜻입니다. 믿는 저장소만 올리세요.
- 시크릿(Git 토큰, 웹훅 시크릿, DNS 토큰)은 `data/`에 저장됩니다. 그 디렉터리를 보호하세요 — 권한, 디스크 암호화, 백업 관리.
- 권장 배포: **80/443만 외부에 노출**. `ADMIN_DOMAIN`을 설정하면 관리 화면이 프록시를 거쳐 SSL로 서빙되고,
  compose가 관리 UI(81)와 웹훅(9100)을 127.0.0.1에 묶어두므로 프록시 밖으로는 나가지 않습니다.
- 들어 있는 방어: scrypt 세션 인증, 로그인 5회 실패 시 15분 잠금, Secure/httpOnly/SameSite 쿠키,
  HMAC 웹훅 검증과 본문 크기 제한, CORS 없음, git/이미지 입력값 검증, API 응답과 로그에서 시크릿 제거.

---

## License

MIT © Kangmin Na
