-- "Left for you": ce i-a ramas utilizatorului de completat dupa un task (report_done left_for_user)
CREATE TABLE task_todos (
  id         INTEGER PRIMARY KEY,
  task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  position   INTEGER NOT NULL,
  text       TEXT NOT NULL,
  done_at    TEXT,
  done_by    TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (task_id, position)
);
CREATE INDEX task_todos_open ON task_todos(task_id, done_at);
