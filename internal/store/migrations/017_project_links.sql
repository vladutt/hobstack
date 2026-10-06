-- legaturi intre proiecte si cererile dintre ele (intrebari si cereri de lucru)
CREATE TABLE project_links (
  id         INTEGER PRIMARY KEY,
  project_a  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE, -- mereu id-ul mai mic
  project_b  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  UNIQUE (project_a, project_b),
  CHECK (project_a < project_b)
);

CREATE TABLE project_requests (
  id           INTEGER PRIMARY KEY,
  kind         TEXT NOT NULL CHECK (kind IN ('question', 'work')),
  from_project INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  from_agent   INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  from_task    INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  to_project   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title        TEXT NOT NULL DEFAULT '',
  text         TEXT NOT NULL,
  status       TEXT NOT NULL CHECK (status IN ('open', 'answered', 'closed', 'pending', 'approved', 'done', 'dismissed')),
  answer       TEXT NOT NULL DEFAULT '',
  to_task      INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  created_at   TEXT NOT NULL,
  closed_at    TEXT
);
CREATE INDEX project_requests_to ON project_requests(to_project, status);
CREATE INDEX project_requests_from ON project_requests(from_agent, kind, status);

-- task-ul creat dintr-o cerere de lucru; nota cu o intrebare primita (anti-bucla)
ALTER TABLE tasks ADD COLUMN request_id INTEGER REFERENCES project_requests(id) ON DELETE SET NULL;
ALTER TABLE messages ADD COLUMN request_id INTEGER REFERENCES project_requests(id) ON DELETE SET NULL;
