-- cererile agentilor de a instala un server MCP in proiect (install_mcp); le aproba mereu utilizatorul
CREATE TABLE mcp_requests (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id    INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  transport   TEXT NOT NULL CHECK (transport IN ('local', 'remote')),
  command     TEXT NOT NULL DEFAULT '',
  args        TEXT NOT NULL DEFAULT '[]',   -- JSON []string
  url         TEXT NOT NULL DEFAULT '',
  secrets     TEXT NOT NULL DEFAULT '[]',   -- JSON []string, numele cheilor
  item_id     INTEGER REFERENCES library_items(id) ON DELETE SET NULL, -- un element Library existent, de activat in proiect
  reason      TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'installed', 'declined')),
  created_at  TEXT NOT NULL,
  closed_at   TEXT
);
CREATE INDEX mcp_requests_project ON mcp_requests(project_id, status);
