-- Task-uri sterse de utilizator (arhivare cu Restore): status 'archived', statusul dinainte si grupul sters
ALTER TABLE tasks ADD COLUMN archived_from TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN archived_at TEXT;
ALTER TABLE tasks ADD COLUMN archived_root INTEGER;

-- CHECK-ul de pe tasks.status trebuie sa primeasca 'archived'. Nu refacem tabela (DROP/RENAME ar rula
-- ON DELETE CASCADE / SET NULL pe tabelele care refera tasks, cu foreign_keys activ); modificam textul
-- schemei, cum permite documentatia SQLite pentru relaxarea unui CHECK. RESET reincarca schema.
PRAGMA writable_schema = ON;
UPDATE sqlite_schema SET sql = replace(sql, 'done'',''blocked'')', 'done'',''blocked'',''archived'')')
  WHERE type = 'table' AND name = 'tasks';
PRAGMA writable_schema = RESET;

CREATE INDEX tasks_archived ON tasks(project_id, archived_root);
