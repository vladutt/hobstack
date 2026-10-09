-- Conturi Claude (abonament, CLAUDE_CODE_OAUTH_TOKEN din `claude setup-token`) alese per proiect.
-- projects.account_id NULL = Default: CLI-ul logat pe masina.
CREATE TABLE claude_accounts (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  token       TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  checked_at  TEXT NOT NULL DEFAULT '',  -- ultima verificare reusita; '' = niciodata
  last_error  TEXT NOT NULL DEFAULT ''   -- ultima eroare de autentificare; '' = merge
);
ALTER TABLE projects ADD COLUMN account_id INTEGER REFERENCES claude_accounts(id);
