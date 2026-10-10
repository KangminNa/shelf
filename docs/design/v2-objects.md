# Naru v2 — 객체 설계

상태: **승인 · R0(코드 옮기기) 끝** · 2026-10-08 승인, 2026-10-10 R0 반영 — 달라진 점은 §12.1
관련: [v2 계획](v2.md) · [웹서버 엔진](caddy-engine.md)

이 문서는 순서가 중요하다.
**① Naru가 하는 일을 먼저 정하고 → ② 이름 짓는 규칙을 정하고 → ③ 하는 일마다 객체를 나눈다.**
객체 이름만 보고 "무엇을 하는지" 한 문장이 떠올라야 한다.

---

## 1. Naru가 하는 일

사용자 눈으로 본 Naru의 일은 열한 가지다. 모든 객체는 이 중 하나에 속한다.

| | 하는 일 | 사용자가 보는 것 |
|---|---|---|
| **A** | 관리자로 들어오기 | 첫 설정, 로그인, 비밀번호 바꾸기, 셸에서 되찾기 |
| **B** | 서버 설정 정하기 | 관리 주소, 인증서 연락처 |
| **C** | 서비스 관리하기 | 서비스 올리기·고치기·지우기, 주소 붙이기·떼기 |
| **D** | 종류마다 다르게 다루기 | 저장소·이미지·정적 사이트·외부 연결이 각자 다르게 만들어지고 서빙된다 |
| **E** | 배포하기 | 새 버전 만들기 → 끊김 없이 바꿔 끼우기, 되돌리기, 멈추기·켜기 |
| **F** | push 받아 배포하기 | 웹훅 확인, 브랜치 확인, 배포 시작 |
| **G** | 웹서버 맞추기 | 주소마다 어디로 보낼지 정하고 웹서버(Caddy)에 반영 |
| **H** | 보여주기 | 홈, 서비스 상세, 배포 기록, 서버 상태 |
| **J** | 지켜보기 | 서비스·웹서버·Docker 상태를 30초마다 보고 바뀌면 알린다. 문제와 고칠 방법, 서비스 CPU·메모리 (M6) |
| **K** | 알리기 | 일어난 일(서비스 멈춤·복구, 배포 실패, 인증서 곧 끝남, 웹서버·Docker 끊김)을 등록한 알림 주소로 보낸다 (M6) |
| **I** | 웹서버 설정 정하기 | 서비스마다 헤더·IP 제한·비밀번호·점검 중·경로별 연결·고급(Caddyfile), nginx 설정 가져오기 (M5) |

그리고 이 일들이 바깥 세계(Docker, git, Caddy, DB, 파일, 네트워크)를 쓰는 데 필요한 **도구**가 있다.

---

## 2. 이름 짓는 규칙

1. **인터페이스 이름 = 하는 일.** "무엇을 하는 것"으로 짓는다. `ImagePuller`(이미지를 받는 것), `PortChecker`(포트가 응답하는지 보는 것).
2. **구현 이름 = 어떻게 하는지.** `ScryptHasher`, `GitDownloader`, `SocketSender`. 같은 일을 다른 방식으로 하면 이름으로 구분된다.
3. **저장하는 것은 모두 `…Store`**, 읽기만 필요한 곳에는 `…Reader`를 준다.
4. **바깥 프로그램 이름(Docker·Caddy·git)은 구현에만 붙인다.** 인터페이스는 하는 일로 — 그래야 바꿔 끼울 수 있다.
5. **데이터 이름은 화면에서 쓰는 말과 같게.** 화면이 "버전·배포·주소"라고 하면 코드도 `Version`·`Deployment`·`Domain`.
6. **쓰지 않는 말:** `Handler`(HTTP 밖에서), `Processor`, `Helper`, `Util`, `Info`, `Data`, `Plan`, `Ledger`, `Registry`, `Roles` — 뜻이 넓어서 이름만 보고 하는 일을 알 수 없다.

---

## 3. 원칙 세 가지

1. **하는 일 하나 = 객체 하나.** 책임을 한 문장으로 말할 수 없으면 쪼갠다.
2. **객체 사이의 관계는 인터페이스로만.** 어떤 객체도 다른 객체의 구체 타입을 모른다. 구체 타입을 아는 곳은 조립하는 `app` 하나뿐이다.
3. **주고받는 것은 데이터뿐.** 넘기는 값은 `model`의 데이터(행동 없는 값)다.

```
                  contract   ← 인터페이스만. 모든 관계가 여기 한 곳에 보인다
                ▲    ▲    ▲
   access · settings · services · kinds · deploy · webhook · webserver · 도구들 · web
                ▼
                  model      ← 데이터. 표준 라이브러리 말고는 모른다
   app = 유일하게 구체 타입을 안다. 만들어서 인터페이스로 꽂는다
```

**`app`을 뺀 모든 패키지는 `model`과 `contract`만 import한다.** 서로를 볼 수 없으니 협력은 반드시 인터페이스를 거친다.

---

## 4. 하는 일마다 객체

표의 **인터페이스**가 그 객체를 바깥에서 보는 유일한 모습이다. 구현은 괄호 안.

### A. 관리자로 들어오기 — `access`

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `PasswordHasher` | 비밀번호를 해시하고, 맞는지 본다 (v1과 같은 방식) | `ScryptHasher` |
| `LoginLimiter` | 같은 IP에서 계속 틀리면 잠근다 | `MemoryLoginLimiter` |
| `SetupKey` | 첫 설정 열쇠(토큰)를 만들고, 계정이 없을 때만 맞는지 본다 | `RandomSetupKey` |
| `LoginManager` | 로그인·로그아웃하고, 세션의 주인이 누구인지 알려준다 | `loginManager` |
| `AccountManager` | 첫 계정을 만들고, 비밀번호를 바꾸고, 셸에서 되찾거나 초기화한다 | `accountManager` |

### B. 서버 설정 정하기 — `settings`

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `AdminDomainSetting` | 관리 주소를 정하고 알려준다. **"환경 변수가 화면보다 우선"은 여기에만 있다** | `adminDomainSetting` |
| `CertEmailSetting` | 인증서 연락처 이메일을 정하고 알려준다 (같은 우선 규칙) | `certEmailSetting` |
| `SetupProgress` | 첫 설정을 마쳤는지 기록하고 알려준다 | `setupProgress` |

### C. 서비스 관리하기 — `services`

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `ServiceEditor` | 서비스를 만들고·고치고, 주소를 붙이고 뗀다 (지우기는 컨테이너·파일까지 함께라 `ServiceControl`) | `serviceEditor` |
| `ServiceLauncher` | 서비스를 만들고 첫 배포까지 한 번에 한다 | `serviceLauncher` |
| `NameChooser` | 이름을 비워 두면 지어 주고, 겹치는지 본다 | `nameChooser` |
| `DomainChecker` | 주소가 다른 서비스나 관리 화면과 겹치는지 본다 | `domainChecker` |

### D. 종류마다 다르게 다루기 — `kinds`

종류(저장소·이미지·정적 사이트·외부 연결)마다 다른 것은 **다섯 가지**다. 종류 하나는 이 다섯 담당자의 묶음이다.

| 인터페이스 | 하는 일 | 저장소 | 이미지 | 정적 사이트 | 외부 연결 |
|---|---|---|---|---|---|
| `InputChecker` | 이 종류에 필요한 값이 다 있는지 본다 | `RepoInput` | `ImageInput` | `StaticInput` | `ExternalInput` |
| `VersionBuilder` | 새 버전을 만든다 | `RepoBuilder` (저장소 → 이미지 빌드) | `ImageFetcher` (이미지 받기) | `StaticBuilder` (폴더 → 배포본) | — |
| `VersionSwapper` | 새 버전을 띄우고(Swap), 웹서버가 새 것을 가리킨 뒤 옛 것을 내린다(Retire) | `ContainerSwapper` | `ContainerSwapper` | `FolderSwapper` | — |
| `DestinationFinder` | 웹서버가 요청을 보낼 곳을 알려준다 | `ContainerAliasDestination` 또는 `ContainerIPDestination` (설치 방식에 따라 `app`이 고른다) | 같음 | `FolderDestination` | `ExternalDestination` |
| `StatusReader` | 화면에 보일 상태를 읽는다 | `ContainerStatus` | `ContainerStatus` | `StaticStatus` | `ExternalStatus` |

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `KindLookup` | 종류 이름으로 그 종류의 담당자 묶음(`KindTools`)을 찾아 준다 | `kindLookup` |

**무중단 교체는 `ContainerSwapper` 한 곳에만 있다** — 저장소와 이미지가 함께 쓴다.

### E. 배포하기 — `deploy`

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `Deployer` | 배포를 정해진 순서로 하고, 예전 버전으로 되돌린다 | `deployer` |
| `DeployLock` | 서비스마다 배포를 한 번에 하나만 하게 하고, 그 사이에 온 요청은 한 번으로 합친다 | `memoryDeployLock` |
| `DeployLog` | 배포 기록을 열고 1초마다 저장한다 | `deployLog` |
| `DeployLogWriter` | 배포 기록에 쓴다. `Step`은 화면이 단계로 읽는 "▶ 한국어 / English" 한 줄 | `deployLog`가 연 것 |
| `OldVersionCleaner` | 되돌리기용으로 최근 버전만 남기고 오래된 것을 지운다 | `oldVersionCleaner` |
| `ServiceControl` | 서비스를 멈추고, 켜고, (컨테이너·이미지·파일째) 지운다 | `serviceControl` |

### F. push 받아 배포하기 — `webhook`

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `HookReceiver` | 웹훅을 확인하고 배포를 맡기거나 거절한다 | `hookReceiver` |
| `SignatureChecker` | 웹훅이 진짜 보낸 곳에서 왔는지 본다. 차례로 시도한다 | `GitHubSignature` → `GitLabToken` → `QuerySecret` |
| `BranchFilter` | 등록한 브랜치의 push인지 본다 | `branchFilter` |

### G. 웹서버 맞추기 — `webserver`

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `SiteMapBuilder` | 서비스·관리 주소·인증서 상태로 "주소마다 어디로 보낼지" 사이트 지도를 만든다 | `siteMapBuilder` |
| `WebServerSync` | 사이트 지도를 웹서버에 맞춘다 — 바뀌었다는 알림을 받으면 바로, 평소엔 30초마다 | `webServerSync` |
| `ConfigWriter` | 사이트 지도를 웹서버 설정으로 쓴다 | `caddy.JSONWriter` |
| `ConfigSender` | 설정을 웹서버에 보낸다 | `caddy.SocketSender`, 그것을 감싸 관리 소켓 설정이 빠졌는지 먼저 보는 `caddy.AdminSocketGuard` |
| `CertificateReader` | 웹서버가 받아 둔 인증서를 읽는다 — 공개 인증서(`.crt`)만, 키는 열지 않는다. 사이트 지도는 이것으로 "HTTPS로 넘겨도 되는가"를 정한다 (불변식 4) | `caddy.CertificateFiles` |

### H. 보여주기 — `views`

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `ServiceViewer` | 화면에 보여줄 서비스 모습(홈 카드·상세·배포 기록·로그)을 모은다. 비밀은 담지 않는다. 로그는 앱 출력과 받은 요청을 시간순으로 합친다 (M6-2) | `serviceViewer` |
| `ServerStats` | 서버의 CPU·메모리·디스크를 알려준다 | `stats.ProcSampler` |

### I. 웹서버 설정 정하기 — `websettings` (M5)

서비스 하나의 웹서버 설정은 그 서비스의 모든 주소에 똑같이 적용된다. 관리 주소에는 적용하지 않는다.
한 요청이 지나가는 순서: **IP 제한 → 점검 중 → 비밀번호 → 헤더·압축 → 고급 → 경로별 연결 → 서비스 기본 목적지.** 압축은 칸 없이 늘 켠다.

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `WebSettingsEditor` | 설정을 적용한다 — 저장하고 바로 웹서버에 맞춰 보고, 거절되면 되돌린다 | `webSettingsEditor` |
| `NginxTranslator` | nginx server 블록을 설정 칸으로 옮긴다. 옮기지 못한 줄과 이유를 함께 | `NginxReader` |
| `LoginHasher` | 기본 인증 비밀번호를 bcrypt로 해시한다 | `BcryptHasher` |

**잘못된 설정 하나가 웹서버 전체를 멈추게 하지 않는다.** 틀린 설정이 저장돼 있으면 이후 모든 맞추기가 실패한다 — 그래서 `Apply`는 저장 직후 `WebServerSync.SyncNow`로 맞춰 보고, 거절되면 저장을 되돌린다.

### 도구 — 바깥 세계에 닿는 것 (각자 자기 패키지)

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `ImageBuilder` | 빌드할 폴더로 이미지를 만든다 | `docker.Builder` |
| `ImagePuller` | 이미지를 받고 그 ID·열어둔 포트를 알려준다 | `docker.Puller` |
| `ImageCleaner` | 이미지가 있는지 보고, 지운다 | `docker.Images` |
| `ContainerStarter` | 컨테이너를 만들어 띄운다 | `docker.Containers` |
| `ContainerRemover` | 컨테이너를 없앤다 | `docker.Containers` |
| `ContainerSwitch` | 컨테이너를 멈추고 켠다 | `docker.Containers` |
| `ContainerWatcher` | 컨테이너 상태·로그, 이 서비스의 컨테이너 목록을 본다 | `docker.Containers` |
| `ContainerLogReader` | 컨테이너가 찍은 최근 줄을 시각·stdout/stderr와 함께 읽는다 | `docker.Containers` |
| `AccessLogReader` | 웹서버가 남긴 요청 기록 파일에서 그 주소들의 최근 요청을 읽는다 (파일 끝 4MB만) | `caddy.AccessLogFile` |
| `AlertSender` | 알림 하나를 그 주소의 형식으로 보낸다 (10초 제한, 응답 본문은 읽고 버린다) | `httppost.AlertPoster` |
| `SnippetCompiler` | 고급 칸의 Caddyfile 지시어를 웹서버 형식으로 바꾼다 — 그 사이트의 경로 처리만 꺼내고 나머지는 "적용되지 않음"으로 알린다 | `caddy.AdaptCompiler` (Caddy 관리 API `/adapt`) |
| `CodeDownloader` | 저장소 코드를 내려받는다 (토큰은 인자·설정 파일에 남기지 않는다) | `git.Downloader` |
| `WorkFolder` | 잠깐 쓸 작업 폴더를 빌려주고 돌려받는다. 그 안의 경로를 심볼릭 링크로 빠져나가지 않게 찾아 준다 | `files.TempFolders` |
| `BuildContextPacker` | 빌드할 폴더를 묶는다 (`.git`·`.dockerignore` 제외) | `files.TarPacker` |
| `DockerfilePortReader` | Dockerfile의 `EXPOSE`에서 앱 포트를 찾는다 | `files.DockerfileReader` |
| `SiteFiles` | 정적 사이트 파일을 올리고·복사하고·지운다 (숨김 파일 제외) | `files.SiteFolders` |
| `PortChecker` | 그 주소의 포트가 응답하는지 본다 | `netcheck.TCP` |
| `DNSChecker` | 도메인이 이 서버를 가리키는지 본다 | `netcheck.DNS` |
| `EventPublisher` · `EventSubscriber` | 일어난 일을 알리고, 듣는다 | `events.Bus` |
| `Clock` · `RandomTokens` | 지금 시각, 난수 문자열 | `system.Clock`, `system.Random` |

### J. 지켜보기 — `watch` (M6)

30초마다 본다. **바뀔 때만** 이벤트를 낸다 — 듣는 쪽(알리기)이 알아서 보낸다 (Observer).

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `HealthWatcher` | 마지막으로 본 것(멈춘 서비스·웹서버·Docker·본 시각)을 알려준다. 도는 고리는 만들 때 함께 돌려준다 | `healthWatcher` |

**알림 폭탄을 막는 규칙:** 두 번 연속(약 1분) 문제일 때만 멈춤으로 본다 · 직접 멈춘 서비스와 배포 중인 서비스는 보지 않는다 ·
Naru가 막 켜졌을 때 이미 멈춰 있던 것은 알리지 않고 기준으로만 삼는다 · 인증서 곧 끝남은 주소마다 하루 한 번.
지켜보는 것은 **컨테이너 서비스**뿐이다 — 정적 사이트는 웹서버가 직접 서빙하고, 외부 연결은 감시하지 않기로 했다.

### K. 알리기 — `notify` (M6)

| 인터페이스 | 하는 일 | 구현 |
|---|---|---|
| `AlertSettings` | 알림 주소를 등록·삭제·시험하고, 보낸 결과를 보여준다. 화면에는 주소를 가린 모습만 준다 | `alerts` |

이벤트를 들으면 줄에 넣고, 따로 도는 일꾼이 주소마다 보낸다 — 보내기가 느려도 배포·지켜보기를 막지 않는다.
형식은 주소로 고른다: Discord(`discord.com/api/webhooks`) → `{"content"}`, Slack(`hooks.slack.com`) → `{"text"}`,
그 밖 → v1과 같은 JSON(`event`·`level`·`title`·`detail`·`service`·`sent_at`), 시크릿이 있으면 `X-Naru-Signature-256`(HMAC-SHA256).
**알림 주소는 비밀이다** (Discord 주소는 그 자체로 비밀번호) — 화면에는 `discord.com/…`처럼 가리고, 로그에 남기지 않는다.

### 설치 방식 — Docker · 설치형 (`app`만 안다)

같은 바이너리를 Docker(compose)로도, 호스트에 직접(systemd)도 설치한다. 화면과 기능은 같고 **바깥에 닿는 방법만** 다르다.
방식은 `NARU_INSTALL=docker|host`(기본 `host`)로 정하고, 그 값을 아는 곳은 `app` 하나다 — `app`이 방식에 맞는 값과 구현을 골라 끼운다 (R12).

| | Docker | 설치형 |
|---|---|---|
| Caddy → 앱 컨테이너 | `ContainerAliasDestination` — 별칭:포트 (같은 네트워크) | `ContainerIPDestination` — 컨테이너 IP:포트 (리눅스 호스트는 브리지 IP에 닿는다) |
| Caddy → "이 서버" | `host.docker.internal` | `127.0.0.1` (`ExternalDestination`이 그릴 때 바꾼다 — 저장된 값은 그대로) |
| Caddy → 관리 화면 · Naru가 듣는 곳 | `naru:8080` · `:8080` | `127.0.0.1:8080` · `127.0.0.1:8080` |
| 정적 사이트 · 인증서 저장소 | `/srv/sites` · 공유 볼륨 | 데이터 폴더의 `sites` · 데이터 폴더의 `caddy` (설정에 저장 위치를 적는다) |
| 실행 · 관리 소켓 | compose 컨테이너 둘 · 둘만 마운트하는 볼륨 | systemd 서비스 둘(같은 `naru` 사용자) · `/run/naru/caddy.sock` (0600) |

컨테이너 IP는 재시작하면 바뀔 수 있다 — `siteMapBuilder`가 맞출 때마다 `ContainerWatcher`로 지금 IP를 읽어 덮어쓴다 (못 읽으면 배포 때 기록한 IP).
응답 확인은 두 방식 모두 IP로 한다 (Naru는 Docker 방식에서도 앱 네트워크에 있다).

**무중단 교체 순서 (두 방식 모두):** 새 것 띄움 → 응답 확인 → 지금 상태 저장 → `WebServerSync.SyncNow` → 옛 것 내림(`VersionSwapper.Retire`).
웹서버가 새 것을 가리키기 전에는 옛 것을 내리지 않는다 — 맞추기가 실패하면 둘 다 남겨 두고 다음 배포에서 정리한다.

### 저장 — `store` (SQLite, SQL은 여기에만)

| 인터페이스 | 하는 일 |
|---|---|
| `AccountStore` · `AccountReader` | 계정을 저장한다 · 읽는다 |
| `SessionStore` | 로그인 세션을 저장하고 찾는다 |
| `SettingStore` | 서버 설정 값을 저장한다 |
| `ServiceStore` · `ServiceReader` | 서비스를 저장한다 · 읽는다 |
| `DomainStore` | 서비스에 붙은 주소를 저장한다 |
| `SecretStore` | 서비스의 비밀(토큰·env·웹훅 시크릿)을 저장한다 |
| `LiveStateStore` | 지금 도는 것(컨테이너·서빙 폴더·포트·멈춤)을 저장한다 |
| `DeployHistoryStore` · `DeployHistoryReader` | 배포 기록을 저장한다 · 읽는다 |
| `HookLogStore` | 웹훅을 받은 기록을 저장한다 |
| `WebSettingsStore` | 서비스의 웹서버 설정을 저장한다 (비밀번호는 bcrypt 해시만) |
| `ChannelStore` · `DeliveryLog` | 알림 주소(비밀)를 저장한다 · 보낸 결과를 남긴다 (최근 200개) |

### 들어오는 쪽

| 패키지 | 하는 일 |
|---|---|
| `web` | HTTP 요청을 위 인터페이스 호출로 바꾸고, 결과·오류 코드를 화면으로 바꾼다. **업무 판단은 하지 않는다** |
| `cli` | 서버 셸의 복구 명령을 `AccountManager` 호출로 바꾼다 |

---

## 5. 데이터 — `model`

화면에서 쓰는 말과 같은 이름을 쓴다. 다른 객체를 가리키지 않는 값이다 (인터페이스·함수 필드가 없다 — R3).

### 값 객체 — 만들 때 한 번 검사한다

| 이름 | 보장하는 것 |
|---|---|
| `ServiceName` | 영문 소문자·숫자·`-` 31자 이내. 비워 두면 `SuggestServiceName`이 짓고 겹치면 `WithSuffix` |
| `DomainName` | 공개 DNS 이름 모양. 붙여 넣은 `https://…/`·대문자·끝의 `.`는 정리된다 |
| `RepoURL` | git이 옵션이나 로컬 파일로 오해할 수 없는 http(s) 주소 |
| `Branch` | `-`로 시작하지 않고 `..`이 없는 브랜치 이름. 비면 기본(`main`) |
| `ImageRef` | Docker 이미지 이름 모양 |
| `ExternalAddress` | `호스트:포트`. `localhost`·`127.0.0.1`은 웹서버 컨테이너 기준 주소로 바뀐다 |
| `FolderPath` | 저장소 안을 벗어나지 않는 경로 (`..` 없음) |
| `Port` | 1~65535. 0은 "모름" |
| `Email` | 인증서 연락처 이메일 |
| `Username` | 계정 이름 — 영문·숫자·`.-_` 2~32자 |
| `EnvVars` · `Volumes` | `KEY=value` 줄들 · `/서버경로:/컨테이너경로[:ro]` 줄들. 저장된 글은 `Stored…`로 그대로 읽고, 틀린 줄은 `List`가 건너뛴다 (v1에서 온 값을 잃지 않게) |
| `KindName` | 서비스 종류 이름. `ParseKindName`은 아는 이름인지만 본다 — 종류마다 다른 행동은 `kinds`만 안다 |
| `ServiceID` · `DomainID` · `DeploymentID` · `AccountID` | 저장소가 준 번호 |

`ParseDomainName(s) (DomainName, error)`를 지나지 않은 문자열은 `DomainName`이 될 수 없다. 그래서 저장·배포·웹서버는 **이미 검사된 값만** 받는다.
틀리면 `InputError{Field, Code}`가 나온다. 문구는 화면이 `err.`+Code로 고른다.

### 계정

| 이름 | 무엇 |
|---|---|
| `Account` | 관리자 한 명 — 번호와 이름 |
| `PasswordHash` | `salt:hash` (v1과 같은 scrypt 모양). **`web`·`cli`는 이 타입을 쓸 수 없다** |
| `SessionToken` · `SessionDigest` | 쿠키에만 있는 세션 원문 · 저장하는 그 SHA-256 |

### 서비스와 배포

| 이름 | 무엇 |
|---|---|
| `ServiceInput` | 화면에서 받은 서비스 값 (값 객체들) |
| `NewService` · `ServiceSettings` | 저장할 새 서비스 · 고칠 수 있는 값 |
| `Service` | 서비스 하나의 지금 모습 — 비밀은 없다 |
| `ServiceSecrets` | 배포에만 쓰는 비밀(env·볼륨·git 토큰·웹훅 시크릿). **`web`·`cli`는 이 타입을 쓸 수 없다** |
| `Domain` · `DomainInput` | 서비스에 붙은 주소 하나 · 붙일 주소 |
| `LiveState` | 지금 도는 것 — 서비스 별칭, 요청을 받는 컨테이너와 그 IP, 서빙 중인 배포본, 포트, 직접 멈췄는지 |
| `HookLog` | 웹훅을 마지막으로 받은 때와 결과 |
| `Version` | 배포할 수 있게 만든 것 — 이미지 ID 또는 배포본 폴더, 커밋, 찾은 포트 |
| `Commit` · `ImageID` · `ImageDetails` | 가져온 커밋 · Docker 이미지 ID · 받은 이미지의 ID와 열어둔 포트 |
| `SiteFolder` | 정적 사이트 배포본 하나 (서비스 이름 + 배포 번호) |
| `CodeSource` | 내려받을 코드 — 저장소·브랜치·토큰 |
| `ContainerSpec` · `ContainerState` · `ContainerStates` | 띄울 컨테이너 · 컨테이너 하나의 상태(앱 네트워크에서의 지금 IP 포함) · 이름별 상태 (nil이면 Docker를 읽지 못함) |
| `Deployment` | 배포 한 번의 기록 |
| `DeployStatus` | 진행 중 · 성공 · 실패 |
| `DeployReason` | 왜 배포했나 — 직접 · push · 처음 올림 · 되돌림 |
| `BuildRequest` · `SwapRequest` | 새 버전 만들기 · 바꿔 끼우기에 넘기는 값 |

### 웹서버·웹훅

| 이름 | 무엇 |
|---|---|
| `Destination` | 요청을 보낼 곳 — 컨테이너:포트 / 폴더 / 외부 주소 |
| `Site` | 사이트 지도의 한 줄 — 주소들, 목적지, HTTPS, HSTS, HTTP를 HTTPS로 넘기는지(`RedirectHTTP` — 인증서가 있을 때만) |
| `SiteMap` | 웹서버에 줄 지도 — 사이트들, 관리 주소(와 그 넘기기), 관리 소켓, 모르는 주소 처리, 바깥 HTTPS 포트 |
| `WebServerStatus` | 웹서버에 닿는지, 마지막 오류, 마지막으로 맞춘 때 |
| `Certificate` · `Certificates` | 웹서버가 받아 둔 인증서 하나(덮는 이름·발급자·기간) · 전부. `Certificates.For(도메인, 지금)`이 그 도메인의 상태를 찾는다 (와일드카드는 한 단계) |
| `CertificateState` | 도메인 하나의 인증서 상태 — 있음(만료일·발급자) · 아직 없음. `Usable`이면 HTTP를 HTTPS로 넘긴다, `EndsSoon`은 14일 안에 끝남. 실패 이유는 M4-2 |
| `DNSAnswer` | 도메인이 이 서버를 가리키는지의 답 |
| `SetBy` | 설정 값을 누가 정했나 — 환경 변수 · 화면 · 아무도 |
| `HookRequest` · `HookResult` | 받은 웹훅(헤더·쿼리·본문) · 처리 결과(HTTP 상태·문구·배포 번호) |
| `InputError` | 어느 칸이 왜 틀렸나 (코드 — 문구는 화면이 만든다) |

### 웹서버 설정 (M5)

| 이름 | 무엇 |
|---|---|
| `WebSettings` | 서비스 하나의 웹서버 설정 — 헤더, 허용 IP, 기본 인증, 점검 중, 경로 규칙, 고급(원문과 바꾼 것). **`web`·`cli`는 이 타입을 쓸 수 없다** (해시가 들어 있다) |
| `HeaderRule` | 응답 헤더 하나 — 값이 비면 지운다 |
| `BasicLogin` | 기본 인증 계정 — 아이디와 bcrypt 해시. **`web`·`cli`는 이 타입을 쓸 수 없다** |
| `PathRoute` · `PathPrefix` | 경로 규칙 — `/api`를 다른 서비스나 외부 주소로, 앞부분 떼기 · `/`로 시작하는 경로 앞부분 (`..`·`*` 없음, `/` 하나는 안 됨) |
| `WebSettingsInput` · `PathRouteInput` | 화면에서 받은 설정 (비밀번호는 원문 — 저장 전에 해시) · 경로 규칙 (대상은 서비스 이름이나 주소) |
| `WebSettingsView` | 화면에 보일 설정 — 비밀번호는 "있음"만, 이 서비스로 보내는 다른 서비스 목록 |
| `NginxImport` · `SkippedLine` | nginx 설정을 옮긴 결과 · 옮기지 못한(또는 필요 없는) 줄과 이유 |
| `SiteSettings` · `SitePath` | 사이트 지도에 실리는 설정 — 경로 목적지가 이미 실제 주소로 바뀐 것 |
| `RefusedError` | 웹서버가 설정을 받아들이지 않았다 — 이유는 웹서버가 한 말 그대로 |

### 지켜보기 · 알리기 (M6)

| 이름 | 무엇 |
|---|---|
| `WatchSnapshot` · `DownService` | 마지막으로 본 것 — 멈춘 서비스(왜), 웹서버·Docker에 닿는지, 본 시각 · 멈춘 서비스 하나 |
| `Alert` · `AlertLevel` | 보낼 알림 — 사건 이름, 문제·복구·알림, 제목, 내용, 서비스 이름 |
| `AlertChannel` · `ChannelID` | 알림 주소 — 이름, 주소, 시크릿. **`web`·`cli`는 이 타입을 쓸 수 없다** (주소가 비밀) |
| `AlertURL` · `ChannelInput` · `ChannelView` | http(s) 주소 값 · 화면에서 받은 것 · 화면에 보일 것(가린 주소, 형식, 시크릿이 있는지) |
| `Delivery` | 보낸 결과 하나 — 주소 이름, 사건, 제목, 성공 여부, 이유, 시각 |

### 로그 (M6-2)

| 이름 | 무엇 |
|---|---|
| `LogLine` · `LogSource` | 로그 한 줄 — 시각, 앱/요청, 앱이면 stdout/stderr와 글자, 요청이면 메서드·경로·상태·걸린 시간 |
| `LogFilter` · `LogsView` | 전체·앱·요청 중 무엇을 볼지 · 서비스 로그 화면 (읽지 못한 쪽은 이유와 함께) |

**접근 로그에 남기지 않는 것:** 관리 주소와 주소 없이 들어온 요청(웹훅의 `?secret=`, 첫 설정의 `?token=`) — 서비스 주소만 남긴다 (`skip_unmapped_hosts`).
`Authorization`·`Cookie`는 Caddy가 `REDACTED`로 남긴다 (실측). 앱 주소의 쿼리 문자열은 그대로 남는다 — 내 서버의 로그다.

### 화면에 보여줄 모습 (`ServiceViewer`가 모은다)

| 이름 | 무엇 |
|---|---|
| `ServiceStatus` | 화면 상태 — 문구 키(실행 중 · 멈춤 · 죽음 · 컨테이너 없음 · 파일 서빙 …)와 색 |
| `ServiceCard` · `HomeView` | 홈의 카드 하나 · 홈 화면 (Docker에 닿지 못했는지 포함) |
| `ServiceView` · `ServiceForm` · `WebhookView` | 서비스 상세 · 설정 폼에 채울 값(토큰 원문 없음, 있는지만) · 웹훅 주소와 시크릿 |
| `DomainView` | 서비스 상세의 주소 하나와 그 인증서 상태 |
| `DeploymentView` · `StepView` | 배포 화면 · 배포 단계 하나 |
| `ServerSnapshot` | 서버의 CPU·메모리·디스크 |

### 일어난 일 (이벤트) — `Event`

| 이름 | 언제 |
|---|---|
| `SiteMapChanged` | 주소·포트·서빙 폴더·관리 주소·첫 설정 상태가 바뀌었을 때 → `WebServerSync`가 바로 맞춘다 |
| `DeployFinished` | 배포가 끝났을 때 (성공·실패) |
| `ServiceDeleted` | 서비스를 지웠을 때 |
| `ServiceDown` · `ServiceUp` | 컨테이너 서비스가 (두 번 연속) 멈췄을 때 · 다시 응답할 때 |
| `WebServerDown` · `WebServerUp` · `DockerDown` · `DockerUp` | 웹서버·Docker에 (두 번 연속) 닿지 않을 때 · 다시 닿을 때 |
| `CertificateEndingSoon` | 인증서가 14일 안에 끝날 때 (주소마다 하루 한 번) |

---

## 6. contract — 인터페이스 전부

> 인터페이스마다 **메서드 다섯 개 이하** (R4). 넘으면 읽기/쓰기처럼 하는 일로 쪼갠다.
> 모든 메서드는 `context.Context`를 먼저 받는다 (메모리 안에서 끝나는 것은 빼고).
> 아래는 `internal/contract/contract.go` 그대로다 — 둘이 다르면 코드가 맞고, 이 문서를 고친다.

```go
// ── A. 관리자로 들어오기 ─────────────────────

// PasswordHasher는 비밀번호를 해시하고, 맞는지 본다.
type PasswordHasher interface {
	Hash(password string) (model.PasswordHash, error)
	Matches(password string, h model.PasswordHash) bool
}

// LoginLimiter는 같은 IP에서 계속 틀리면 잠근다.
type LoginLimiter interface {
	Allowed(ip string) bool
	Failed(ip string)
	Succeeded(ip string)
}

// SetupKey는 첫 설정 열쇠를 만들고, 계정이 없을 때만 맞는지 본다.
type SetupKey interface {
	Value() string
	Matches(ctx context.Context, key string) bool
}

// LoginManager는 로그인·로그아웃하고, 세션의 주인이 누구인지 알려준다.
type LoginManager interface {
	LogIn(ctx context.Context, ip, user, password string) (model.SessionToken, error)
	WhoIs(ctx context.Context, t model.SessionToken) (model.Account, bool)
	LogOut(ctx context.Context, t model.SessionToken)
}

// AccountManager는 첫 계정을 만들고, 비밀번호를 바꾸고, 셸에서 되찾거나 초기화한다.
type AccountManager interface {
	CreateFirst(ctx context.Context, setupKey string, user model.Username, password string) (model.SessionToken, error)
	ChangePassword(ctx context.Context, who model.Account, current, next string, keep model.SessionToken) error
	Recover(ctx context.Context, user, password string) error
	ResetAll(ctx context.Context) error
	Names(ctx context.Context) ([]string, error)
}

// AccountReader는 계정을 읽는다.
type AccountReader interface {
	Count(ctx context.Context) (int, error)
	FindByName(ctx context.Context, user string) (model.Account, model.PasswordHash, error)
	Names(ctx context.Context) ([]string, error)
}

// AccountStore는 계정을 저장한다.
type AccountStore interface {
	CreateFirst(ctx context.Context, u model.Username, h model.PasswordHash) (model.AccountID, error)
	SetPassword(ctx context.Context, id model.AccountID, h model.PasswordHash) error
	DeleteAll(ctx context.Context) error
}

// SessionStore는 로그인 세션을 저장하고 찾는다.
type SessionStore interface {
	Save(ctx context.Context, d model.SessionDigest, who model.AccountID, until time.Time) error
	FindOwner(ctx context.Context, d model.SessionDigest, now time.Time) (model.Account, bool)
	Delete(ctx context.Context, d model.SessionDigest) error
	DeleteOthers(ctx context.Context, who model.AccountID, keep model.SessionDigest) error
}

// ── B. 서버 설정 정하기 ──────────────────────

// AdminDomainSetting은 관리 주소를 정하고 알려준다. 환경 변수가 화면보다 우선한다.
type AdminDomainSetting interface {
	Get(ctx context.Context) (model.DomainName, model.SetBy)
	Set(ctx context.Context, d model.DomainName) error // 빈 값이면 지운다
}

// CertEmailSetting은 인증서 연락처 이메일을 정하고 알려준다.
type CertEmailSetting interface {
	Get(ctx context.Context) (model.Email, model.SetBy)
	Set(ctx context.Context, e model.Email) error
}

// SetupProgress는 첫 설정을 마쳤는지 기록하고 알려준다.
type SetupProgress interface {
	Done(ctx context.Context) bool
	MarkDone(ctx context.Context) error
}

// SettingStore는 서버 설정 값을 저장한다.
type SettingStore interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
}

// ── C. 서비스 관리하기 ───────────────────────

// ServiceEditor는 서비스를 만들고·고치고, 주소를 붙이고 뗀다.
type ServiceEditor interface {
	Create(ctx context.Context, in model.ServiceInput) (model.ServiceID, error)
	Update(ctx context.Context, id model.ServiceID, in model.ServiceInput) (needsRedeploy bool, err error)
	AddDomain(ctx context.Context, id model.ServiceID, d model.DomainInput) error
	RemoveDomain(ctx context.Context, id model.ServiceID, d model.DomainID) error
}

// ServiceLauncher는 서비스를 만들고 첫 배포까지 한 번에 한다.
type ServiceLauncher interface {
	Launch(ctx context.Context, in model.ServiceInput) (model.ServiceID, model.DeploymentID, error)
}

// NameChooser는 이름을 비워 두면 지어 주고, 겹치는지 본다.
type NameChooser interface {
	Choose(ctx context.Context, in model.ServiceInput) (model.ServiceName, error)
}

// DomainChecker는 주소가 다른 서비스나 관리 화면과 겹치는지 본다.
type DomainChecker interface {
	Check(ctx context.Context, d model.DomainName) error
}

// ServiceReader는 서비스를 (주소와 함께) 읽는다.
type ServiceReader interface {
	List(ctx context.Context) ([]model.Service, error)
	Get(ctx context.Context, id model.ServiceID) (model.Service, error)
	NameTaken(ctx context.Context, n model.ServiceName) bool
}

// ServiceStore는 서비스를 저장한다.
type ServiceStore interface {
	Create(ctx context.Context, s model.NewService) (model.ServiceID, error)
	Update(ctx context.Context, id model.ServiceID, s model.ServiceSettings) error
	Delete(ctx context.Context, id model.ServiceID) error
}

// DomainStore는 서비스에 붙은 주소를 저장한다.
type DomainStore interface {
	FindOwner(ctx context.Context, d model.DomainName) (model.ServiceID, bool)
	Add(ctx context.Context, id model.ServiceID, d model.DomainInput) error
	Remove(ctx context.Context, id model.ServiceID, d model.DomainID) error
}

// SecretStore는 서비스의 비밀을 저장한다.
type SecretStore interface {
	Get(ctx context.Context, id model.ServiceID) (model.ServiceSecrets, error)
	Set(ctx context.Context, id model.ServiceID, s model.ServiceSecrets) error
}

// LiveStateStore는 지금 도는 것을 저장한다.
type LiveStateStore interface {
	Save(ctx context.Context, id model.ServiceID, s model.LiveState) error
}

// ── D. 종류마다 다르게 다루기 ─────────────────

// InputChecker는 이 종류에 필요한 값이 다 있는지 보고, 이 종류에 맞게 정리한다 (안 쓰는 칸 비우기, 기본 브랜치).
type InputChecker interface {
	Check(in model.ServiceInput) (model.ServiceInput, error)
}

// VersionBuilder는 새 버전을 만든다.
type VersionBuilder interface {
	Build(ctx context.Context, req model.BuildRequest, log DeployLogWriter) (model.Version, error)
}

// VersionSwapper는 새 버전으로 바꿔 끼운다. Swap은 새 것을 띄우고 응답까지만 확인한다 — 실패하면 지금 도는 것을 그대로 둔다.
// Retire는 웹서버가 새 것을 가리킨 뒤에 옛 것을 내린다 (그 전에 내리면 설치형에서 요청이 끊긴다).
type VersionSwapper interface {
	Swap(ctx context.Context, req model.SwapRequest, v model.Version, log DeployLogWriter) (model.LiveState, error)
	Retire(ctx context.Context, s model.Service, keep model.LiveState, log DeployLogWriter)
}

// DestinationFinder는 웹서버가 요청을 보낼 곳을 알려준다.
type DestinationFinder interface {
	Find(s model.Service) model.Destination
}

// StatusReader는 화면에 보일 상태를 읽는다.
type StatusReader interface {
	Read(s model.Service, containers model.ContainerStates) model.ServiceStatus
}

// KindTools는 종류 하나의 도구 묶음이다. 배포가 없는 종류는 Builder·Swapper가 nil.
type KindTools struct {
	Input       InputChecker
	Builder     VersionBuilder
	Swapper     VersionSwapper
	Destination DestinationFinder
	Status      StatusReader
}

// KindLookup은 종류 이름으로 그 종류의 도구 묶음을 찾아 준다.
type KindLookup interface {
	Find(k model.KindName) (KindTools, bool)
}

// ── E. 배포하기 ────────────────────────────

// Deployer는 배포를 정해진 순서로 하고, 예전 버전으로 되돌린다.
type Deployer interface {
	Deploy(ctx context.Context, id model.ServiceID, why model.DeployReason) (model.DeploymentID, error)
	RollBack(ctx context.Context, id model.ServiceID, to model.DeploymentID) (model.DeploymentID, error)
	IsDeploying(id model.ServiceID) bool
}

// DeployLock은 서비스마다 배포를 한 번에 하나만 하게 하고, 그 사이에 온 요청은 한 번으로 합친다.
type DeployLock interface {
	TryLock(id model.ServiceID, why model.DeployReason) (unlock func() (again model.DeployReason, ok bool), locked bool)
	IsLocked(id model.ServiceID) bool
}

// DeployLogWriter는 배포 기록을 쓴다. Step은 "▶ 한국어 / English" 한 줄이다.
type DeployLogWriter interface {
	io.Writer
	Step(ko, en string)
}

// DeployLog는 배포 기록을 쓰고 1초마다 저장한다.
type DeployLog interface {
	Open(d model.DeploymentID) (w DeployLogWriter, close func() (whole string))
}

// OldVersionCleaner는 되돌리기용으로 최근 버전만 남기고 오래된 것을 지운다.
type OldVersionCleaner interface {
	Clean(ctx context.Context, s model.Service, live model.DeploymentID)
}

// ServiceControl은 서비스를 멈추고, 켜고, (컨테이너·이미지·파일째) 지운다.
type ServiceControl interface {
	Stop(ctx context.Context, id model.ServiceID) error
	Start(ctx context.Context, id model.ServiceID) error
	Remove(ctx context.Context, id model.ServiceID) error
}

// DeployHistoryReader는 배포 기록을 읽는다.
type DeployHistoryReader interface {
	Get(ctx context.Context, d model.DeploymentID) (model.Deployment, error)
	Recent(ctx context.Context, id model.ServiceID, n int) ([]model.Deployment, error)
	Succeeded(ctx context.Context, id model.ServiceID) ([]model.DeploymentID, error)
}

// DeployHistoryStore는 배포 기록을 저장한다.
type DeployHistoryStore interface {
	Start(ctx context.Context, id model.ServiceID, why model.DeployReason) (model.DeploymentID, error)
	SaveLog(ctx context.Context, d model.DeploymentID, log string) error
	Finish(ctx context.Context, d model.Deployment) error
	CloseInterrupted(ctx context.Context, note string) (int, error)
}

// ── F. push 받아 배포하기 ─────────────────────

// HookReceiver는 웹훅을 확인하고 배포를 맡기거나 거절한다.
type HookReceiver interface {
	Receive(ctx context.Context, id model.ServiceID, r model.HookRequest) model.HookResult
}

// SignatureChecker는 웹훅이 진짜 보낸 곳에서 왔는지 본다. 자기 방식이 아니면 CanCheck가 거짓 — 다음 것이 본다.
type SignatureChecker interface {
	CanCheck(r model.HookRequest) bool
	IsGenuine(r model.HookRequest, secret string) bool
}

// BranchFilter는 등록한 브랜치의 push인지 본다.
type BranchFilter interface {
	Wanted(r model.HookRequest, s model.Service) (ok bool, why string)
}

// HookLogStore는 웹훅을 받은 기록을 저장한다.
type HookLogStore interface {
	Save(ctx context.Context, id model.ServiceID, l model.HookLog) error
}

// ── G. 웹서버 맞추기 ─────────────────────────

// SiteMapBuilder는 서비스·관리 주소로 "주소마다 어디로 보낼지" 사이트 지도를 만든다.
type SiteMapBuilder interface {
	Build(ctx context.Context) (model.SiteMap, error)
}

// WebServerSync는 사이트 지도를 웹서버에 맞춘다.
type WebServerSync interface {
	SyncNow(ctx context.Context) error
	Status() model.WebServerStatus
}

// ConfigWriter는 사이트 지도를 웹서버 설정으로 쓴다.
type ConfigWriter interface {
	Write(m model.SiteMap) ([]byte, error)
}

// ConfigSender는 설정을 웹서버에 보낸다.
type ConfigSender interface {
	Send(ctx context.Context, config []byte) error
	Ping(ctx context.Context) error
}

// CertificateReader는 웹서버가 받아 둔 인증서를 읽는다. 공개 인증서만 열고 키 파일은 열지 않는다.
// 읽지 못하면 오류와 함께 빈 목록 — 쓰는 쪽은 "인증서 없음"으로 본다 (HTTP로라도 열리는 쪽으로 무너진다).
type CertificateReader interface {
	Read(ctx context.Context) (model.Certificates, error)
}

// ── I. 웹서버 설정 정하기 ─────────────────────

// WebSettingsEditor는 서비스의 웹서버 설정을 적용한다. 저장한 뒤 바로 웹서버에 맞춰 보고,
// 웹서버가 거절하면 저장을 되돌리고 model.RefusedError로 그 이유를 돌려준다.
// warnings는 적용은 됐지만 알려야 할 것 — 고급 칸에서 사이트 밖이라 버린 지시어 같은 것.
type WebSettingsEditor interface {
	Apply(ctx context.Context, id model.ServiceID, in model.WebSettingsInput) (warnings []string, err error)
}

// WebSettingsStore는 서비스의 웹서버 설정을 저장한다. 없으면 빈 설정이다.
type WebSettingsStore interface {
	Get(ctx context.Context, id model.ServiceID) (model.WebSettings, error)
	Set(ctx context.Context, id model.ServiceID, s model.WebSettings) error
}

// NginxTranslator는 nginx server 블록을 웹서버 설정 칸으로 옮긴다. 저장하지 않는다 — 화면이 미리 보여주고 사람이 적용한다.
type NginxTranslator interface {
	Translate(text string) model.NginxImport
}

// LoginHasher는 기본 인증 비밀번호를 웹서버가 아는 해시(bcrypt)로 만든다.
type LoginHasher interface {
	Hash(password string) (string, error)
}

// ── J. 지켜보기 ────────────────────────────

// HealthWatcher는 지켜보기가 마지막으로 본 것을 알려준다. 도는 고리(30초마다)는 만들 때 함께 돌려준다.
// 바뀔 때만 이벤트(ServiceDown·ServiceUp·WebServerDown …)를 낸다 — 보내는 일은 알리기가 듣고 한다.
type HealthWatcher interface {
	Snapshot() model.WatchSnapshot
}

// ── K. 알리기 ─────────────────────────────

// AlertSettings는 알림 주소를 등록·삭제·시험하고, 보낸 결과를 보여준다. 화면에는 주소를 가린 모습만 준다.
type AlertSettings interface {
	Channels(ctx context.Context) ([]model.ChannelView, error)
	Add(ctx context.Context, in model.ChannelInput) error
	Remove(ctx context.Context, id model.ChannelID) error
	Test(ctx context.Context, id model.ChannelID) error
	Deliveries(ctx context.Context, n int) ([]model.Delivery, error)
}

// ChannelStore는 알림 주소를 저장한다. 주소와 시크릿은 비밀이다.
type ChannelStore interface {
	List(ctx context.Context) ([]model.AlertChannel, error)
	Add(ctx context.Context, c model.AlertChannel) (model.ChannelID, error)
	Remove(ctx context.Context, id model.ChannelID) error
}

// DeliveryLog는 보낸 결과를 남긴다 (최근 것만).
type DeliveryLog interface {
	Save(ctx context.Context, d model.Delivery) error
	Recent(ctx context.Context, n int) ([]model.Delivery, error)
}

// ── H. 보여주기 ────────────────────────────

// ServiceViewer는 화면에 보여줄 서비스 모습을 모은다. 비밀(토큰·비밀번호)은 담지 않는다.
type ServiceViewer interface {
	Home(ctx context.Context) (model.HomeView, error)
	Detail(ctx context.Context, id model.ServiceID) (model.ServiceView, error)
	Deployment(ctx context.Context, id model.ServiceID, d model.DeploymentID) (model.DeploymentView, error)
	Logs(ctx context.Context, id model.ServiceID, f model.LogFilter) (model.LogsView, error) // 앱 출력과 요청을 시간순으로
}

// ServerStats는 서버의 CPU·메모리·디스크를 알려준다.
type ServerStats interface {
	Now() model.ServerSnapshot
}

// ── 도구 ─────────────────────────────────

// ImageBuilder는 빌드할 폴더(묶은 것)로 이미지를 만든다.
type ImageBuilder interface {
	Build(ctx context.Context, buildFolder io.Reader, tag string, log io.Writer) (model.ImageID, error)
}

// ImagePuller는 이미지를 받고 그 ID·열어둔 포트를 알려준다.
type ImagePuller interface {
	Pull(ctx context.Context, ref model.ImageRef, log io.Writer) (model.ImageDetails, error)
}

// ImageCleaner는 이미지가 있는지 보고, 지운다.
type ImageCleaner interface {
	Exists(ctx context.Context, id model.ImageID) bool
	Remove(ctx context.Context, ref string) error
}

// ContainerStarter는 컨테이너를 만들어 띄우고, 앱 네트워크에서의 IP를 알려준다 (네트워크가 없으면 만든다).
type ContainerStarter interface {
	Start(ctx context.Context, spec model.ContainerSpec) (ip string, err error)
}

// ContainerRemover는 컨테이너를 없앤다.
type ContainerRemover interface {
	Remove(ctx context.Context, name string) error
}

// ContainerSwitch는 컨테이너를 멈추고 켠다.
type ContainerSwitch interface {
	TurnOff(ctx context.Context, name string) error
	TurnOn(ctx context.Context, name string) error
}

// ContainerWatcher는 컨테이너 상태·로그, 이 서비스의 컨테이너 목록을 본다.
type ContainerWatcher interface {
	All(ctx context.Context) (model.ContainerStates, error)
	One(ctx context.Context, name string) (model.ContainerState, error)
	Logs(ctx context.Context, name string, lines int) (string, error)
	BelongingTo(ctx context.Context, id model.ServiceID) ([]string, error)
}

// CodeDownloader는 저장소 코드를 내려받는다.
type CodeDownloader interface {
	Download(ctx context.Context, from model.CodeSource, into string, log io.Writer) (model.Commit, error)
}

// WorkFolder는 잠깐 쓸 작업 폴더를 빌려주고, 그 안의 경로를 안전하게 찾아 준다.
type WorkFolder interface {
	Borrow(name string) (path string, giveBack func(), err error)
	Inside(root string, p model.FolderPath) (string, error) // 심볼릭 링크로 밖에 나가면 실패
}

// BuildContextPacker는 빌드할 폴더를 묶는다 (.git·.dockerignore 제외).
type BuildContextPacker interface {
	Pack(folder string) io.ReadCloser
}

// DockerfilePortReader는 Dockerfile의 EXPOSE에서 앱 포트를 찾는다. Dockerfile이 없으면 found가 거짓.
type DockerfilePortReader interface {
	Ports(folder string) (ports []model.Port, found bool)
}

// SiteFiles는 정적 사이트 파일을 올리고·복사하고·지운다 (숨김 파일 제외).
type SiteFiles interface {
	Publish(from string, to model.SiteFolder) (count int, err error)
	Copy(from, to model.SiteFolder) error
	Exists(f model.SiteFolder) bool
	Prune(site model.ServiceName, keep int, live model.DeploymentID)
	RemoveSite(site model.ServiceName) error
}

// ContainerLogReader는 컨테이너가 찍은 최근 n줄을 시각과 함께 읽는다.
type ContainerLogReader interface {
	Recent(ctx context.Context, container string, n int) ([]model.LogLine, error)
}

// AccessLogReader는 웹서버가 남긴 요청 기록에서 그 주소들의 최근 n개를 읽는다. 아직 기록이 없으면 빈 목록.
type AccessLogReader interface {
	Recent(ctx context.Context, hosts []string, n int) ([]model.LogLine, error)
}

// AlertSender는 알림 하나를 그 주소의 형식(Discord·Slack·일반 JSON)으로 보낸다. 오래 걸리면 끊는다.
type AlertSender interface {
	Send(ctx context.Context, ch model.AlertChannel, a model.Alert) error
}

// SnippetCompiler는 고급 칸의 Caddyfile 지시어(사이트 블록 안쪽)를 웹서버 형식으로 바꾼다.
// 그 사이트의 경로 처리만 꺼내고, 그 밖(TLS·관리·포트·다른 사이트)은 ignored로 알려준다.
type SnippetCompiler interface {
	Compile(ctx context.Context, caddyfile string) (compiled []byte, ignored []string, err error)
}

// PortChecker는 그 주소의 포트가 응답하는지 본다.
type PortChecker interface {
	Answers(ctx context.Context, host string, port model.Port) error
}

// DNSChecker는 도메인이 이 서버를 가리키는지 본다.
type DNSChecker interface {
	PointsHere(ctx context.Context, d model.DomainName, thisServer string) model.DNSAnswer
}

// EventPublisher는 일어난 일을 알린다.
type EventPublisher interface {
	Publish(e model.Event)
}

// EventSubscriber는 일어난 일을 듣는다.
type EventSubscriber interface {
	Subscribe(listen func(model.Event))
}

// Clock은 지금 시각이다.
type Clock interface {
	Now() time.Time
}

// RandomTokens는 난수 문자열(16진수)이다.
type RandomTokens interface {
	New(bytes int) string
}
```

---

## 7. 누가 무엇을 쓰나

이 표가 그대로 `app`의 조립 코드다. 오른쪽은 **모두 인터페이스**다.

| 객체 | 쓰는 것 |
|---|---|
| `loginManager` | AccountReader · SessionStore · PasswordHasher · LoginLimiter · RandomTokens · Clock |
| `accountManager` | AccountReader · AccountStore · SessionStore · PasswordHasher · SetupKey · RandomTokens · Clock |
| `RandomSetupKey` | AccountReader · RandomTokens |
| `adminDomainSetting` · `certEmailSetting` · `setupProgress` | SettingStore · EventPublisher (+ 환경 변수 값은 만들 때 받는다) |
| `nameChooser` | ServiceReader |
| `domainChecker` | DomainStore · AdminDomainSetting |
| `serviceEditor` | ServiceReader · ServiceStore · DomainStore · SecretStore · NameChooser · DomainChecker · KindLookup · RandomTokens · EventPublisher |
| `serviceLauncher` | ServiceEditor · KindLookup · Deployer |
| `deployer` | DeployLock · DeployHistoryStore · DeployHistoryReader · DeployLog · ServiceReader · LiveStateStore · KindLookup · OldVersionCleaner · EventPublisher · WebServerSync |
| `serviceControl` | ServiceReader · ServiceStore · LiveStateStore · DeployLock · ContainerSwitch · ContainerRemover · ContainerWatcher · ImageCleaner · SiteFiles · DeployHistoryReader · EventPublisher |
| `RepoBuilder` | WorkFolder · CodeDownloader · SecretStore · DockerfilePortReader · BuildContextPacker · ImageBuilder |
| `ImageFetcher` | ImagePuller |
| `StaticBuilder` | WorkFolder · CodeDownloader · SecretStore · SiteFiles |
| `ContainerSwapper` | ContainerStarter · ContainerRemover · ContainerSwitch · ContainerWatcher · PortChecker · SecretStore · ImageCleaner |
| `FolderSwapper` | SiteFiles |
| `oldVersionCleaner` | DeployHistoryReader · ImageCleaner · SiteFiles |
| `hookReceiver` | ServiceReader · SecretStore · SignatureChecker(차례로) · BranchFilter · Deployer · HookLogStore · Clock |
| `siteMapBuilder` | ServiceReader · KindLookup · AdminDomainSetting · CertEmailSetting · SetupProgress · CertificateReader · Clock · WebSettingsStore · ContainerWatcher |
| `webServerSync` | SiteMapBuilder · ConfigWriter · ConfigSender(`AdminSocketGuard`로 감싼 것) · EventSubscriber |
| `serviceViewer` | ServiceReader · SecretStore · DeployHistoryReader · ContainerWatcher · KindLookup · AdminDomainSetting · Deployer · CertificateReader · Clock · WebSettingsStore · ContainerLogReader · AccessLogReader |
| `webSettingsEditor` | ServiceReader · WebSettingsStore · SnippetCompiler · LoginHasher · WebServerSync |
| `healthWatcher` | ServiceReader · KindLookup · ContainerWatcher · PortChecker · Deployer · WebServerSync · CertificateReader · AdminDomainSetting · Clock · EventPublisher |
| `alerts` | ChannelStore · DeliveryLog · AlertSender · ServiceReader · EventSubscriber · Clock |
| `web` | LoginManager · AccountManager · SetupKey · AdminDomainSetting · CertEmailSetting · SetupProgress · ServiceLauncher · ServiceEditor · ServiceViewer · Deployer · ServiceControl · HookReceiver · ServerStats · WebServerSync · DNSChecker · CertificateReader · Clock · WebSettingsEditor · NginxTranslator · AlertSettings |

---

## 8. 흐름 — 이름만 따라 읽어도 무슨 일인지 보인다

### 서비스 올리기

```
web → ServiceLauncher.Launch
        ├ ServiceEditor.Create
        │   ├ KindLookup.Find(종류).Input.Check        이 종류에 필요한 값이 다 있나
        │   ├ NameChooser.Choose                      이름 짓기·겹침 확인
        │   ├ DomainChecker.Check                     주소 겹침 확인
        │   ├ ServiceStore.Create · DomainStore.Add · SecretStore.Set(웹훅 시크릿)
        │   └ EventPublisher.Publish(SiteMapChanged)  → WebServerSync가 듣고 맞춘다
        └ Deployer.Deploy(처음 올림)
```

### push → 끊김 없는 배포

```
web → HookReceiver.Receive
        ├ SignatureChecker 차례로 (GitHub → GitLab → ?secret=)   아니면 401
        ├ BranchFilter.Wanted                                   다른 브랜치면 무시
        ├ Deployer.Deploy(push)                                 배포 중이면 "끝나고 한 번 더"
        └ HookLogStore.Save

Deployer.Deploy
   DeployLock.TryLock → DeployHistoryStore.Start → DeployLog.Open
   → VersionBuilder.Build     (RepoBuilder: WorkFolder · CodeDownloader · BuildContextPacker · ImageBuilder)
   → VersionSwapper.Swap      (ContainerSwapper: ContainerStarter로 새 것(IP를 돌려받음) → PortChecker로 그 IP에 응답 확인)
   → LiveStateStore.Save → WebServerSync.SyncNow  (웹서버가 새 것을 가리킨다 — 설치형은 새 IP로)
   → VersionSwapper.Retire    (그 뒤에야 옛 것을 끈다 · 맞추기가 실패했으면 둘 다 남긴다)
   → DeployHistoryStore.Finish → OldVersionCleaner.Clean
   → EventPublisher.Publish(DeployFinished, SiteMapChanged)
```

### 웹서버 설정 적용 (M5)

```
web → WebSettingsEditor.Apply
        ├ 경로 대상: 서비스 이름이면 그 서비스 번호, 아니면 ExternalAddress
        ├ LoginHasher.Hash (새 비밀번호가 있을 때만)
        ├ SnippetCompiler.Compile (고급 칸이 있을 때) — 버린 지시어는 warnings로
        ├ WebSettingsStore.Set
        └ WebServerSync.SyncNow — 거절되면 옛 설정으로 Set · SyncNow 후 RefusedError
```

### 웹서버 맞추기

```
WebServerSync (SiteMapChanged를 듣거나 30초마다)
   SiteMapBuilder.Build → ConfigWriter.Write → ConfigSender.Send
     └ CertificateReader.Read — 인증서가 있는 HTTPS 주소만 RedirectHTTP (생기거나 끝나면 다음 맞추기에서 바뀐다)
                                               └ AdminSocketGuard: 관리 소켓 설정이 빠졌으면 보내지 않는다
                                                  └ SocketSender: 유닉스 소켓으로 보낸다
```

---

## 9. 디자인 패턴 — 어디에, 왜

| 패턴 | 어디 | 왜 |
|---|---|---|
| **Value Object** | `ServiceName` · `DomainName` · `RepoURL` … | 잘못된 값이 존재할 수 없게. 검사는 만들 때 한 곳 |
| **Strategy** | 종류 담당자 다섯 (`InputChecker` … `StatusReader`) | 종류마다 다른 행동을 `if 종류 ==` 대신 객체로 |
| **Abstract Factory** | `KindLookup` | 한 종류에 맞는 담당자 묶음을 함께 내준다 — 다른 종류 것과 섞이지 않게 |
| **Template Method** | `Deployer` | 배포 순서는 고정, 각 단계의 내용은 종류 담당자가 |
| **Chain of Responsibility** | `SignatureChecker` 차례 | GitHub·GitLab·레지스트리를 더해도 `HookReceiver`는 그대로 |
| **Decorator** | `AdminSocketGuard`가 `ConfigSender`를 감싼다 | "보내기 전 확인"을 보내기와 떼어 놓되 빠뜨릴 수 없게 |
| **Facade** | `ServiceLauncher` | "만들고 배포"의 순서를 화면이 몰라도 되게 |
| **명령과 조회 분리** | `ServiceEditor` ↔ `ServiceViewer` | 바꾸는 쪽과 보여주는 쪽은 바뀌는 이유가 다르다 |
| **Repository** | `store` | 저장 방식을 업무 규칙에서 떼어 낸다. SQL은 한 곳 |
| **Ports & Adapters** | `contract` ↔ 도구·저장 | Docker·Caddy·git·SQLite를 가짜로 바꿔 테스트하고 갈아 끼울 수 있게 |
| **Observer** | `EventPublisher` · `EventSubscriber` | "바뀌면 알리기"를 바뀐 자리에 두고, 듣는 쪽은 자유롭게 늘린다 |
| **Reconciler** | `WebServerSync` | 원하는 모습과 실제를 계속 맞춘다 — 재시작에 강하다 |
| **Composition Root** | `app` | 구체 타입은 여기서만 만나 인터페이스로 꽂힌다 |

**쓰지 않는 것:** DI 컨테이너, 제네릭 Repository, 비동기 메시지 큐, 상속 계층 — 서버 한 대·프로세스 하나 규모에서는 이득보다 비용이 크다.

---

## 10. 규칙 — 테스트가 강제한다

`internal/archtest`가 소스를 `go/parser`·`go/types`로 읽어 확인한다. 어기면 `go test`가 실패한다.

| # | 규칙 |
|---|---|
| R1 | `app`을 뺀 내부 패키지는 `model`·`contract`만 import한다 |
| R2 | 다른 객체를 담는 필드의 타입은 `contract`의 인터페이스다 (구체 타입 금지). 인터페이스만 담은 묶음 `KindTools`는 된다 |
| R3 | `model`은 표준 라이브러리만 import하고, 인터페이스·함수 필드를 갖지 않는다 (데이터만) |
| R4 | 인터페이스는 메서드 다섯 개 이하 |
| R5 | `contract`의 인터페이스·`model`의 타입은 모두 이 문서 §4·§5에 이름과 한 문장이 있다 |
| R6 | §2의 쓰지 않는 말(`Handler`·`Processor`·`Helper`·`Util`·`Info`·`Data`·`Plan`·`Ledger`·`Registry`·`Roles`)이 타입 이름에 없다 (`web`의 HTTP 핸들러 함수는 예외) |
| R7 | SQL은 `store`에만 |
| R8 | 바깥 세계에 닿는 것 — `os/exec`·`syscall`·`archive/tar` import, 파일 읽고 쓰기(`os.WriteFile`…), 연결(`net.Dial`…, `http.Client`…) — 은 도구·저장·조립(`app`)에만 |
| R9 | 종류 이름으로 분기하는 코드는 `kinds`에만 |
| R10 | `web`·`cli`는 `model.ServiceSecrets`·`model.PasswordHash`·`model.WebSettings`·`model.BasicLogin`·`model.AlertChannel`을 쓰지 않는다 (그것을 주고받는 인터페이스도) |
| R11 | 검사용 정규식은 `model`에만 |
| R12 | 설정을 환경 변수에서 읽는 건(`os.Getenv`·`LookupEnv`) `app`·`cmd`뿐이다 — 그래서 설치 방식(`NARU_INSTALL`)에 따른 분기도 `app`에만 있다. 도구가 자식 프로세스(git)에 환경을 넘기는 `os.Environ`은 허용 |

---

## 11. 일하는 방식 — 인터페이스가 먼저

결정이 필요한 변경은 모두 이 순서다.

1. **설계 노트** — §1의 어느 일인가, 새로 생기거나 바뀌는 인터페이스(Go 시그니처 그대로), §7의 바뀐 줄, 지켜야 할 불변식, 패턴과 이유, 고르지 않은 대안
2. **승인** — 납득되지 않으면 여기서 고친다. 코드는 아직 없다
3. **테스트** — 인터페이스에 대한 테스트를 가짜 구현으로 먼저
4. **구현**
5. **규칙 확인** — `archtest` 통과, 이 문서 갱신

인터페이스가 바뀌지 않는 버그 수정은 1~2를 건너뛴다.

---

## 12. 지금 코드를 옮기는 순서 (R0 — 동작 변화 없음)

M4 전에 한다. 지금의 화면·배포 테스트가 회귀 테스트다 — 단계마다 그대로 통과해야 한다.

| 단계 | 내용 | 끝났다는 기준 |
|---|---|---|
| R0-1 | `model`과 `contract`를 만든다. 아직 아무도 쓰지 않는다 | 컴파일 · R3 · R4 · R6 |
| R0-2 | 도구와 저장을 인터페이스 구현으로 옮긴다 — `store` · `docker` · `git` · `files` · `caddy` · `netcheck` · `stats` · `events` · `system` | R7 · R8 |
| R0-3 | `kinds` — 종류 담당자 열여섯 + `KindLookup` | R9 (지금 20곳의 종류 분기 → `kinds` 안으로) |
| R0-4 | `access` · `settings` · `services` · `deploy` · `webhook` · `webserver` · `views` | 화면이 DB를 직접 부르는 곳 12 → 0, 웹서버 알림 호출 10 → 0 |
| R0-5 | `web`·`cli`가 인터페이스만 쓰게 바꾸고, `app`을 §7 표대로 다시 쓴다 | R1 · R2 · R10 |
| R0-6 | `archtest` 전체 + 이 문서와 대조 | R1~R11 |

**R0 끝 (2026-10-10).** 여섯 단계 모두 기준을 넘었다 — `go vet`·`go test ./...`·`archtest` R1~R11 통과.
옛 패키지(`auth`·`dnscheck`·`engine`·`hostinfo`·`service`·`source`·`v1import`)는 지웠고, 그 테스트는 모두 새 자리로 옮겼다:
`auth`→`access`, `dnscheck`→`model`·`netcheck`, `source`→`model`·`files`, `engine`→`caddy`·`webserver`, `hostinfo`→`stats`,
`service`·`v1import`→`store`, 배포→`deploy`(진짜 `kinds`·`store`·`files` + 가짜 Docker), 화면 흐름→`app` 인수 테스트(진짜 조립 + 가짜 바깥).

### 12.1 R0에서 설계와 달라진 점

| 무엇 | 설계 | 코드 | 왜 |
|---|---|---|---|
| 종류 담당자 묶음 이름 | `KindHandlers` | `KindTools` | "Handler"는 §2에서 쓰지 않기로 한 말 (R6) |
| `InputChecker.Check` | `error`만 | `(ServiceInput, error)` | 종류에 맞게 정리(안 쓰는 칸 비우기, 기본 브랜치)까지 한 값을 돌려준다 |
| `ServiceEditor` | 지우기 포함 | 지우기 없음 | 지우기는 컨테이너·이미지·파일까지 함께라 `ServiceControl.Remove` 한 곳 |
| `serviceControl` | — | `DeployLock`을 쓴다 | 배포 중인 서비스는 지우지 않는다 (옛 동작) |
| `WorkFolder` | 빌리기·돌려주기 | + `Inside` | 저장소 안 경로를 심볼릭 링크로 빠져나가지 않게 찾는 일 |
| `DockerfilePortReader.Ports` | 포트만 | `(ports, found)` | "Dockerfile이 없다"와 "EXPOSE가 없다"를 구분해 알려준다 |
| `SwapRequest` | 비밀 포함 | 비밀 없음 | §7대로 `ContainerSwapper`가 `SecretStore`에서 직접 읽는다 — 배포기는 비밀을 만지지 않는다 |
| `DeployLock.TryLock` | — | `why`가 비면 합치지 않는다 | 되돌리기는 "끝나고 한 번 더"로 합치면 안 된다 |
| `siteMapBuilder` · `serviceViewer` | `DomainStore`를 씀 | 쓰지 않음 | `ServiceReader`가 주소까지 함께 준다 |
| `Deployer`·`WebServerSync` 만들기 | — | `NewDeployer`는 `wait`, `NewWebServerSync`는 `run`을 함께 돌려준다 | 수명(멈추기·기다리기)은 조립(`app`)만 다룬다 — 인터페이스는 세 개·두 개 그대로 |
| 저장된 env·볼륨 | — | `StoredEnvVars`·`StoredVolumes` | v1에서 온 값에 틀린 줄이 하나 있어도 나머지를 잃지 않게 |

**동작이 바뀐 곳 (작게, 의도해서)**

- 서비스 폼의 포트 칸에 숫자가 아닌 값을 적으면 예전엔 조용히 0이었다 → 이제 "포트는 1~65535" 오류.
- 이미지 서비스는 브랜치를 저장하지 않는다 (예전엔 `main`이 들어가 있었다).
- 정적 사이트의 "보내는 곳"은 웹서버가 보는 경로(`/srv/sites/…`)로 보인다 (예전엔 `data/sites/…`).
- v1 프록시 호스트 중 설명이 NULL인 것도 옮긴다 (예전엔 SQL 비교 때문에 빠졌다).
- 셸 복구 명령(`naru users` 등)은 작업 폴더 정리·"중단된 배포 닫기"를 하지 않는다 — 돌고 있는 서버의 배포를 건드리지 않게 (서버를 띄울 때만 한다).
- `ADMIN_DOMAIN`·`ACME_EMAIL`이 모양에 맞지 않으면 시작하지 않는다 (예전엔 그대로 웹서버에 넘겼다).

---

## 13. 아직 안 되는 기능

| 기능 | 왜 아직인가 | 들어갈 곳 |
|---|---|---|
| 인증서 발급 실패 이유 (M4-2) | Caddy는 실패를 로그에만 남긴다 — Naru가 그리는 설정에 로그 파일 출력을 넣고 읽는 쪽을 권한다. 실제 문구는 staging에서 모은다 | `CertificateReader` 옆에 실패 읽기 (설계 노트) |
| 비공개 레지스트리 | 레지스트리 자격 증명을 어디에 둘지 정하지 않았다 | `SecretStore` + `ImagePuller` (설계 노트) |
| 빌드가 필요한 정적 사이트 | 빌드를 어떤 컨테이너에서 돌릴지 정하지 않았다 | `StaticBuilder` 앞에 사이트 빌드 단계 (설계 노트) |
