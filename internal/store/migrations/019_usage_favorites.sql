-- tokenii unui run (intrare + cache + iesire), din "usage" al rezultatului claude.
-- 0 = inca necitit (run-urile vechi se completeaza din log la pornire), -1 = necunoscut.
ALTER TABLE runs ADD COLUMN tokens INTEGER NOT NULL DEFAULT 0;
-- steluta din lista de proiecte: favoritele stau primele
ALTER TABLE projects ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0;
