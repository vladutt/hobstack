-- istoricul unui task: cate un rand pe fiecare pas (creat, asignat, raportat, ...);
-- se sterge odata cu task-ul
CREATE TABLE task_events (
  id         INTEGER PRIMARY KEY,
  task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  kind       TEXT NOT NULL,            -- created, assigned, started, reported, approved, rejected, ...
  actor      TEXT NOT NULL DEFAULT '', -- numele agentului, "You" sau "Foreman"
  text       TEXT NOT NULL             -- propozitie scurta, maxim ~200 de caractere
);
CREATE INDEX task_events_task ON task_events(task_id, id);
