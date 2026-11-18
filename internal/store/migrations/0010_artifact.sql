CREATE TABLE artifact (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL DEFAULT '',
  task_id         TEXT NOT NULL DEFAULT '',
  message_id      TEXT NOT NULL DEFAULT '',
  title           TEXT NOT NULL,
  kind            TEXT NOT NULL,
  mime            TEXT NOT NULL DEFAULT '',
  filename        TEXT NOT NULL,
  relative_path   TEXT NOT NULL DEFAULT '',
  uri             TEXT NOT NULL DEFAULT '',
  size            INTEGER NOT NULL DEFAULT 0,
  summary         TEXT NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL
);

CREATE INDEX idx_artifact_conversation ON artifact(conversation_id, created_at DESC);
CREATE INDEX idx_artifact_task ON artifact(task_id, created_at DESC);
CREATE INDEX idx_artifact_message ON artifact(message_id);
CREATE INDEX idx_artifact_created ON artifact(created_at DESC);
