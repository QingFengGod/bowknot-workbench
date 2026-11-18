CREATE TABLE task (
  id                     TEXT PRIMARY KEY,
  title                  TEXT NOT NULL,
  brief                  TEXT NOT NULL DEFAULT '',
  status                 TEXT NOT NULL,
  assigner_kind          TEXT NOT NULL,
  assigner_id            TEXT NOT NULL DEFAULT '',
  assignee_id            TEXT NOT NULL,
  origin_conversation_id TEXT NOT NULL DEFAULT '',
  origin_message_id      TEXT NOT NULL DEFAULT '',
  thread_conversation_id TEXT NOT NULL DEFAULT '',
  parent_task_id         TEXT NOT NULL DEFAULT '',
  group_id               TEXT NOT NULL DEFAULT '',
  attempt                INTEGER NOT NULL DEFAULT 0,
  result_summary         TEXT NOT NULL DEFAULT '',
  result_message_id      TEXT NOT NULL DEFAULT '',
  error                  TEXT NOT NULL DEFAULT '',
  created_at             INTEGER NOT NULL,
  started_at             INTEGER NOT NULL DEFAULT 0,
  finished_at            INTEGER NOT NULL DEFAULT 0,
  reported_at            INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX idx_task_thread ON task(thread_conversation_id);
CREATE INDEX idx_task_assignee ON task(assignee_id, status, created_at DESC);
CREATE INDEX idx_task_origin ON task(origin_conversation_id, created_at DESC);

ALTER TABLE message ADD COLUMN sender_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE message ADD COLUMN sender_id TEXT NOT NULL DEFAULT '';
ALTER TABLE message ADD COLUMN msg_type TEXT NOT NULL DEFAULT 'chat';
ALTER TABLE message ADD COLUMN task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE message ADD COLUMN visibility TEXT NOT NULL DEFAULT 'user';
CREATE INDEX idx_message_task ON message(task_id);
