-- Grant-urile din "Needs you" tin pe tot task-ul (task_id) sau pe chat (NULL, pana la New chat); nu se mai consuma.
DROP INDEX command_grants_open;
DELETE FROM command_grants WHERE used_at IS NOT NULL;
ALTER TABLE command_grants DROP COLUMN used_at;
ALTER TABLE command_grants ADD COLUMN task_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE;
CREATE INDEX command_grants_agent ON command_grants(agent_id, task_id);

-- felul cererii de acces: '' = tool refuzat, 'ask' = comanda pe o regula Ask,
-- 'command' = comanda in afara tool-urilor unui agent fara Shell (prefix calculat)
ALTER TABLE access_requests ADD COLUMN kind TEXT NOT NULL DEFAULT '';
UPDATE access_requests SET kind = 'ask' WHERE prefix <> '';
