CREATE TABLE app_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE conversation (
  id         TEXT PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind IN ('assistant', 'agent', 'group')),
  owner_id   TEXT NOT NULL DEFAULT '',
  title      TEXT NOT NULL,
  summary    TEXT NOT NULL DEFAULT '',
  pinned     INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX idx_conversation_owner ON conversation(kind, owner_id, updated_at DESC);

CREATE TABLE message (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
  seq             INTEGER NOT NULL,
  role            TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
  content         TEXT NOT NULL DEFAULT '',
  status          TEXT NOT NULL,
  error           TEXT NOT NULL DEFAULT '',
  sender_name     TEXT NOT NULL DEFAULT '',
  sender_avatar   TEXT NOT NULL DEFAULT '',
  emotion         TEXT NOT NULL DEFAULT '',
  memory_written  INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL,
  UNIQUE (conversation_id, seq)
);
CREATE INDEX idx_message_conversation ON message(conversation_id, seq);

CREATE TABLE run (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
  message_id      TEXT NOT NULL REFERENCES message(id) ON DELETE CASCADE,
  status          TEXT NOT NULL,
  started_at      INTEGER NOT NULL,
  finished_at     INTEGER
);
CREATE INDEX idx_run_conversation ON run(conversation_id, status);

CREATE TABLE agent (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  avatar_text TEXT NOT NULL DEFAULT '',
  persona     TEXT NOT NULL DEFAULT '',
  model_id    TEXT NOT NULL DEFAULT '',
  pinned      INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

CREATE TABLE agent_group (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  pinned     INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE TABLE agent_group_member (
  group_id TEXT NOT NULL REFERENCES agent_group(id) ON DELETE CASCADE,
  agent_id TEXT NOT NULL,
  PRIMARY KEY (group_id, agent_id)
);

CREATE TABLE app (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  display_name TEXT NOT NULL,
  version      TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  icon_text    TEXT NOT NULL DEFAULT '',
  source_type  TEXT NOT NULL,
  source_value TEXT NOT NULL DEFAULT '',
  install_dir  TEXT NOT NULL DEFAULT '',
  installed_at INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL DEFAULT 0,
  data_size    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE app_tool (
  app_id      TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  dangerous   INTEGER NOT NULL DEFAULT 0,
  enabled     INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (app_id, name)
);

CREATE TABLE plugin (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  version      TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  icon_text    TEXT NOT NULL DEFAULT '',
  source_type  TEXT NOT NULL,
  source_value TEXT NOT NULL DEFAULT '',
  install_dir  TEXT NOT NULL DEFAULT '',
  installed_at INTEGER NOT NULL
);

CREATE TABLE plugin_tool (
  plugin_id   TEXT NOT NULL REFERENCES plugin(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  dangerous   INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (plugin_id, name)
);

CREATE TABLE plugin_grant (
  plugin_id TEXT NOT NULL REFERENCES plugin(id) ON DELETE CASCADE,
  agent_id  TEXT NOT NULL,
  PRIMARY KEY (plugin_id, agent_id)
);
