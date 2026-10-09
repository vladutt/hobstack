-- Limitele contului Claude (fereastra de 5 ore si cea saptamanala), din ultimul rate_limit_event al unui run.
-- Un rand pe cont; account_id 0 = Default (CLI-ul logat). Fara cheie straina: Default nu e in claude_accounts.
CREATE TABLE account_usage (
  account_id        INTEGER PRIMARY KEY,
  status            TEXT NOT NULL DEFAULT '',   -- allowed, allowed_warning, rejected
  five_hour_used    REAL NOT NULL DEFAULT 0,    -- 0..1
  five_hour_resets  TEXT,                       -- RFC3339 UTC; NULL = necunoscut
  seven_day_used    REAL NOT NULL DEFAULT 0,
  seven_day_resets  TEXT,
  updated_at        TEXT NOT NULL
);
