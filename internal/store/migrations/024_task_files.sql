-- Fisiere predate de agenti pe un task (report_done files): copii (snapshot) in projects/<pid>/task-files/<task>/
CREATE TABLE task_files (
  id          INTEGER PRIMARY KEY,
  task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id    INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  agent_name  TEXT NOT NULL DEFAULT '',
  path        TEXT NOT NULL,
  name        TEXT NOT NULL,
  stored      TEXT NOT NULL,
  size        INTEGER NOT NULL,
  mime        TEXT NOT NULL,
  created_at  TEXT NOT NULL
);
CREATE INDEX task_files_task ON task_files(task_id, path, id);
CREATE INDEX task_files_project ON task_files(project_id);
