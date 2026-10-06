-- agentii adaugati de un lider si cererile de aprobare pentru ei
ALTER TABLE agents ADD COLUMN created_by INTEGER REFERENCES agents(id) ON DELETE SET NULL;

CREATE TABLE agent_requests (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  lead_id     INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  run_id      TEXT NOT NULL DEFAULT '',
  spec        TEXT NOT NULL,   -- JSON: name, role, model, background, instructions, doneDef, tools
  reason      TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'approved', 'dismissed')),
  agent_id    INTEGER REFERENCES agents(id) ON DELETE SET NULL, -- agentul creat la aprobare
  created_at  TEXT NOT NULL,
  closed_at   TEXT
);
CREATE INDEX agent_requests_project ON agent_requests(project_id, status);
CREATE INDEX agent_requests_lead ON agent_requests(lead_id, status);
