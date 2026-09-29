-- ierarhie si review: legaturile agentilor
ALTER TABLE agents ADD COLUMN parent_id INTEGER REFERENCES agents(id) ON DELETE SET NULL;
ALTER TABLE agents ADD COLUMN reviewer_id INTEGER REFERENCES agents(id) ON DELETE SET NULL;
ALTER TABLE agents ADD COLUMN responsibilities TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN pos_x REAL;
ALTER TABLE agents ADD COLUMN pos_y REAL;

-- mai multi lideri pe proiect; coloana position ramane, dar nu mai e folosita
DROP INDEX IF EXISTS agents_one_lead;

-- proiectele vechi: workerii raporteaza fostului lead
UPDATE agents
   SET parent_id = (SELECT l.id FROM agents l WHERE l.project_id = agents.project_id AND l.position = 'lead')
 WHERE position = 'worker';

ALTER TABLE tasks ADD COLUMN creator_id INTEGER REFERENCES agents(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN parent_task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN review_stage TEXT NOT NULL DEFAULT '' CHECK (review_stage IN ('', 'reviewer', 'lead'));

-- task-urile vechi au fost create de lead, iar cele din review asteapta decizia lui
UPDATE tasks
   SET creator_id = (SELECT l.id FROM agents l WHERE l.project_id = tasks.project_id AND l.position = 'lead');
UPDATE tasks SET review_stage = 'lead' WHERE status = 'review';

CREATE INDEX agents_parent ON agents(parent_id);
CREATE INDEX tasks_parent ON tasks(parent_task_id);
