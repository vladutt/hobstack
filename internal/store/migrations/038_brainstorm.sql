-- brainstorming: subiecte (ca niste canale) per proiect, runde (mesajul utilizatorului), carduri (cate unul per agent per runda)
CREATE TABLE brainstorm_topics (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  archived    INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE brainstorm_participants (
  topic_id INTEGER NOT NULL REFERENCES brainstorm_topics(id) ON DELETE CASCADE,
  agent_id INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  PRIMARY KEY (topic_id, agent_id)
);

CREATE TABLE brainstorm_rounds (
  id         INTEGER PRIMARY KEY,
  topic_id   INTEGER NOT NULL REFERENCES brainstorm_topics(id) ON DELETE CASCADE,
  text       TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE brainstorm_cards (
  id         INTEGER PRIMARY KEY,
  topic_id   INTEGER NOT NULL REFERENCES brainstorm_topics(id) ON DELETE CASCADE,
  round_id   INTEGER NOT NULL REFERENCES brainstorm_rounds(id) ON DELETE CASCADE,
  agent_id   INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  status     TEXT NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting', 'running', 'done', 'stopped', 'failed', 'skipped')),
  kind       TEXT NOT NULL DEFAULT '',     -- idea | wild | question, completat la final
  title      TEXT NOT NULL DEFAULT '',
  text       TEXT NOT NULL DEFAULT '',
  basis      TEXT NOT NULL DEFAULT '',
  options    TEXT NOT NULL DEFAULT '[]',   -- JSON []string, optiunile unei intrebari
  answer     TEXT NOT NULL DEFAULT '',
  picked     INTEGER NOT NULL DEFAULT 0,
  priority   INTEGER NOT NULL DEFAULT 0,
  follows_id INTEGER,                      -- cardul la care raspunde acesta
  run_id     TEXT NOT NULL DEFAULT '',
  error      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX brainstorm_cards_agent ON brainstorm_cards(agent_id, status);
CREATE INDEX brainstorm_cards_topic ON brainstorm_cards(topic_id);

CREATE TABLE brainstorm_files (
  id         INTEGER PRIMARY KEY,
  topic_id   INTEGER NOT NULL REFERENCES brainstorm_topics(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  stored     TEXT NOT NULL,
  mime       TEXT NOT NULL DEFAULT '',
  size       INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

-- sesiunea Claude a fiecarui agent pe un subiect
CREATE TABLE brainstorm_sessions (
  topic_id    INTEGER NOT NULL REFERENCES brainstorm_topics(id) ON DELETE CASCADE,
  agent_id    INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  session_id  TEXT NOT NULL DEFAULT '',
  prompt_hash TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (topic_id, agent_id)
);
