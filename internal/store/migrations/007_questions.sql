-- intrebarile agentilor catre utilizator ("Needs you")
CREATE TABLE questions (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id    INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  run_id      TEXT NOT NULL DEFAULT '',
  text        TEXT NOT NULL,
  options     TEXT NOT NULL DEFAULT '[]',
  answer      TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'answered', 'dismissed')),
  created_at  TEXT NOT NULL,
  closed_at   TEXT
);
CREATE INDEX questions_project ON questions(project_id, status);
CREATE INDEX questions_run ON questions(run_id);
