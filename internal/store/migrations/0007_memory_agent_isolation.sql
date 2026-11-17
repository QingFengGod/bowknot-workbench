-- 旧逻辑会把子 Agent 对话产出的记忆写进用户全局域，导致主助手读到子 Agent 的上下文。
-- 先把与目标 Agent 已有记忆重复的全局记录删掉，避免触发去重索引冲突。
DELETE FROM memory
WHERE scope = 'user'
  AND status = 'active'
  AND source_conversation_id IN (
      SELECT id FROM conversation
      WHERE kind = 'agent' AND owner_id NOT IN ('', 'main-agent')
  )
  AND EXISTS (
      SELECT 1
      FROM conversation c
      JOIN memory existing
        ON existing.scope = 'agent'
       AND existing.owner_id = c.owner_id
       AND existing.kind = memory.kind
       AND existing.content_hash = memory.content_hash
       AND existing.status = 'active'
      WHERE c.id = memory.source_conversation_id
  );

-- 再把剩余的全局记录迁回来源 Agent 的专属域。
UPDATE memory
SET scope = 'agent',
    owner_id = (
        SELECT c.owner_id
        FROM conversation c
        WHERE c.id = memory.source_conversation_id
    )
WHERE scope = 'user'
  AND status = 'active'
  AND source_conversation_id IS NOT NULL
  AND source_conversation_id <> ''
  AND EXISTS (
      SELECT 1
      FROM conversation c
      WHERE c.id = memory.source_conversation_id
        AND c.kind = 'agent'
        AND c.owner_id NOT IN ('', 'main-agent')
  );
