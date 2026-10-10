# 설치형으로 설치하기 (systemd)

Naru와 웹서버(Caddy)를 컨테이너가 아니라 서버에 직접 설치합니다. Docker 방식(`docker compose up`)과 기능은 같고,
다음이 다릅니다.

- 웹서버가 호스트에 있어서 **서버의 프로그램에 `localhost`로 바로** 닿습니다.
- 앱 컨테이너는 어느 Docker 네트워크에 있어도 **컨테이너 IP로** 닿습니다 — 같은 네트워크로 묶을 필요가 없습니다.
- 리눅스 서버가 필요합니다 (macOS에서는 컨테이너 IP에 닿지 않습니다).

> Naru는 Docker를 다루므로 **관리자 계정은 설계상 root와 같습니다.** 본인 서버에서만 쓰세요.

## 1. 준비

- Docker Engine — 저장소·이미지 서비스를 띄웁니다 (정적 사이트·외부 연결만 쓰면 없어도 화면은 뜹니다).
- Caddy — [공식 패키지](https://caddyserver.com/docs/install#debian-ubuntu-raspbian)로 설치한 뒤, 패키지가 만든 서비스는 끕니다.
  이 서비스 대신 Naru의 `naru-caddy.service`가 Caddy를 띄웁니다.

```bash
sudo systemctl disable --now caddy
```

## 2. 사용자와 폴더

웹서버와 Naru는 같은 `naru` 사용자로 돕니다 (웹서버가 받은 인증서를 Naru가 읽고, Naru가 올린 정적 사이트를 웹서버가 읽습니다).

```bash
sudo useradd --system --home /var/lib/naru --shell /usr/sbin/nologin --groups docker naru
sudo install -d -o naru -g naru -m 0700 /var/lib/naru
sudo install -d -m 0755 /etc/naru
```

## 3. 바이너리

저장소에서 빌드합니다 (Docker만 있으면 됩니다). 서버가 arm64면 `GOARCH=arm64`.

```bash
GOOS=linux GOARCH=amd64 scripts/go.sh build -trimpath -ldflags "-s -w" -o dist/naru ./cmd/naru
```

```bash
sudo install -m 0755 dist/naru /usr/local/bin/naru
```

## 4. 서비스 등록

```bash
sudo install -m 0644 deploy/host/caddy-bootstrap.json /etc/naru/caddy-bootstrap.json
sudo install -m 0644 deploy/host/naru-caddy.service deploy/host/naru.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now naru-caddy naru
```

## 5. 첫 설정

설정 주소는 로그에만 찍힙니다 — 로그를 볼 수 있는 사람만 관리자를 만들 수 있습니다.

```bash
sudo journalctl -u naru | grep setup
```

`http://<서버 IP>/setup?token=…`로 들어가 계정 → 관리 주소 → HTTPS 연락처를 정합니다.

**방화벽:** 바깥에는 80·443만 엽니다. Naru 자신은 `127.0.0.1:8080`에만 듣습니다.

## 업그레이드 · 되돌리기

새 바이너리로 바꾸고 Naru만 다시 시작합니다. 웹서버는 그대로 돌아서 사이트가 끊기지 않습니다.

```bash
sudo cp /usr/local/bin/naru /usr/local/bin/naru.prev
sudo install -m 0755 dist/naru /usr/local/bin/naru
sudo systemctl restart naru
```

되돌릴 때는 `naru.prev`를 다시 `naru`로 바꾸고 같은 명령으로 다시 시작합니다. DB 마이그레이션은 앞으로만 가므로, 되돌리기 전에 `/var/lib/naru`를 백업해 두세요.

## 계정 되찾기

```bash
sudo -u naru env NARU_INSTALL=host NARU_DATA_DIR=/var/lib/naru naru users
sudo -u naru env NARU_INSTALL=host NARU_DATA_DIR=/var/lib/naru naru passwd admin '새 비밀번호'
```

## 지우기

```bash
sudo systemctl disable --now naru naru-caddy
sudo rm /etc/systemd/system/naru.service /etc/systemd/system/naru-caddy.service /usr/local/bin/naru
sudo rm -r /etc/naru
# 데이터(계정·서비스·인증서·비밀)는 /var/lib/naru — 백업했는지 확인한 뒤 지웁니다
```

Naru가 띄운 앱 컨테이너(`naru-*`)는 그대로 남습니다. 필요 없으면 `docker rm -f`로 지웁니다.
