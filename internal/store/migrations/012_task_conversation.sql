-- conversatia pe task: destinatarul si textul complet al comunicarii (task, raport,
-- feedback). '' = eveniment de istoric simplu, fara corp.
ALTER TABLE task_events ADD COLUMN to_actor TEXT NOT NULL DEFAULT ''; -- agent, "You" sau '' = nimeni anume
ALTER TABLE task_events ADD COLUMN body     TEXT NOT NULL DEFAULT ''; -- textul complet ('' = doar istoric)

-- conversatia task-ului cauta mesajele dupa run si run-urile dupa task
CREATE INDEX messages_run ON messages(run_id);
CREATE INDEX runs_task ON runs(task_id);
