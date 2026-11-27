-- Skill-uri incarcate mereu: flag-ul global pe element si alegerea proiectului (suprascrie flag-ul).
ALTER TABLE library_items ADD COLUMN always_load INTEGER NOT NULL DEFAULT 0;
CREATE TABLE library_always (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  item_id    INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  always     INTEGER NOT NULL,
  PRIMARY KEY (project_id, item_id)
);
