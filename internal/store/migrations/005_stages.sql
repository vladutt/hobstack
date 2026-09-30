-- coloane proprii pe board: etape de lucru cu agentii lor
CREATE TABLE stages (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  position   INTEGER NOT NULL,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'work' CHECK (kind IN ('work', 'check'))
);
CREATE INDEX stages_project ON stages(project_id, position);

CREATE TABLE stage_agents (
  stage_id INTEGER NOT NULL REFERENCES stages(id) ON DELETE CASCADE,
  agent_id INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  PRIMARY KEY (stage_id, agent_id)
);

-- etapa curenta a task-ului (NULL = proiect fara etape, sau a trecut de ultima)
ALTER TABLE tasks ADD COLUMN stage_id INTEGER REFERENCES stages(id);
-- task creat de utilizator din board: la final decide tot utilizatorul
ALTER TABLE tasks ADD COLUMN by_user INTEGER NOT NULL DEFAULT 0;
