-- sarcini care creeaza singure task-uri pe un program
CREATE TABLE recurring_tasks (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  assignee_id INTEGER REFERENCES agents(id) ON DELETE SET NULL, -- NULL = prima etapa (doar cu etape)
  kind        TEXT NOT NULL,              -- every_hours, daily, weekly
  every_hours INTEGER NOT NULL DEFAULT 0,
  at          TEXT NOT NULL DEFAULT '',   -- HH:MM, ora locala
  weekdays    INTEGER NOT NULL DEFAULT 0, -- masca, bitul 1<<time.Weekday
  auto_accept INTEGER NOT NULL DEFAULT 0,
  enabled     INTEGER NOT NULL DEFAULT 1,
  next_run_at TEXT NOT NULL,              -- RFC3339, UTC
  last_run_at TEXT,                       -- ultima incercare (creat, sarit sau eroare)
  last_task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  last_note   TEXT NOT NULL DEFAULT '',   -- "Skipped at 09:00: T-12 still open", eroarea, sau ''
  created_at  TEXT NOT NULL
);
CREATE INDEX recurring_tasks_project ON recurring_tasks(project_id, id);

ALTER TABLE tasks ADD COLUMN recurring_id INTEGER REFERENCES recurring_tasks(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN auto_accept INTEGER NOT NULL DEFAULT 0;
