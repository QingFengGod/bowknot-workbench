CREATE TABLE memory (
  id                     TEXT PRIMARY KEY,
  scope                  TEXT NOT NULL CHECK (scope IN ('user', 'agent')),
  owner_id               TEXT NOT NULL,
  kind                   TEXT NOT NULL CHECK (kind IN ('profile', 'preference', 'fact', 'decision', 'event')),
  content                TEXT NOT NULL,
  content_hash           TEXT NOT NULL,
  keywords               TEXT NOT NULL DEFAULT '',
  occurred_at            INTEGER NOT NULL,
  day_key                TEXT NOT NULL,
  source_conversation_id TEXT,
  source_message_id      TEXT,
  source_role            TEXT NOT NULL DEFAULT 'user',
  confidence             REAL NOT NULL DEFAULT 0.5,
  importance             INTEGER NOT NULL DEFAULT 0,
  pinned                 INTEGER NOT NULL DEFAULT 0,
  status                 TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'archived')),
  supersedes_id          TEXT REFERENCES memory(id) ON DELETE SET NULL,
  created_at             INTEGER NOT NULL,
  updated_at             INTEGER NOT NULL,
  last_used_at           INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX idx_memory_dedupe
  ON memory(scope, owner_id, kind, content_hash)
  WHERE status = 'active';
CREATE INDEX idx_memory_owner_day
  ON memory(scope, owner_id, day_key);
CREATE INDEX idx_memory_owner_status
  ON memory(scope, owner_id, status, updated_at DESC);

CREATE TABLE memory_digest (
  id             TEXT PRIMARY KEY,
  scope          TEXT NOT NULL,
  owner_id       TEXT NOT NULL,
  level          TEXT NOT NULL CHECK (level IN ('day', 'week', 'month')),
  period_start   TEXT NOT NULL,
  period_end     TEXT NOT NULL,
  summary        TEXT NOT NULL,
  keywords       TEXT NOT NULL DEFAULT '',
  memory_count   INTEGER NOT NULL,
  source_version TEXT NOT NULL,
  dirty          INTEGER NOT NULL DEFAULT 0,
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL,
  UNIQUE (scope, owner_id, level, period_start)
);
CREATE INDEX idx_digest_owner_period
  ON memory_digest(scope, owner_id, level, period_start DESC);

CREATE TABLE memory_digest_item (
  digest_id TEXT NOT NULL REFERENCES memory_digest(id) ON DELETE CASCADE,
  memory_id TEXT NOT NULL REFERENCES memory(id) ON DELETE CASCADE,
  PRIMARY KEY (digest_id, memory_id)
);
