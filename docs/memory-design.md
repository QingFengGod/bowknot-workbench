# 长期记忆系统设计

> 状态：设计已确认，尚未实现
> 关联：[infrastructure-plan.md](./infrastructure-plan.md)
> 结论：不需要修改现有基础实现；本轮基础设施不实现记忆系统，等聊天主链路和模型网关落地后再做。

## 1. 目标

- 由 AI 判断值得长期保留的内容，只写原子事实，不写整条消息。
- 按发生时间归档，支持按天、按月回顾。
- 通过分层摘要控制注入上下文的大小，避免用得越久加载越多。
- 每条记忆可追溯到来源消息，可编辑、删除、置顶。
- 本地优先，不依赖云端向量库。

## 2. 非目标

- 不把完整对话或整段回复直接写入记忆。
- 不在每轮对话注入全部记忆。
- 首版不做 embedding 与向量检索。
- 不自动删除用户记忆。
- 不在本轮基础设施阶段实现。

## 3. 三层结构

```text
message          原始对话，短期上下文
  ↓ AI 抽取
memory           原子记忆，按 occurred_at / day_key 归档
  ↓ 按周期聚合
memory_digest    日摘要、月摘要，只做索引
  ↓ 需要时展开
memory           回查该周期内的原子记忆
```

`conversation.summary` 是会话列表展示用的摘要，和 `memory_digest` 不是一回事，不要合并。

## 4. 作用域与类型

作用域：

- `scope=user`：跨 Agent 的全局偏好，例如「回复用中文」「少用列表」。
- `scope=agent`：某个 Agent 专属记忆，例如「代码搭档要求先读代码再改」。

类型 `kind`：

- `profile`：用户背景、长期身份信息
- `preference`：偏好、习惯、表达方式
- `fact`：稳定事实
- `decision`：已经确认的决策
- `event`：带时间的事件

## 5. 时间语义

一条记忆存三个时间：

- `occurred_at`：事情发生或生效的时间，用于排序和归档。
- `day_key`：按用户时区计算的 `YYYY-MM-DD`，用于日摘要，避免 UTC 跨天。
- `created_at`：记忆写入数据库的时间。

普通偏好默认取消息时间；「下周三开会」这类事件由抽取结果给出 `occurred_at`。

## 6. 数据表

```sql
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
```

说明：

- `content_hash` 是规范化内容的 SHA-256，用于幂等和去重。
- 唯一索引只约束 `active` 记忆；被替代或归档的历史记录不参与去重。
- `source_version` 由来源记忆的 ID 与 `updated_at` 计算，用于判断摘要是否过期。
- `dirty` 标记摘要需要重建。
- 不建 `memory_job` 表；首版抽取失败只记录日志，不重试。

## 7. 写入流程

### 7.1 触发时机

- 助手回复状态变为 `done` 后异步执行，不阻塞回复。
- 停止或失败的消息默认不抽取，避免把半截内容写成事实。

### 7.2 抽取输入与输出

输入只给最近一两轮对话，不给全部历史。输出固定 JSON 数组，每轮最多 3 条：

```json
[
  {
    "scope": "user",
    "kind": "preference",
    "content": "用户偏好中文回复，少用列表",
    "keywords": ["中文", "回复", "格式"],
    "occurred_at": "2026-11-16T10:30:00+08:00",
    "confidence": 0.9,
    "supersedes_id": ""
  }
]
```

约束：

- `content` 不超过 200 字，必须是完整、独立可读的陈述。
- 只记录用户明确表达或确认的内容；助手自己的推测不写入。
- 用户明确纠正旧信息时，`supersedes_id` 指向旧记忆。
- 抽取前过滤 API Key、密码、身份证号等敏感内容，命中则跳过。

### 7.3 去重与冲突

1. 规范化内容：去首尾空白、合并连续空白、统一中英文标点。
2. 计算 `content_hash`。
3. 相同 `scope + owner_id + kind + content_hash` 且状态为 `active` 时，只更新 `updated_at`、`confidence`、`last_used_at`，不新增。
4. 如果抽取结果带 `supersedes_id`，把旧记忆标记为 `superseded`，新记忆记录关联。
5. 没有明确纠正但内容相近时，保留两条，检索时按 `updated_at` 与 `confidence` 排序，避免误删用户历史。

### 7.4 与 message 的关系

- 写入或更新至少一条记忆后，把对应助手消息的 `memory_written` 置为 `true`。
- `memory_written` 的含义是「本轮产生了记忆」，不是「整条消息被保存」。
- 抽取失败保持 `false`，不阻塞消息落库。

## 8. 摘要生成

### 8.1 日摘要

- 只处理有 `active` 记忆的日期。
- 在日期切换后、应用空闲时或首次检索发现缺失时生成。
- 输入是该 `scope + owner_id + day_key` 下的记忆列表，按 `occurred_at` 排序。
- 输出 100–200 字摘要 + 3–8 个关键词。
- 摘要只压缩，不引入新事实。
- 写入 `memory_digest`，同时写入 `memory_digest_item` 关联。
- `source_version` 变化时把 `dirty` 置为 1，下次重新生成。

### 8.2 月摘要

- 由该月的日摘要聚合，不直接读原始记忆。
- 数据量不大时可以先不做，等日摘要积累到一定数量再加。

### 8.3 约束

- 摘要不替代原子记忆，原子记忆不因生成摘要而删除。
- 被 `superseded` 或 `archived` 的记忆不参与摘要。
- 单日记忆超过 50 条时分块摘要，再合并，避免一次输入过长。

## 9. 检索流程

### 9.1 始终注入

每次组装 prompt 时取：

- `pinned = 1` 的 `active` 记忆
- 最近 7 天的 `active` 记忆
- 当前问题关键词命中的记忆

合并去重，按 `pinned > 关键词命中 > importance > updated_at` 排序，最多 12 条或 1500 字。

### 9.2 按需展开

出现以下情况时再查摘要：

- 用户提到「上次」「之前」「那天」「上周」「去年」
- 用户问「你还记得……」
- 始终注入的内容不足以回答当前问题

流程：

1. 用关键词与时间范围检索 `memory_digest`。
2. 取 Top 3–5 个周期。
3. 通过 `memory_digest_item` 回查该周期的原子记忆。
4. 与始终注入的记忆合并、去重、重新排序。

如果命中的周期还没有摘要，先按需生成日摘要再检索。

### 9.3 排序与预算

建议评分：

```text
score = pinned * 100
      + keyword_match * 10
      + importance * 2
      + confidence
      + recency_bonus
```

总记忆块限制 2000 字。超出时按评分从低到高截断，并记录日志。不要注入 `superseded` 或 `archived`。

### 9.4 检索方式

首版记忆量通常只有几百到几千条，使用：

- `scope + owner_id + day_key` 索引
- `content` / `keywords` 的关键词匹配

暂不引入 FTS5。SQLite 默认 `unicode61` 对中文分词不友好；如果后续需要全文检索，优先评估 FTS5 `trigram` 分词，而不是直接依赖默认分词器。

### 9.5 使用记录

注入后异步更新 `last_used_at`，用于后续衰减、排序和清理建议，不阻塞回复。

## 10. Prompt 组装

```text
[Agent 人设]
...

[用户全局记忆]
- 2026-11-10：用户偏好中文回复，少用列表

[Agent 专属记忆]
- 2026-11-12：用户要求代码改动保持最小范围

[相关历史记忆]
- 2026-11-05：用户决定消息主存储使用 SQLite

[最近对话]
...
```

要求：

- 记忆块与人设分开，便于审计和删除。
- 每条记忆带日期，方便模型理解时间关系。
- 不把摘要和原始记忆重复注入。
- 记忆块不参与系统提示词的硬编码，全部由检索结果动态生成。

## 11. 用户控制与隐私

- 全局和单个 Agent 都提供「允许长期记忆」开关。
- 提供记忆列表：查看、编辑、删除、置顶、按时间筛选。
- 「已记住」标记可点开查看本轮写入的记忆。
- 支持清空全部记忆；清空前二次确认。
- 抽取前过滤敏感信息，禁止保存 API Key、密码、证件号、银行卡号。
- 记忆只存本地 SQLite；注入 prompt 时会随请求发送给模型服务，需要在隐私说明中明确。

## 12. 失败与一致性

- 消息落库与记忆写入不在同一个事务：消息先落库，记忆异步写。
- 记忆写入使用独立事务，失败不影响消息状态。
- 摘要生成失败保留 `dirty=1`，下次启动或空闲时重试。
- 应用退出时取消进行中的抽取与摘要任务；未完成的记忆下一轮再处理。
- 删除记忆后，相关摘要标记 `dirty`，下次重建。
- 删除会话不删除长期记忆，只把 `source_conversation_id` 置空。

## 13. 性能与容量

- 记忆数量远小于消息数量，通常几百到几千条，不需要分页。
- 摘要数量约为每年 365 条日摘要，月摘要 12 条，增长可控。
- 注入上下文有固定预算，不随记忆总量线性增长。
- 主要成本是抽取和摘要的模型调用，使用低成本模型、异步执行、按天批量可控制。

## 14. 对现有基础设施的影响

结论：**现有阶段 0/1 实现不需要修改，只需要后续增量添加。**

具体：

- 不修改 `conversation`、`message`、`run`、`agent` 等现有表。
- 新增迁移 `0002_memory.sql`，不改写 `0001_init.sql`。
- `domain` 新增 `Memory`、`MemoryDigest`、`MemoryDigestItem`。
- `store.Store` 新增 `Memories`、`MemoryDigests` 两个子 store；现有子 store 不变。
- `message.memory_written` 继续保留，语义明确为「本轮产生了记忆」。
- `conversation.summary` 继续作为会话列表摘要，不与记忆摘要混用。
- 模型网关在阶段 3 需要补一个非流式的结构化调用接口，用于抽取和摘要；现有流式接口不变。
- 配置后续增加 `memory` 段：开关、抽取模型、摘要模型、时区、注入预算。
- `contract` 后续增加记忆管理接口；`ChatMessage.MemoryWritten` 不变。

## 15. 实施阶段建议

不建议放在本轮基础设施实现，原因：

1. 抽取依赖真实模型网关，当前还没落地。
2. 检索效果需要用真实对话验证，提前建表容易返工。
3. 本轮应先把会话落盘、分页、模型流式链路跑通。

建议顺序：

1. 阶段 2：会话与消息落盘、分页、中断恢复
2. 阶段 3：模型网关、非流式结构化调用
3. 记忆 M1：`memory` 表 + Store 子 store
4. 记忆 M2：AI 自动抽取、去重、冲突处理
5. 记忆 M3：始终注入 + 关键词检索
6. 记忆 M4：日摘要 + 按需展开
7. 记忆 M5：记忆管理 UI、开关、清空
8. 记忆 M6（可选）：月摘要、FTS5 trigram、embedding

## 16. 验收标准

- 用户说「记住……」后，重启应用仍能检索到该记忆。
- 同一事实重复抽取只保留一条 `active` 记忆。
- 用户纠正旧信息后，旧记忆变为 `superseded`，不再注入。
- 「上次那个方案」能通过日摘要定位到对应日期并展开原子记忆。
- 注入的记忆块不超过配置预算。
- 记忆关闭后不抽取、不注入。
- 删除记忆后不再出现在任何 prompt 中。
- API Key、密码等敏感内容不会被写入记忆。
- 日摘要生成失败不会影响聊天与消息落库。
