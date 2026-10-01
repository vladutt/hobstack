-- conversatia pe task: destinatarul si textul complet al comunicarii (task, raport,
-- feedback). '' = eveniment de istoric simplu, fara corp.
ALTER TABLE task_events ADD COLUMN to_actor TEXT NOT NULL DEFAULT ''; -- agent, "You" sau '' = nimeni anume
ALTER TABLE task_events ADD COLUMN body     TEXT NOT NULL DEFAULT ''; -- textul complet ('' = doar istoric)
