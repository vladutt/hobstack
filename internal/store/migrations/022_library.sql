-- Library: tool-uri (scripturi), skill-uri si servere MCP refolosite intre proiecte
CREATE TABLE library_items (
  id                INTEGER PRIMARY KEY,
  kind              TEXT NOT NULL CHECK (kind IN ('tool', 'skill', 'mcp')),
  name              TEXT NOT NULL,
  description       TEXT NOT NULL,
  status            TEXT NOT NULL CHECK (status IN ('proposed', 'published', 'declined', 'unpublished')),
  source_project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  source_task_id    INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  made_by_agent_id  INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  made_by           TEXT NOT NULL DEFAULT '',
  needs             TEXT NOT NULL DEFAULT '[]',
  secrets           TEXT NOT NULL DEFAULT '[]',
  transport         TEXT NOT NULL DEFAULT '',
  url               TEXT NOT NULL DEFAULT '',
  command           TEXT NOT NULL DEFAULT '',
  args              TEXT NOT NULL DEFAULT '[]',
  entry             TEXT NOT NULL DEFAULT '',
  tool_names        TEXT NOT NULL DEFAULT '[]',
  created_at        TEXT NOT NULL,
  published_at      TEXT,
  UNIQUE (kind, name)
);
CREATE TABLE library_versions (
  id         INTEGER PRIMARY KEY,
  item_id    INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  version    TEXT NOT NULL,
  dir        TEXT NOT NULL,
  files      INTEGER NOT NULL DEFAULT 0,
  lines      INTEGER NOT NULL DEFAULT 0,
  note       TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (item_id, version)
);
CREATE TABLE library_installs (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  item_id    INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  version_id INTEGER NOT NULL REFERENCES library_versions(id) ON DELETE CASCADE,
  enabled    INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (project_id, item_id)
);
CREATE TABLE library_secrets (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  item_id    INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  value      TEXT NOT NULL,
  PRIMARY KEY (project_id, item_id, name)
);
CREATE TABLE library_uses (
  id       INTEGER PRIMARY KEY,
  item_id  INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  task_id  INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  agent_id INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  at       TEXT NOT NULL
);
CREATE INDEX library_uses_item ON library_uses(item_id, at);
