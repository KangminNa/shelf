# v1(Shelf) → v2(Naru) 전환

실서버를 v1에서 v2로 넘기는 순서다. **명령을 그대로 따라 한다.** 서버에서 돌린다.

| 경로 | 무엇 |
|---|---|
| `/home/ubuntu/project/shelf` | v1 — 컨테이너 `shelf`가 80/443을 잡고 있다. 데이터는 `data/` |
| `/home/ubuntu/project/naru` | v2 — 이 저장소의 `v2` 브랜치. 운영 구성은 `deploy/docker/` |

지키는 것

- **v1 데이터 폴더에는 쓰지 않는다.** v2는 복사본을 쓴다. 언제든 `docker start shelf`로 돌아갈 수 있어야 한다.
- **앱 컨테이너(`shelf-*`)는 다시 시작하지 않는다.** v2가 이름 그대로 넘겨받는다.
- **80/443은 한 번에 하나만.** 넘길 때는 "v1 멈춤 → v2 켬", 되돌릴 때는 "v2 멈춤 → v1 켬".
- 지우는 것은 이름으로만 한다 (`docker volume prune` 같은 것은 쓰지 않는다).

중단은 4단계(전환)에만 있다 — HTTP는 몇 초(가짜 실서버 리허설에서 1초 미만), HTTPS는 Caddy가 인증서를 새로 받을 때까지
(주소 몇 개면 보통 1분 안).
인증서가 준비되기 전에는 HTTPS로 넘기지 않으니 그동안 HTTP는 된다. HSTS가 켜진 주소는 그 사이 브라우저에서 열리지 않는다.

---

## 1. 조사 — 읽기만

```bash
uname -m; docker version --format '{{.Server.Version}}'; df -h /home; free -h
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
docker network inspect shelf-net --format '{{range .Containers}}{{.Name}} {{end}}'
sudo ss -ltnp | grep -E ':(80|443|8080|18080|18081|18443) '
sudo ls -la /home/ubuntu/project/shelf/data
grep -E '^(ADMIN_DOMAIN|ACME_EMAIL)=' /home/ubuntu/project/shelf/.env
```

- `8080`·`18080`·`18081`·`18443`을 다른 것이 쓰고 있으면 멈추고 포트를 정한다.
- GitHub 저장소 설정 → Webhooks의 주소가 `https://<관리 주소>/hooks/<번호>` 꼴이면 그대로 동작한다. 다른 꼴이면 전환 뒤 바꾼다.
- v1에만 있던 것(DNS-01 와일드카드 인증서, 직접 올린 인증서, `/hooks/self`)은 3단계의 "not imported" 줄에 나온다.

## 2. 준비 — 중단 없음

```bash
cd /home/ubuntu/project
docker tag "$(docker inspect shelf --format '{{.Image}}')" shelf:v1-final   # 되돌릴 이미지를 이름으로 붙잡아 둔다
sudo tar czf "shelf-data-$(date +%F).tgz" -C shelf data                   # v1 데이터 백업
git clone -b v2 https://github.com/KangminNa/shelf.git naru
cd naru/deploy/docker
cp env.example .env && chmod 600 .env
# .env: ACME_EMAIL·ADMIN_DOMAIN은 v1 .env 값 그대로, NARU_NETWORK=shelf-net
docker compose build                                                       # 이미지를 미리 만든다
```

## 3. 그림자 실행 — 중단 없음

v2를 **127.0.0.1의 다른 포트**(18080/18443)와 **내부 인증서**로 띄워, v1 데이터로 모든 주소가 맞는 컨테이너로 가는지 본다.
알림 주소(`notify.db`)는 복사하지 않는다 — 그림자가 알림을 보내지 않게.

```bash
cd /home/ubuntu/project/naru
sudo mkdir -m 700 rehearsal-data
sudo cp -p ../shelf/data/auth.db* ../shelf/data/proxy.db* ../shelf/data/deploy.db* rehearsal-data/
cd deploy/docker && docker compose -f rehearsal.yml up -d
sleep 5; docker logs naru-rehearsal 2>&1 | grep -E 'imported from v1|not imported|config applied'
```

`not imported` 줄을 하나씩 읽는다 — 빠지는 것이 괜찮은지 정한다.
그다음 주소마다 v1(80·443)과 그림자(18080·18443)의 응답을 나란히 본다. 그림자의 내부 인증서는 몇 초 뒤에 생긴다.

```bash
sleep 10
domains=$(docker run --rm -v /home/ubuntu/project/naru/rehearsal-data:/d alpine \
  sh -c "apk add -q sqlite >/dev/null && sqlite3 /d/naru.db 'SELECT domain FROM domains ORDER BY domain'")
for d in $domains; do
  v1=$(curl -s -o /dev/null -w '%{http_code}' --resolve "$d:80:127.0.0.1" "http://$d/")/$(curl -sk -o /dev/null -w '%{http_code}' --resolve "$d:443:127.0.0.1" "https://$d/")
  v2=$(curl -s -o /dev/null -w '%{http_code}' --resolve "$d:18080:127.0.0.1" "http://$d:18080/")/$(curl -sk -o /dev/null -w '%{http_code}' --resolve "$d:18443:127.0.0.1" "https://$d:18443/")
  echo "$d   v1 http/https: $v1   v2 http/https: $v2"
done
```

상태 코드가 같으면 된다. 다를 수 있는 것:
- HTTP에서 HTTPS로 넘기는 코드 — v1은 `301`, v2는 `307`(인증서가 준비된 뒤에만). 둘 다 넘김이면 같은 것이다.
- v1에서 "HTTPS 강제"를 끄고 HTTPS만 켰던 주소 — v1은 HTTP `200`, v2는 `307` (v2는 인증서가 있으면 늘 넘긴다).
- v1에서 HTTPS가 꺼져 있던 주소는 둘 다 HTTPS `000`이 맞다. 관리 화면은 ssh 터널로 열어 **기존 계정으로 로그인**해 본다:
`ssh -L 18081:127.0.0.1:18081 서버` → `http://localhost:18081`. 이 화면에서 배포·멈추기는 누르지 않는다.

끝나면 지운다.

```bash
cd /home/ubuntu/project/naru/deploy/docker
docker compose -f rehearsal.yml down -v
sudo rm -rf /home/ubuntu/project/naru/rehearsal-data
```

## 4. 전환 — 중단 1~2분

v1을 멈춘 **뒤에** 데이터를 복사한다 — 멈춘 DB라야 복사본이 온전하다.

```bash
cd /home/ubuntu/project/naru
docker stop shelf                                         # ── 여기서부터 중단
sudo mkdir -p -m 700 data
sudo cp -p ../shelf/data/auth.db* ../shelf/data/proxy.db* ../shelf/data/deploy.db* ../shelf/data/notify.db* data/
cd deploy/docker && docker compose up -d                  # ── v2가 80/443을 잡는다
sleep 5; docker logs naru 2>&1 | grep -E 'imported from v1|not imported|config applied'
```

확인

```bash
domains=$(docker run --rm -v /home/ubuntu/project/naru/data:/d alpine \
  sh -c "apk add -q sqlite >/dev/null && sqlite3 /d/naru.db 'SELECT domain FROM domains ORDER BY domain'")
for d in $domains; do
  echo "$d  http: $(curl -s -o /dev/null -w '%{http_code}' --resolve "$d:80:127.0.0.1" "http://$d/")  https: $(curl -s -o /dev/null -w '%{http_code}' --resolve "$d:443:127.0.0.1" "https://$d/")"
done
```

- 모든 주소가 HTTPS로 응답할 때까지 1분쯤 기다린다 (`https: 000`은 아직 인증서를 받는 중).
- 관리 주소에서 **기존 계정으로 로그인** — 홈에 "주의가 필요한 것"이 무엇을 말하는지 본다.
- GitHub 웹훅 화면에서 아무 배달이나 **Redeliver** → 서비스 화면에 "ping" 또는 받은 시각이 보인다.

## 5. 되돌리기 리허설 — 전환 직후, v2에서 아무것도 바꾸기 전에

```bash
cd /home/ubuntu/project/naru/deploy/docker
docker compose stop && docker start shelf                 # v1로
curl -s -o /dev/null -w '%{http_code}\n' https://<관리 주소>/login
docker stop shelf && docker compose start                 # 다시 v2로
```

v2는 마지막 설정과 받아 둔 인증서로 바로 이어서 뜬다. 서버가 재부팅돼도 멈춰 둔 `shelf`는 다시 뜨지 않는다 (`docker stop`한 컨테이너는
`unless-stopped`여도 멈춘 채로 있다) — v1 폴더에서 `docker compose up`을 하지 않는 한 80/443을 다투지 않는다.

## 되돌리기 — 실제로 필요할 때

```bash
cd /home/ubuntu/project/naru/deploy/docker && docker compose stop && docker start shelf
```

v2로 넘어온 뒤 생긴 변화(새 배포·설정·새로 만든 `naru-*` 컨테이너)는 v1에 없다. v1은 넘어가기 직전 모습으로 돈다.

## 6. 정리 — 7일 뒤, 문제가 없으면

```bash
docker rm shelf                   # 멈춰 둔 v1 컨테이너
docker rmi shelf:v1-final         # 되돌릴 이미지
```

- GitHub의 Naru 저장소에서 `/hooks/self` 웹훅을 지운다 (v2에는 스스로 업데이트하는 기능이 없다).
- v1 데이터 폴더와 백업(`shelf-data-*.tgz`)은 한동안 더 둔다.

## Naru 업데이트

```bash
cd /home/ubuntu/project/naru && git pull && cd deploy/docker && docker compose up -d --build
```
