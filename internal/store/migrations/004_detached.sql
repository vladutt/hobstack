-- agent nelegat ("pe banca"): fara sef si nelegat nici de utilizator.
-- Are sens doar cand parent_id e NULL; nu lucreaza pana il legi.
ALTER TABLE agents ADD COLUMN detached INTEGER NOT NULL DEFAULT 0;
