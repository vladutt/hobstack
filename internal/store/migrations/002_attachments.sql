CREATE TABLE attachments (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  message_id INTEGER REFERENCES messages(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('project','message')),
  name       TEXT NOT NULL,
  stored     TEXT NOT NULL,
  size       INTEGER NOT NULL,
  mime       TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX attachments_project ON attachments(project_id);
CREATE INDEX attachments_message ON attachments(message_id);
