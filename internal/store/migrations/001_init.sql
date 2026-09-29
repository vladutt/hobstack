CREATE TABLE projects (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL,
  goal         TEXT NOT NULL DEFAULT '',
  context      TEXT NOT NULL DEFAULT '',
  workdir      TEXT NOT NULL,
  paused       INTEGER NOT NULL DEFAULT 0,
  max_parallel INTEGER NOT NULL DEFAULT 2,
  budget_usd   REAL    NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL
);

CREATE TABLE agents (
  id           INTEGER PRIMARY KEY,
  project_id   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  role         TEXT NOT NULL DEFAULT '',
  position     TEXT NOT NULL CHECK (position IN ('lead','worker')),
  model        TEXT NOT NULL DEFAULT 'sonnet',
  background   TEXT NOT NULL DEFAULT '',
  instructions TEXT NOT NULL DEFAULT '',
  done_def     TEXT NOT NULL DEFAULT '',
  tools        TEXT NOT NULL DEFAULT '[]',
  session_id   TEXT NOT NULL DEFAULT '',
  prompt_hash  TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL
);
-- exact un lead per proiect
CREATE UNIQUE INDEX agents_one_lead ON agents(project_id) WHERE position = 'lead';
-- nume unic in proiect, fara diferenta intre litere mari si mici
CREATE UNIQUE INDEX agents_name ON agents(project_id, name COLLATE NOCASE);

CREATE TABLE tasks (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  number      INTEGER NOT NULL,
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  assignee_id INTEGER NOT NULL REFERENCES agents(id),
  status      TEXT NOT NULL CHECK (status IN ('queued','in_progress','review','done','blocked')),
  result      TEXT NOT NULL DEFAULT '',
  feedback    TEXT NOT NULL DEFAULT '',
  send_backs  INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  UNIQUE (project_id, number)
);
CREATE INDEX tasks_assignee ON tasks(assignee_id);

CREATE TABLE runs (
  id         TEXT PRIMARY KEY,
  agent_id   INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id    INTEGER REFERENCES tasks(id),
  status     TEXT NOT NULL CHECK (status IN ('running','done','failed','stopped','interrupted')),
  started_at TEXT NOT NULL,
  ended_at   TEXT,
  cost_usd   REAL,
  error      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE messages (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id   INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('msg','event')),
  author     TEXT NOT NULL,
  text       TEXT NOT NULL,
  run_id     TEXT,
  delivered  INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);
CREATE INDEX messages_agent ON messages(agent_id, id);
