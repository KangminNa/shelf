-- 서비스마다 웹서버 설정 (M5). 칸이 자주 늘어날 설정이라 JSON 한 덩어리로 둔다 — 모양은 model.WebSettings가 지킨다.
-- 비밀번호 보호는 bcrypt 해시만 들어 있다 (원문은 어디에도 없다).
CREATE TABLE web_settings (
  service_id INTEGER PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  data TEXT NOT NULL
);
