-- Elemente din Library pornite in toate proiectele si cheile lor globale.
ALTER TABLE library_items ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0;
CREATE TABLE library_default_secrets (
  item_id INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  name    TEXT NOT NULL,
  value   TEXT NOT NULL,
  PRIMARY KEY (item_id, name)
);
