-- modul full autonomy per proiect, task-uri escaladate si cererile de acces:
-- un refuz de permisiune al unui agent devine un rand aici (comasat per agent + tool)
ALTER TABLE projects ADD COLUMN autonomy INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN escalated INTEGER NOT NULL DEFAULT 0;

CREATE TABLE access_requests (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id    INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  tool        TEXT NOT NULL,
  detail      TEXT NOT NULL DEFAULT '',
  count       INTEGER NOT NULL DEFAULT 1,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  dismissed   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX access_requests_open ON access_requests(project_id, dismissed);
