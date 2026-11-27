-- Memoria proiectului: fapte si decizii cu cheie, scrise de agenti (MCP) sau de utilizator, vazute in fiecare tura.
CREATE TABLE project_memory (
  id              INTEGER PRIMARY KEY,
  project_id      INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  key             TEXT NOT NULL,
  summary         TEXT NOT NULL,
  body            TEXT NOT NULL DEFAULT '',
  locked          INTEGER NOT NULL DEFAULT 0,
  author_agent_id INTEGER,          -- NULL = utilizatorul; fara FK: agentul poate fi sters
  author_name     TEXT NOT NULL,
  task_id         INTEGER,          -- NULL = fara task
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  deleted_at      TEXT,             -- NULL = activa
  UNIQUE (project_id, key)
);
-- Istoricul fiecarei intrari: set / forget / restore / lock / unlock.
CREATE TABLE project_memory_versions (
  id              INTEGER PRIMARY KEY,
  memory_id       INTEGER NOT NULL REFERENCES project_memory(id) ON DELETE CASCADE,
  action          TEXT NOT NULL,
  summary         TEXT NOT NULL DEFAULT '',
  body            TEXT NOT NULL DEFAULT '',
  author_agent_id INTEGER,
  author_name     TEXT NOT NULL,
  task_id         INTEGER,
  reason          TEXT NOT NULL DEFAULT '',
  at              TEXT NOT NULL
);
CREATE INDEX project_memory_versions_memory ON project_memory_versions (memory_id);
