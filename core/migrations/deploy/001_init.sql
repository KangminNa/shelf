CREATE TABLE IF NOT EXISTS projects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  repo_url TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT 'main',
  install_cmd TEXT DEFAULT 'npm install',
  build_cmd TEXT DEFAULT '',
  start_cmd TEXT DEFAULT 'npm start',
  port INTEGER,
  env TEXT DEFAULT '',
  domain TEXT DEFAULT '',
  webhook_secret TEXT NOT NULL,
  auto_deploy INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER DEFAULT (unixepoch()),
  updated_at INTEGER DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS deployments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL,
  commit_hash TEXT DEFAULT '',
  commit_message TEXT DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  trigger_type TEXT NOT NULL DEFAULT 'manual',
  log TEXT DEFAULT '',
  duration_ms INTEGER DEFAULT 0,
  created_at INTEGER DEFAULT (unixepoch())
);

CREATE INDEX idx_deployments_project ON deployments(project_id);
CREATE INDEX idx_deployments_created ON deployments(created_at);
