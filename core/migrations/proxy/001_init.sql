CREATE TABLE IF NOT EXISTS proxy_hosts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain TEXT NOT NULL UNIQUE,
  target_scheme TEXT NOT NULL DEFAULT 'http',
  target_host TEXT NOT NULL,
  target_port INTEGER NOT NULL,
  ssl_enabled INTEGER NOT NULL DEFAULT 0,
  force_ssl INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  description TEXT DEFAULT '',
  created_at INTEGER DEFAULT (unixepoch()),
  updated_at INTEGER DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS ssl_certs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain TEXT NOT NULL UNIQUE,
  cert_path TEXT,
  key_path TEXT,
  provider TEXT DEFAULT 'manual',
  expires_at INTEGER,
  auto_renew INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS access_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain TEXT,
  method TEXT,
  path TEXT,
  status INTEGER,
  duration_ms INTEGER,
  ip TEXT,
  user_agent TEXT,
  created_at INTEGER DEFAULT (unixepoch())
);

CREATE INDEX idx_access_logs_domain ON access_logs(domain);
CREATE INDEX idx_access_logs_created ON access_logs(created_at);
