-- 정적 사이트: 저장소의 어느 폴더를 서빙하나, 지금 서빙 중인 배포는 무엇인가
ALTER TABLE services ADD COLUMN folder TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN release TEXT NOT NULL DEFAULT '';
-- 컨테이너 서비스: 지금 요청을 받는 컨테이너 이름. 비어 있으면 container(v1에서 넘겨받은 이름)다.
-- 배포할 때마다 새 컨테이너가 같은 네트워크 별칭(container)으로 붙고, 옛 것은 내려간다 — 그래서 끊기지 않는다.
ALTER TABLE services ADD COLUMN instance TEXT NOT NULL DEFAULT '';
-- 직접 멈춘 서비스 — 감시가 장애로 보지 않는다
ALTER TABLE services ADD COLUMN stopped INTEGER NOT NULL DEFAULT 0;
-- 웹훅을 마지막으로 받은 때와 결과 — "GitHub이 정말 보냈나?"에 답한다
ALTER TABLE services ADD COLUMN hook_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE services ADD COLUMN hook_result TEXT NOT NULL DEFAULT '';

CREATE TABLE deployments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  service_id INTEGER NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'success', 'failed')),
  trigger_type TEXT NOT NULL DEFAULT 'manual',   -- manual | webhook | create | rollback | v1
  commit_hash TEXT NOT NULL DEFAULT '',
  commit_message TEXT NOT NULL DEFAULT '',
  image TEXT NOT NULL DEFAULT '',                 -- 실행한 이미지 ID — 되돌리기에 쓴다
  log TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL DEFAULT (unixepoch()),
  finished_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX deployments_service ON deployments(service_id, id);
