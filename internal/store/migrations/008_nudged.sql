-- momentul ultimei impingeri a detectorului de task-uri blocate;
-- NULL = neimpins de la ultima schimbare (SaveTask il curata)
ALTER TABLE tasks ADD COLUMN nudged_at TEXT;
