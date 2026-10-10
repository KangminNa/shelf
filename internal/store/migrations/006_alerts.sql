-- 알림 주소 (M6). 주소와 시크릿은 비밀이다 — Discord 웹훅 주소는 그것만으로 보낼 수 있다.
CREATE TABLE alert_channels (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  secret TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

-- 보낸 결과 (최근 200개만 남긴다). 주소를 지워도 결과는 읽을 수 있게 이름을 함께 적는다.
CREATE TABLE alert_deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  channel_id INTEGER NOT NULL,
  channel_name TEXT NOT NULL,
  event TEXT NOT NULL,
  title TEXT NOT NULL,
  ok INTEGER NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX alert_deliveries_recent ON alert_deliveries(id DESC);
