-- foldere de lucru per proiect (ex. Client + Landing); primul (position 0) e cel implicit
CREATE TABLE project_folders (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  position   INTEGER NOT NULL,
  name       TEXT NOT NULL,
  path       TEXT NOT NULL,
  context    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX project_folders_project ON project_folders(project_id, position);

-- fiecare proiect existent primeste folderul lui de lucru. Numele e ultimul
-- segment din cale: rtrim(w, <w fara '/'>) taie numele de la coada si lasa
-- "…/", iar replace scoate acel prefix. "/" sau gol devine "Main".
INSERT INTO project_folders (project_id, position, name, path)
  SELECT id, 0,
    COALESCE(NULLIF(replace(rtrim(workdir, '/'), rtrim(rtrim(workdir, '/'), replace(rtrim(workdir, '/'), '/', '')), ''), ''), 'Main'),
    workdir
  FROM projects;

-- folderul de baza al agentului; NULL = primul folder al proiectului
ALTER TABLE agents ADD COLUMN folder_id INTEGER REFERENCES project_folders(id) ON DELETE SET NULL;
