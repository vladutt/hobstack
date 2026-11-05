-- task-ul a trecut prin etape (coloane proprii): send back il intoarce intr-o etapa;
-- 0 = drumul standard, chiar intr-un proiect cu etape
ALTER TABLE tasks ADD COLUMN via_stages INTEGER NOT NULL DEFAULT 0;
UPDATE tasks SET via_stages = 1
  WHERE stage_id IS NOT NULL OR project_id IN (SELECT project_id FROM stages);
