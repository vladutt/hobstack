-- calea originala (absoluta) a fisierului atasat de un agent: "Show in Finder" o deschide cat timp exista
ALTER TABLE task_files ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE attachments ADD COLUMN source TEXT NOT NULL DEFAULT '';
