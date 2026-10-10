-- De unde vine un element din Library (GitHub, Claude Code, Claude.ai, plugin), pentru "Update available"
CREATE TABLE library_sources (
  item_id    INTEGER PRIMARY KEY REFERENCES library_items(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  repo       TEXT NOT NULL DEFAULT '',
  path       TEXT NOT NULL DEFAULT '',
  ref        TEXT NOT NULL DEFAULT '',
  label      TEXT NOT NULL DEFAULT '',
  latest     TEXT NOT NULL DEFAULT '',
  checked_at TEXT
);
