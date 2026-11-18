# 主助手与子 Agent 协作链路实现计划

> 状态：P1、P2、P3、P4 已实施（2026-11-18）
> 最后更新：2026-11-18
> 关联代码：`internal/app/agent_hub.go`、`internal/app/agent.go`、`internal/app/assistant.go`、`internal/store`、`internal/infra/tool`

## 1. 目标

把现在的「一次工具调用 + 新建会话 + 文本汇报」升级为一套可追踪的协作模型，支撑以下产品形态：

- 主助手是秘书/调度中心，默认唯一对用户发言的人。
- 子 Agent 是专业员工，有独立的姓名、人设、模型、工具授权和记忆。
- 主助手把专业任务派给员工，员工完成后向主助手汇报，主助手必须主动向用户汇报。
- 用户也可以直接打开某位员工的会话，单独沟通并让它完成事情。
- 后续群聊中，主助手是群主/管理员，成员 Agent 可在群内相互沟通协作。

不改的东西：生成链路（`runGeneration` / `streamRunner`）、工具授权与审批（`tool.Service`）、记忆体系、应用与插件体系、品牌与 UI 视觉规范。

## 2. 已确认的产品决策

| # | 决策 |
| --- | --- |
| 1 | 员工完成后必须向主助手汇报；主助手必须主动向用户汇报，不等待用户追问 |
| 2 | 主助手派发的任务，员工不能直接对用户说话，只能写入任务线程并向主助手汇报 |
| 3 | 用户直接与员工沟通（直聊）不受限制，属于用户可见会话 |
| 4 | 每位员工默认同时只处理 1 个后台任务，其余排队 |
| 5 | Agent 之间的协作第一阶段只允许发生在群聊内，不允许私聊 |
| 6 | 用户需要某位员工完成某件事时，直接单独跟该员工聊天即可，不强制经过主助手 |

补充决策（本次设计确定）：

- 任务与任务线程 1:1；同一任务的追问或重试复用同一线程。
- 主助手汇报到「任务来源会话」：从助手页派的回到助手会话，从群聊派的回到群聊。
- 任务线程是内部工作记录，P1 中用户在 UI 里只读查看，不在其中直接发消息；直聊是独立会话。
- 员工的后台任务串行执行；用户的直聊是交互优先，不受任务队列阻塞。
- 应用重启后，未完成的任务统一收敛为失败，可通过「重试」重新执行，不在启动时自动调用模型、不补发汇报。

## 3. 设计总览

四层概念：

1. **参与者（Participant）**：`user`、`main`、`agent`、`group`。
2. **会话（Conversation）**：用户↔主助手、用户↔员工、任务线程、群聊。
3. **任务（Task）**：一等对象，记录谁派给谁、状态、线程、结果。
4. **消息（Message）**：带发送者、类型、任务归属与可见性。

四条硬规则：

- 主助手是唯一默认对外发言人。
- 员工只对任务负责，结果只能回给派单人。
- 直聊是用户与员工的独立通道，不产生任务，也不向主助手汇报。
- 群聊是协作空间，跨 Agent 通信只能发生在这里。

## 4. 核心模型

### 4.1 任务状态机

```text
queued ──claim──> running ──done────> done
  │                  ├──fail───────> failed
  │                  ├──cancel─────> canceled
  │                  └──timeout────> timeout
  └──cancel─────────> canceled

failed / canceled / timeout ──retry──> queued（attempt + 1，复用同一线程）
```

约束：

- 终态只能写入一次，使用条件更新 `WHERE status IN ('queued','running')` 保证幂等。
- 只有 `running` 状态可以消费模型事件；取消后迟到的 `done` 不能覆盖终态。
- 每次进入终态只允许产生一次用户汇报，用 `reported_at` 去重。

### 4.2 Task 实体

```go
type TaskStatus string

const (
    TaskQueued   TaskStatus = "queued"
    TaskRunning  TaskStatus = "running"
    TaskDone     TaskStatus = "done"
    TaskFailed   TaskStatus = "failed"
    TaskCanceled TaskStatus = "canceled"
    TaskTimeout  TaskStatus = "timeout"
)

type Task struct {
    ID                   string
    Title                string
    Brief                string // 完整任务描述，派发时由主助手提供
    Status               TaskStatus
    AssignerKind         string // main（P1）；user / agent 预留
    AssignerID           string
    AssigneeID           string // 子 Agent ID
    OriginConversationID string // 来源会话，汇报写回这里
    OriginMessageID      string // 触发派发的工具调用所在消息
    ThreadConversationID string // 子 Agent 的任务线程
    ParentTaskID         string // 预留：任务派生
    GroupID              string // 预留：群聊任务
    Attempt              int
    ResultSummary        string // 面向主助手的结构化结论
    ResultMessageID      string // 线程里最终回复的消息 ID
    Error                string
    CreatedAt            int64
    StartedAt            int64
    FinishedAt           int64
    ReportedAt           int64
}
```

### 4.3 消息信封

在现有 `domain.Message` / `contract.ChatMessage` 上增量扩展，不改变 `role` 语义：

```go
type Message struct {
    // 现有字段保持不变
    ID, ConversationID, Seq, Role, Content, Status, Error string
    SenderName, SenderAvatar, Emotion                    string
    MemoryWritten                                        bool
    CreatedAt                                            int64

    // 新增
    SenderKind  string // user | main | agent | group
    SenderID    string // 主助手固定 main；员工为 agentID
    MessageType string // chat | task_dispatch | task_report | system
    TaskID      string // 关联任务，空表示普通消息
    Visibility  string // user | internal
}
```

渲染约定：

- `task_dispatch`：任务线程里的第一条消息，`SenderKind=main`，`Visibility=internal`，不展示为普通用户消息。
- `task_report`：员工在线程里的最终回复，`Visibility=internal`。
- 主助手写回来源会话的汇报：`SenderKind=main`，`MessageType=task_report`，`Visibility=user`，带 `TaskID`，UI 渲染成任务卡片。
- 现有群聊消息继续使用 `SenderName/SenderAvatar`，P1 不改群聊。

### 4.4 通信矩阵

| 发起方 → 接收方 | 通道 | P1 | 可见性 |
| --- | --- | --- | --- |
| 用户 → 主助手 | direct | 允许 | user |
| 用户 → 员工 | direct | 允许 | user |
| 主助手 → 员工 | 任务线程 | 允许 | internal |
| 员工 → 主助手 | 任务线程 | 允许 | internal |
| 员工 → 用户 | 无 | 禁止（主助手派发的任务） | - |
| 主助手 → 用户 | 来源会话 | 允许（汇报） | user |
| 员工 → 员工 | 无 | 禁止（P3 群聊内开放） | - |
| 任意参与者 → 群聊 | group | P3 | group |

约束落在两处：

- **提示词层**：任务线程的系统提示词明确「你只能把结果写进本线程，不能直接联系用户」。
- **写入层**：员工生成链路只写任务线程；写入来源会话的唯一入口是 `TaskService` 的汇报方法，员工无法绕过。

## 5. 运行时设计

### 5.1 TaskService

新增 `internal/app/task.go`，职责：

- `Dispatch(ctx, in)`：校验员工存在 → 在一个短事务内创建 task + 任务线程（`conversation.kind = 'agent'`，`owner_id = assignee`）→ 写入 dispatch 消息 → 入队。返回 task。
- `Cancel(taskID)`：排队中直接置终态；运行中停止线程生成并等待收尾。
- `Retry(taskID)`：仅允许终态任务；`attempt + 1`，复用线程，追加一条重试派发消息，状态回到 `queued`。
- `Claim / Finish / Fail / Terminal`：状态迁移全部走 store 的条件更新。
- `Tasks(filter)`、`Task(id)`、`TaskMessages(id)`：供 UI 与工具查询。
- 完成处理：消费事件流，终态落库，写出结构化结果，再调用汇报。
- `Shutdown(ctx)`：停止调度、等待在途任务收尾。

### 5.2 调度与并发

- 每位员工一个 FIFO 队列，后台任务并发度固定为 1。
- 全局同时执行的任务数上限（默认 4），避免一次派发压满模型网关。
- 队列只约束后台任务；用户直聊不受队列影响，直接进入现有 `streamRunner`。
- 进程内队列不持久化；重启后按 5.6 收敛。

### 5.3 执行

复用 `AgentService` 的生成链路，但需要把「写用户消息 + 生成」拆开：

- 新增 `AgentService.StartTurn(ctx, in TurnInput)`：`TurnInput` 携带 `DisplayText`、`LLMText`、发送者元信息、消息类型、任务 ID。
- `Send` 变成 `StartTurn` 的直聊封装，行为保持不变。
- 任务线程采用两段式提示词模式：
  - `direct` 模式：现有员工提示词，允许直接回应用户。
  - `task` 模式：追加「你正在执行主助手的委派任务，结果只写入本线程，完成后由主助手向用户汇报」。
- 任务线程的系统提示词同时注入：任务标题、完整任务描述、派单人（主助手姓名）、任务 ID。

### 5.4 完成与汇报

1. 员工流结束，拿到最终内容与状态。
2. `ResultSummary`：用员工最终内容做一次轻量压缩（模型调用失败时回退为截断原文），写入 task。
3. 汇报主助手：用主助手的 system prompt + 结构化输入（标题、状态、结果摘要、失败原因、耗时）生成 1–3 句用户可见文案；失败时回退固定模板。
4. 写入来源会话：`AppendReport` 替换为 `ReportService.Report(task, content)`，校验来源会话不是任务线程，写一条 `task_report` 消息并更新会话摘要、递增 revision。
5. 汇报成功后写 `reported_at`，避免重复汇报。

汇报时机：直接追加，不触发主助手新一轮生成。助手页已有的「非流式中才刷新」逻辑可以避免打断流式回复；消息按 `seq` 排在流式占位消息之后，刷新后顺序正确。

汇报必须覆盖所有终态：成功、失败、取消、超时都要汇报；取消与超时汇报由用户主动操作或系统触发，同样带上任务标题。

### 5.5 取消、超时与重试

- 取消：工具 `builtin_cancel_task` 与 UI 任务卡片都可以触发；运行中先 `Stop(threadConversationID)`，等待收尾（最多 2s），再写终态。
- 超时：默认 10 分钟，从 `started_at` 起算；超时先停止线程再写 `timeout`。
- 重试：仅终态可重试；复用线程，追加「继续完成任务」的派发消息，状态回到 `queued`，`attempt + 1`。
- 迟到事件：终态写入后到达的 done 事件直接丢弃。

### 5.6 启动收敛

`store.Open` 之后、服务启动之前执行：

- `queued` / `running` → `failed`，`error = "应用重启导致任务中断，可在任务列表重试"`。
- 不自动重跑，不补发汇报。
- 现有的 `Runs.MarkInterrupted` 继续负责生成任务的收敛。

### 5.7 生命周期

装配顺序：`Store → tool.Service → AgentService/AssistantService → TaskService → shell`。

关闭顺序：`TaskService` 先停止调度并等待在途任务，再关闭 `AgentService` / `AssistantService`。`TaskService` 不参与退出时的汇报补写，避免阻塞退出。

## 6. 数据与迁移

新增 `internal/store/migrations/0009_task.sql`：

```sql
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
```

关键取舍：

- **不新增 `conversation.kind`**。`conversation.kind` 有 `CHECK` 约束，SQLite 只能重建表；而 `message` 通过外键级联引用 `conversation`，在迁移事务内无法关闭外键，重建会导致消息被级联删除。任务线程继续使用 `kind = 'agent'`，通过 `task.thread_conversation_id` 区分「任务线程」与「直聊会话」。
- **不修改 `message.role`**。`role` 只有 `user/assistant`，显示层继续按角色渲染；发送者身份、消息类型、可见性全部走新增列。
- 新增列都有默认值，旧数据自动兼容：历史消息 `msg_type = 'chat'`、`visibility = 'user'`。
- 旧版委派产生的会话没有 task 记录，升级后按普通直聊会话展示。

## 7. 接口与契约变更

### 7.1 新增 `internal/contract/task.go`

```go
type TaskStatus string

type TaskBrief struct {
    ID           string
    Title        string
    Status       TaskStatus
    AssigneeName string
}

type TaskDetail struct {
    Task
    AssigneeName string
    Origin       string
}

type TaskService interface {
    Tasks(agentID string, status TaskStatus, limit int) []TaskDetail
    Task(id string) TaskDetail
    TaskMessages(id string) []ChatMessage
    CancelTask(id string) error
    RetryTask(id string) error
    TaskRevision() uint64
}
```

- `ChatMessage` 增加 `SenderKind`、`SenderID`、`MessageType`、`TaskID`、`Visibility`、`Task *TaskBrief`（读取时按 `task_id` 关联，供 UI 直接渲染卡片）。
- 任务状态版本由 `TaskService.TaskRevision()` 提供，与消息 revision 独立，避免任务状态变化引发整页消息重载（实现采用这一方案）。

### 7.2 Store 新增 `Tasks` 子 store

```go
Create(ctx, task) error
Get(ctx, id) (domain.Task, error)
List(ctx, filter) ([]domain.Task, error)
Claim(ctx, id, startedAt) (bool, error)      // queued -> running
Finish(ctx, id, status, result, err, at) (bool, error)
MarkReported(ctx, id, at) error
ConvergeInterrupted(ctx) (int, error)
```

所有状态迁移返回 `bool` 表示是否抢到迁移权；调用方据此决定是否继续或放弃。

### 7.3 工具变更

| 工具 | 变更 |
| --- | --- |
| `builtin_run_agent` | 保留 ID，内部改为 `TaskService.Dispatch`，返回「已派发，任务 ID 为 xxx」；删除已失效的 3 分钟超时与旧注释 |
| `builtin_list_tasks` | 新增：按状态/员工查询任务，供主助手回答「进度如何」 |
| `builtin_cancel_task` | 新增：取消指定任务 |
| 授权 | 三个工具继续只对 `MainAssistantAgentID` 开放；群聊（agentID 为主助手）可用 |
| 子 Agent | 不声明任何任务工具 |

新增 `internal/infra/tool/task.go` 存放规格；`agenthub.go` 只保留创建/列举/派发三个规格。

## 8. 代码改造清单

### 8.1 保留

- `internal/app/chat.go`：`runGeneration`、`streamRunner`、工具调用循环。
- `internal/infra/tool/service.go`：授权、审批、超时、审计。
- `internal/app/agent.go` 的会话与消息读写、人设渲染、记忆接入。
- `internal/ui/theme`、`components`、页面骨架与分栏。

### 8.2 改造

| 文件 | 改动 |
| --- | --- |
| `internal/domain/chat.go` | Message 增加五个元信息字段 |
| `internal/domain/task.go`（新增） | Task 与状态常量 |
| `internal/store/store.go` | 注册 `Tasks` 子 store |
| `internal/store/message.go` | 列表/写入/扫描增加新列 |
| `internal/app/agent.go` | 抽出 `StartTurn`；提示词按 direct/task 模式分支；任务线程注入任务上下文 |
| `internal/app/agent_hub.go` | 删除 `watchDelegated` 与 `report`，`RunAgent` 改为调用 `TaskService.Dispatch` |
| `internal/app/assistant.go` | `AppendReport` 改为带任务元信息的汇报写入，保留来源会话校验 |
| `internal/app/chat.go` | `toContractMessage(s)` 补齐元信息与任务摘要 |
| `internal/app/tool_executor.go` | 分派 `builtin_list_tasks` / `builtin_cancel_task` |
| `cmd/desktop/main.go` | 创建 `TaskService`、注入 shell、注册关闭钩子 |
| `internal/ui/shell/shell.go` | Deps 增加 `Tasks contract.TaskService` |

### 8.3 删除

- `AgentHub.watchDelegated`、`AgentHub.report`、`polishReport`、`humanizeReport`、`taskTitle`（能力迁移到 `TaskService` 与 `ReportService`）。
- `AssistantService.AppendReport` 的旧实现（保留同名方法但改签名或直接替换为 `Report`）。

### 8.4 新增

| 文件 | 内容 |
| --- | --- |
| `internal/domain/task.go` | 任务实体与状态 |
| `internal/store/task.go` | 任务持久化与状态迁移 |
| `internal/store/migrations/0009_task.sql` | 迁移 |
| `internal/app/task.go` | 调度、队列、执行、取消、重试、收敛 |
| `internal/app/report.go` | 主助手汇报文案生成与写入 |
| `internal/contract/task.go` | UI 契约 |
| `internal/infra/tool/task.go` | 任务工具规格 |
| `internal/ui/components/task_card.go` | 任务卡片（状态、操作、查看过程） |

## 9. UI 方案

### 9.1 助手页

- 带 `task_report` 类型的消息渲染成任务卡片：标题、员工姓名、状态标签、汇报正文；底部提供「查看过程」「取消」（仅运行中）「重试」（仅终态）。
- 「查看过程」打开右侧 Drawer，展示任务线程消息（只读），复用 `MessageView` 的只读形态。
- 任务卡片属于新组件，进 `internal/ui/components/`，只接收值、状态与回调，状态指针由页面持有。
- 页面轮询 `TaskRevision()` 更新卡片状态；任务线程内容不参与主会话消息刷新。

### 9.2 Agents 页

- 员工会话列表增加「任务」分组或状态标记：任务线程显示状态徽标，直聊会话保持原样。
- 点击任务线程进入只读工作记录；不提供输入框。
- 员工详情区显示当前排队数、运行中任务。

### 9.3 群聊（P3）

- 群聊消息流支持多发送者；主助手固定为群主，消息带 `SenderKind=main`，成员消息带 `SenderKind=agent` 与成员姓名、头像。
- 主助手负责编排：首轮回复中可用 `@成员名`（兼容全角 `＠`）点名；用户在群里直接 @ 成员也会触发该成员发言。被点名的成员依次在群内发言，成员之间也可以继续 @ 其他成员。
- 单条用户消息最多触发 3 轮**成员**讨论，每名成员在同一轮用户消息内只会发言一次；讨论结束后群主再汇总一次，汇总不计入成员轮次。
- 主助手与成员的历史消息在模型上下文里按「【姓名】」标注发言人，避免成员观点被误认为群主发言。
- 群聊协作状态通过 `AgentService.GroupRevision()` / `GroupBusy()` 暴露，Agents 页在成员发言期间轮询刷新，流式内容按 600ms 刷新一次。
- 群内产生的任务沿用同一 task 模型，`GroupID` 与 `AssignerKind=main` 落库，任务汇报写回群聊。
- 群聊中的单条「重答」会丢失 @ 编排上下文，P3 暂不提供重答入口。

## 10. 分期与验收

### P1 任务与汇报（核心）

范围：

1. 迁移与 store：task 表、message 元信息列、Tasks 子 store。
2. `TaskService`：派发、串行队列、全局并发上限、完成/失败/取消/超时、重试、启动收敛。
3. `AgentService.StartTurn` 与任务模式提示词。
4. `ReportService`：结构化结果 + 主助手汇报 + 固定模板回退 + `reported_at` 去重。
5. 工具：更新 `builtin_run_agent`，新增 `builtin_list_tasks`、`builtin_cancel_task`。
6. UI：助手页任务卡片与过程 Drawer；Agents 页任务列表与状态。
7. 删除 `AgentHub` 的旧后台逻辑，重写 `agent_hub_test.go`。

验收：

- 主助手派发后立即返回；员工串行执行，同一员工第二个任务自动排队。
- 任务完成后，来源会话出现一条带任务卡片的汇报；失败、取消、超时同样有汇报。
- 员工在任务线程内不会产生用户可见消息。
- 用户在同一员工的直聊会话发消息不受后台任务队列影响。
- 取消运行中任务会在 2 秒内停止并在卡片上显示已取消；重启后未完成任务显示失败且可重试。
- `gofmt`、`go vet`、`go build`、`go test ./...` 通过。

### P2 直聊与任务上下文

- 员工在直聊中可查看自己的历史任务摘要（只读上下文），不暴露用户全局记忆。
- 助手页提供「转交某员工」的显式入口（可选）。
- 任务卡片支持跳转到对应员工的直聊会话。

### P3 群聊协作

- 群聊多参与者消息模型、主助手群主编排、@ 成员、成员互相协作、轮次上限。
- 群内产生的任务沿用同一 task 模型，`GroupID` 与 `AssignerKind=main` 落库。

### P4 可观测性与增强

- 任务审计视图、耗时与成功率统计：
  - 新增一级页面 `internal/ui/pages/tasks/`（路由 `/tasks`），展示总任务数、成功率、平均耗时与进行中数量。
  - 支持按状态、员工与时间范围（全部 / 近 7 天 / 近 30 天）筛选，任务列表可打开详情抽屉查看说明、结果、错误、产物与执行记录。
  - 右侧「员工表现」按员工聚合任务数、成功率与平均耗时。
- 结果产物挂到 task：产物表已有 `task_id` 关联，任务卡片、任务审计详情与产物页均按任务聚合展示。
- 任务完成后的记忆快照与跨任务检索：
  - 任务进入 `done` / `failed` 终态后，给负责员工写入一条 `kind=event` 的记忆快照（含任务标题、结果与产物数量）；员工关闭长期记忆时跳过。
  - `builtin_list_tasks` 支持 `query` 参数，按标题与描述检索历史任务；任务列表结果附带结果摘要，便于主助手跨任务回答。
- 重试策略：自动重试次数与退避固定为「首次 + 2 次重试」，退避 5s、15s；只对限流、超时、网络等临时错误自动重试，中间失败不向用户汇报；手动重试沿用同一线程。

## 11. 风险与约束

| 风险 | 应对 |
| --- | --- |
| `conversation.kind` / `message.role` 的 CHECK 约束需要重建表，且外键级联会误删消息 | 不改枚举，任务关系放 task 表与消息新增列 |
| SQLite 单连接（`SetMaxOpenConns(1)`），长事务会阻塞 UI 读取 | 任务创建事务只做插入；模型调用、事件消费全部在事务外 |
| `streamRunner` 按会话互斥，无法表达员工级并发 1 | 员工级并发由 `TaskService` 队列控制；直聊独立 |
| 汇报消息插入时主助手正在流式生成 | 汇报直接追加；助手页保持「流式结束再刷新」，消息按 seq 排序 |
| 取消与完成同时写终态 | 所有迁移使用条件更新，只有抢到迁移权的一方才继续 |
| 退出时在途任务丢失汇报 | 关闭顺序先停调度；启动收敛为失败，不补发汇报 |
| 现有测试依赖旧汇报逻辑 | P1 重写 `agent_hub_test.go`，新增任务状态迁移与队列串行测试 |

## 12. 测试计划

- store：任务创建/查询/条件迁移幂等、`ConvergeInterrupted`、消息新列读写与旧数据默认值。
- TaskService：同员工串行、全局并发上限、取消、超时、重试、汇报去重、终态后迟到事件丢弃。
- 汇报：模型润色成功与失败回退两条路径；群聊来源与助手来源两种写入目标。
- 工具：`builtin_run_agent` 返回任务 ID；子 Agent 调用被授权层拒绝；`cancel_task` 越权校验。
- 回归：直聊发送、流式渲染、历史分页、记忆注入不受影响。
- UI 交互按项目约定由人工运行验收，不做自动化。

## 13. 实施状态

| 阶段 | 状态 |
| --- | --- |
| 设计 | 完成 |
| P1 | 完成 |
| P2 | 完成 |
| P3 | 完成 |
| P4 | 完成 |
