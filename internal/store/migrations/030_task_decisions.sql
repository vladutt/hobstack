-- Intrebarile cu optiuni puse de agent la report_done (decisions) si raspunsurile utilizatorului
CREATE TABLE task_decisions (
  id          INTEGER PRIMARY KEY,
  task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  position    INTEGER NOT NULL,
  question    TEXT NOT NULL,
  options     TEXT NOT NULL,               -- JSON []string
  multi       INTEGER NOT NULL DEFAULT 0,
  asked_by    TEXT NOT NULL DEFAULT '',
  asked_at    TEXT NOT NULL,
  answer      TEXT NOT NULL DEFAULT '[]',  -- JSON []string, alegerile
  other       TEXT NOT NULL DEFAULT '',
  answered_at TEXT
);
CREATE INDEX task_decisions_task ON task_decisions(task_id, position);
-- pasii utilizatorului (user_steps) stau langa valorile de completat (left_for_user)
ALTER TABLE task_todos ADD COLUMN kind TEXT NOT NULL DEFAULT 'value';
