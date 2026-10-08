-- Reguli pentru comenzile agentilor cu Shell: globale (project_id 0) si per proiect
CREATE TABLE command_rules (
  project_id INTEGER NOT NULL,
  prefix     TEXT NOT NULL,
  mode       TEXT NOT NULL CHECK (mode IN ('allow', 'ask', 'block')),
  updated_at TEXT NOT NULL,
  PRIMARY KEY (project_id, prefix)
);
INSERT INTO command_rules (project_id, prefix, mode, updated_at) VALUES
  (0, 'git push', 'ask', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  (0, 'rm -rf', 'ask', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  (0, 'brew install', 'ask', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  (0, 'sudo', 'block', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
-- "Allow once": un singur run al agentului are voie la prefix
CREATE TABLE command_grants (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id   INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  prefix     TEXT NOT NULL,
  created_at TEXT NOT NULL,
  used_at    TEXT
);
CREATE INDEX command_grants_open ON command_grants(agent_id, used_at);
-- cererile de acces pentru o comanda anume (prefixul regulii Ask); '' = cerere de tool
ALTER TABLE access_requests ADD COLUMN prefix TEXT NOT NULL DEFAULT '';
