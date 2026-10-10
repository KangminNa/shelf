CREATE TABLE users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  -- v1과 같은 형식: "salt:hash" (scrypt N=16384 r=8 p=1, 64바이트, salt는 hex 문자열 그대로)
  password_hash TEXT NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

-- 토큰 원문은 쿠키에만 있다. DB에는 SHA-256만 남겨 DB가 새도 세션을 훔칠 수 없게 한다.
CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
