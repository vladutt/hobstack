-- task oprit de utilizator: ramane in Queued, dar nu-l mai ia nimeni pana la Resume
ALTER TABLE tasks ADD COLUMN stopped INTEGER NOT NULL DEFAULT 0;
