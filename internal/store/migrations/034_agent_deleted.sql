-- Un agent cu task-uri terminate se poate sterge: ramane in baza (task-urile tin assignee_id), marcat deleted,
-- fara echipa, fara legaturi si cu numele eliberat; ListAgents nu-l mai intoarce, iar task-urile lui apar neasignate.
ALTER TABLE agents ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0;
