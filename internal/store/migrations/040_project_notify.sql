-- Notificarile de sistem per proiect (pornite implicit).
ALTER TABLE projects ADD COLUMN notify INTEGER NOT NULL DEFAULT 1;
