CREATE TABLE tool_call (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL,
  message_id      TEXT NOT NULL,
  run_id          TEXT NOT NULL,
  tool_id         TEXT NOT NULL,
  agent_id        TEXT NOT NULL,
  arguments       TEXT NOT NULL DEFAULT '',
  result          TEXT NOT NULL DEFAULT '',
  status          TEXT NOT NULL,
  error           TEXT NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL,
  finished_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_tool_call_conversation ON tool_call(conversation_id, created_at);
CREATE INDEX idx_tool_call_message ON tool_call(message_id);
