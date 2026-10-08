-- 서비스 하나 = 사용자가 올린 것 하나. 종류가 넷이다.
--   repo     Git 저장소를 빌드해 컨테이너로
--   image    이미지를 받아 컨테이너로
--   static   파일을 웹서버가 직접 서빙 (컨테이너 없음)
--   external 이미 떠 있는 것에 주소만 (NAS, 호스트의 프로그램 등)
CREATE TABLE services (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL CHECK (kind IN ('repo', 'image', 'static', 'external')),
  source TEXT NOT NULL DEFAULT '',      -- 저장소 주소 | 이미지 이름
  branch TEXT NOT NULL DEFAULT 'main',
  build_path TEXT NOT NULL DEFAULT '',
  container TEXT NOT NULL DEFAULT '',   -- repo·image의 컨테이너 이름. v1에서 온 것은 shelf-*
  port INTEGER NOT NULL DEFAULT 0,      -- 앱이 컨테이너 안에서 듣는 포트
  upstream TEXT NOT NULL DEFAULT '',    -- external의 연결 대상 host:port (https://로 시작하면 TLS)
  auto_deploy INTEGER NOT NULL DEFAULT 1,
  -- 아래는 비밀. 화면과 API 응답에 그대로 내보내지 않는다.
  env TEXT NOT NULL DEFAULT '',
  volumes TEXT NOT NULL DEFAULT '',
  git_token TEXT NOT NULL DEFAULT '',
  webhook_secret TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE domains (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  service_id INTEGER NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  domain TEXT NOT NULL UNIQUE,
  https INTEGER NOT NULL DEFAULT 1,     -- 인증서를 받아 443에서도 서빙
  hsts INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX domains_service ON domains(service_id);
