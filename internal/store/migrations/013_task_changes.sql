-- modificarile de fisiere si comenzile facute de agenti pe un task (din tool_use-urile run-urilor)
CREATE TABLE task_changes (
  id         INTEGER PRIMARY KEY,
  task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  run_id     TEXT NOT NULL,
  agent_id   INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  agent_name TEXT NOT NULL DEFAULT '',   -- numele la momentul run-ului (ramane si daca agentul e sters)
  created_at TEXT NOT NULL,
  kind       TEXT NOT NULL,              -- create, update, edit, command
  tool       TEXT NOT NULL,
  path       TEXT NOT NULL DEFAULT '',   -- relativ la folderul agentului cand e in el, altfel absolut
  patch      TEXT NOT NULL DEFAULT '',   -- JSON []Hunk; '' pentru command
  command    TEXT NOT NULL DEFAULT '',
  truncated  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX task_changes_task ON task_changes(task_id, id);
