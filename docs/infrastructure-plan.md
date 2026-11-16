# 基础设施层实现计划

> 范围：`internal/store`、`internal/infra` 与 `internal/platform`。本文只规划基础设施与现有服务的接入方式，不包含 UI 改造。

## 1. 目标

把当前纯内存的后端替换为可持久化、可替换的基础设施，同时保持 `internal/contract` 对 UI 的承诺不变。

具体目标：

1. 数据落盘：会话、消息、Agent、群聊、应用、插件、授权关系重启后不丢失。
2. 存储入口唯一：所有结构化数据都经 `store.Store` 读写，SQL 不泄漏到上层。
3. 外部能力可替换：模型调用、工具执行、应用包安装都有独立接口，mock 与真实实现可切换。
4. 生命周期可控：应用退出能取消进行中的流式生成、关闭数据库、刷新日志。
5. 安全边界清晰：不可信包解包、工具授权、危险操作审批都有明确落点。

## 2. 范围与非目标

**范围内**

- 数据目录、配置、密钥读取
- Store 门面、子 store、事务、迁移、种子数据
- 模型网关（流式调用、重试、取消、错误归一化）
- 工具注册表、授权校验、执行器外壳
- 应用包 / 插件包的解析、校验、安装与卸载
- 日志、生命周期、优雅退出

**非目标（本阶段不做）**

- 不重构 UI 页面，不改变页面交互
- 不实现真实的向量记忆检索
- 长期记忆系统单独设计，见 [memory-design.md](./memory-design.md)；本轮只保留 `memory_written` 语义，不实现抽取、摘要与检索
- 不实现插件/应用的进程级沙箱，只定义执行器接口与边界
- 不引入 HTTP/RPC 服务端；后端仍是进程内服务
- 不实现多设备同步、云备份

## 3. 现有约束

- Go 1.27，Windows 桌面端，构建产物不应依赖 CGO。
- 依赖方向：`contract <- app -> domain`，`app -> store`，`store -> domain`；`store` 不引用 `contract`、`ui`、`backend`。
- UI 只通过 `contract` 调用后端，`backend` 现有内存实现保留，作为行为对照与演示数据来源。
- 新增依赖必须先确认，包括 `go.mod` 变更与 `go mod tidy`。
- 并发约束：锁内不做阻塞操作，流式回调前释放锁；后台事件必须回到主线程。

## 4. 目标目录

```text
internal/
├── platform/
│   ├── paths/          数据目录解析与初始化
│   ├── lifecycle/      服务注册、反向关闭、根 context
│   ├── id/             可排序 ID 生成
│   └── logging/        slog 初始化与文件输出
├── domain/             实体与值对象，只依赖标准库
├── store/              唯一的结构化数据入口
│   ├── store.go        Open / Close / 门面字段
│   ├── tx.go           事务与 tx 克隆
│   ├── migrate.go      embed 迁移
│   ├── migrations/
│   │   └── 0001_init.sql
│   ├── seed.go         首次启动种子数据
│   ├── conversation.go 会话
│   ├── message.go      消息与序号
│   ├── run.go          生成任务状态
│   ├── agent.go        Agent 人设
│   ├── group.go        群聊与成员
│   ├── app.go          应用 + 工具开关
│   ├── plugin.go       插件 + 工具 + 授权
│   └── meta.go         内部键值
├── infra/
│   ├── config/         配置加载与校验
│   ├── llm/            模型网关与 OpenAI 兼容实现
│   ├── tool/           工具注册、授权、执行器
│   └── package/        manifest 解析、下载、解包、安装
├── app/                用例编排（后续阶段新增）
└── backend/            保留内存实现，供演示与测试
```

`domain` 只依赖标准库；`store` 是唯一 import `database/sql` 的包；`infra` 不直接触碰 SQL。

## 5. 模块设计

### 5.1 数据目录与配置

数据根目录统一由 `platform/paths` 解析，优先级：

1. 环境变量 `BOWKNOT_HOME`
2. `os.UserHomeDir()/.bowknot-workbench`

目录布局：

```text
<root>/
├── bowknot.db
├── config.json
├── apps/<appID>/<version>/     应用包内容
├── apps/<appID>/data/          应用数据（卸载时可保留）
├── plugins/<pluginID>/<version>/
├── plugins/<pluginID>/data/
├── logs/
└── tmp/                        下载与解包临时目录
```

`config.json` 最小字段：

```json
{
  "logLevel": "info",
  "model": {
    "provider": "openai-compatible",
    "baseUrl": "",
    "apiKey": "",
    "defaultModel": ""
  }
}
```

首版 API Key 明文写入 `config.json` 的 `model.apiKey`，不单独引入密钥模块。落地时必须配套三条约束：

- 加载配置后对外只暴露脱敏后的模型信息，`apiKey` 不出现在 contract 返回值里
- 日志、错误信息、调试输出统一经过脱敏函数，禁止打印完整 Key
- `config.json` 创建时尽量收紧文件权限，并在文档中提示用户不要分享该文件

后续如需升级为系统凭据，只替换配置加载层，不影响调用方。

### 5.2 Store 设计

#### 依赖

使用 `modernc.org/sqlite`（纯 Go，无 CGO）。该依赖已确认允许引入，阶段 0 再修改 `go.mod` 并执行 `go mod tidy`。

#### 设计原则

1. `store.Store` 是具体类型，不是接口；调用方直接持有 `*store.Store`。
2. 门面持有唯一的 `*sql.DB`，内部按聚合拆子 store，子 store 只共享执行器。
3. 事务通过克隆同一个门面传递，事务内外 API 完全一致。
4. store 只做持久化与结构约束，不写业务规则、不返回中文文案、不调用网络。
5. 一个聚合一个文件，避免 `store` 变成几千行的巨型文件。

#### 核心类型

```go
package store

type Store struct {
    db *sql.DB
    q  querier

    Conversations *ConversationStore
    Messages      *MessageStore
    Runs          *RunStore
    Agents        *AgentStore
    Groups        *GroupStore
    Apps          *AppStore
    Plugins       *PluginStore
    Meta          *MetaStore
}

type querier interface {
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Options struct {
    Path        string
    BusyTimeout time.Duration
    Logger      *slog.Logger
    Seed        bool
}

func Open(ctx context.Context, opts Options) (*Store, error)
func (s *Store) Close(ctx context.Context) error
func (s *Store) Tx(ctx context.Context, fn func(tx *Store) error) error
```

`Open` 内部顺序固定：

1. 打开数据库并设置 PRAGMA
2. 执行迁移
3. 把残留的 `streaming` run 收敛为 `stopped`
4. 按 `Seed` 与 `meta.seed_version` 决定是否导入演示数据

`Close` 执行 `PRAGMA wal_checkpoint(TRUNCATE)` 后关闭连接；关闭后所有方法返回 `ErrClosed`。

#### 子 store

每个子 store 只持有 `querier`，不持有 `*sql.DB`：

```go
type MessageStore struct{ q querier }

func (s *MessageStore) List(ctx context.Context, conversationID string) ([]domain.Message, error)
func (s *MessageStore) Append(ctx context.Context, m domain.Message) (domain.Message, error)
func (s *MessageStore) Update(ctx context.Context, m domain.Message) error
func (s *MessageStore) Delete(ctx context.Context, id string) error
```

| 子 store | 主要方法 | 说明 |
| --- | --- | --- |
| `Conversations` | `List(kind, ownerID)`、`Get`、`Create`、`UpdateMeta`、`Delete` | 助手、Agent、群聊共用，取代 `"group:"` 前缀 |
| `Messages` | `List`、`Append`、`Update`、`Delete` | `Append` 在事务内分配 `seq` 并回填 |
| `Runs` | `Start`、`Finish`、`MarkInterrupted` | 启动时收敛中断的生成任务 |
| `Agents` | `List`、`Get`、`Upsert`、`Delete` | 人设与模型 |
| `Groups` | `List`、`Get`、`Upsert`、`Delete` | 成员变更在事务内整体替换 |
| `Apps` | `List`、`Get`、`Insert`、`Delete`、`ReplaceTools`、`SetToolEnabled`、`SetDataSize`、`MarkUsed` | 应用与工具开关 |
| `Plugins` | `List`、`Get`、`Insert`、`Delete`、`ReplaceTools`、`SetGrants` | 插件、工具、授权 |
| `Meta` | `Get`、`Set`、`Delete` | 种子版本、内部标记 |

调用形态保持扁平：

```go
sessions, err := db.Conversations.List(ctx, domain.KindAssistant, "main")
messages, err := db.Messages.List(ctx, sessionID)
apps, err := db.Apps.List(ctx)
```

#### 事务

`Tx` 使用 `BEGIN IMMEDIATE`，克隆一个 `Store`，把所有子 store 的执行器替换为 `*sql.Tx`：

```go
err := db.Tx(ctx, func(tx *store.Store) error {
    if err := tx.Apps.Insert(ctx, app); err != nil {
        return err
    }
    return tx.Apps.ReplaceTools(ctx, app.ID, app.Tools)
})
```

约定：

- 事务内可以调用任意子 store，事务外不需要感知 `*sql.Tx`
- 不允许嵌套 `Tx`，重复调用返回错误
- 事务只包住数据库写入，不能包住模型调用、下载、解包等外部 I/O
- 单条写入方法（如 `Messages.Append`）在未处于事务中时自行开启短事务

#### 连接与 PRAGMA

启动时设置：

```sql
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;
```

- 首版 `SetMaxOpenConns(1)`，读写串行，桌面本地场景足够可靠
- 写操作保持短事务，避免长事务阻塞读取
- 后续读多写少时再加只读连接池，`Store` 对外 API 不变
- 应用退出前执行 `wal_checkpoint(TRUNCATE)`

#### 初始 schema

```sql
CREATE TABLE schema_migrations (
  version    INTEGER PRIMARY KEY,
  applied_at INTEGER NOT NULL
);

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
  conversation_id TEXT NOT NULL,
  message_id      TEXT NOT NULL,
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
```

说明：

- 会话统一使用 `conversation`，助手、Agent、群聊靠 `kind` 区分，不再使用 `"group:"` 前缀。
- 消息序号 `seq` 用于保证顺序与稳定的增量同步，重启后从最大值继续递增。
- `run` 用于启动时把残留 `streaming` 状态收敛为 `stopped`，避免 UI 卡在生成中。
- `app_meta` 存种子版本、内部标记等非业务键值，避免为一次性状态额外建表。

#### 迁移

- SQL 文件放在 `internal/store/migrations/`，命名 `0001_init.sql`。
- 通过 `//go:embed migrations/*.sql` 嵌入二进制，运行时不需要外部文件。
- 启动时读取 `schema_migrations`，在事务内按版本顺序执行。
- 迁移只允许向前，不做自动回滚；失败则启动中止并输出中文错误。
- 每次新增表结构都必须新增迁移文件，禁止直接改历史文件。

#### 种子数据

区分开发版与正式版两种来源：

- **开发版**：空库时导入一套演示数据，内容与当前内存 mock 对齐，包含演示会话、应用和插件，方便调试 UI。
- **正式版**：不导入演示会话，只安装随包内置的应用与插件。内置包放在 `internal/builtin/apps/*.tar` 与 `internal/builtin/plugins/*.tar`，通过 `embed.FS` 打进二进制，首次启动时复用 `package.Installer` 安装。

共同约定：

- 种子逻辑放在 `store/seed.go`，复用子 store 与 `Tx`，不直接写 SQL。
- 导入完成后写入 `meta.seed_version`，区分 `demo.v1` 与 `builtin.v1`，后续启动不重复导入。
- 开发版可用 `BOWKNOT_SEED=0` 关闭演示数据；正式版内置包不提供关闭开关。
- `cmd/desktop` 根据构建标签 `dev` 选择种子模式，不在运行时猜测环境。

#### ID、时间与错误

- ID 由 app 层通过 `platform/id` 生成，store 不生成 ID，保证测试可确定。
- 时间戳由 app 层赋值，store 只存；`Append` 只负责分配 `seq`。
- store 暴露三类错误：`ErrNotFound`、`ErrConflict`、`ErrClosed`，其余用 `%w` 包装。
- **store 不返回中文文案**，中文错误由 app 层映射，避免存储层被 UI 语义污染。
- 业务校验（标题非空、至少保留一个会话）留在 app；store 只依赖唯一约束、外键和 `CHECK`。
- 查询返回的切片由扫描过程新建，调用方可以安全持有。

### 5.3 模型网关

职责：把「一次对话生成」抽象为可取消的流，屏蔽供应商差异。

```go
type ChatRequest struct {
    Model    string
    System   string
    Messages []Message
    Tools    []ToolSpec
}

type Chunk struct {
    Delta     string
    ToolCalls []ToolCall
    Usage     *Usage
    Done      bool
}

type Provider interface {
    Stream(ctx context.Context, req ChatRequest) (<-chan Chunk, error)
    Models(ctx context.Context) ([]ModelInfo, error)
}
```

实现要点：

- 首版实现 OpenAI 兼容的 `/chat/completions` 流式协议，配置里可切换 `baseUrl` 与模型 ID。
- 重试只发生在「尚未产生首个 token」且错误可重试时，采用指数退避，最多 3 次。
- `ctx` 取消必须立刻关闭底层连接，并把已生成内容交给上层落库为 `stopped`。
- 错误归一化：`auth`（密钥无效）、`rate_limit`（限流）、`timeout`（超时）、`network`（网络）、`server`（上游错误）、`canceled`（主动取消）。
- 网关不直接依赖 store；持久化由上层应用服务完成。
- 事件输出带递增 `Seq`，为后续工具调用、审批事件预留字段。

### 5.4 工具注册与执行

当前 `AppTool` 与 `PluginTool` 是两套结构，基础设施层先统一为内部模型：

```go
type ToolSpec struct {
    ID          string // app:<appID>/<name> 或 plugin:<pluginID>/<name>
    Name        string
    Description string
    Dangerous   bool
    Source      string // app / plugin
    SourceID    string
}

type Executor interface {
    Execute(ctx context.Context, call Call) (Result, error)
}
```

执行流程：

1. 从注册表按 `ToolSpec.ID` 解析工具来源。
2. 通过 `store.Apps` 校验工具是否启用，通过 `store.Plugins` 校验 Agent 是否被授权。
3. `Dangerous=true` 时先发出 `approval_required` 事件，等待 UI 确认；拒绝则返回 `canceled`。
4. 带超时调用执行器，限制输出大小，捕获 panic 转为错误。
5. 记录一次审计条目（工具、来源、Agent、耗时、结果状态），首版写日志文件即可。

首版执行器只提供 `in-process` 空实现与测试实现，真实插件执行不在本阶段范围内。执行器接口保留 `Transport` 字段，后续可切换为子进程或独立沙箱。

授权数据：

- 应用工具沿用 `app_tool.enabled`（全局开关）。
- 插件工具沿用 `plugin_grant(plugin_id, agent_id)`。
- 不新增面向 UI 的接口，现有 `SetAuthorizedAgents`、`SetToolEnabled` 的语义不变。

### 5.5 包管理

#### manifest

应用与插件统一使用 `manifest.json`：

```json
{
  "id": "weekly-report",
  "name": "周报助手",
  "version": "1.2.0",
  "description": "根据工作记录生成周报",
  "entry": "index.html",
  "tools": [
    { "name": "generateWeeklyReport", "description": "生成周报草稿" }
  ]
}
```

远程包为 tar 归档，`manifest.json` 位于归档根目录；解包时只接受普通文件与目录。

校验规则：

- `id` 只允许小写字母、数字、`.`、`-`，长度 1-64。
- `version` 必须为语义化版本。
- `tools[].name` 唯一且符合标识符规则。
- 包的 `id` 与已安装项冲突时拒绝。

#### 安装流程

1. `local`：直接读取目录或 tar 包。
2. `remote`：`net/http` 下载 tar 包到 `tmp/`，限制最大体积（默认 200MB），校验 SHA-256（可选字段）。
3. 使用 `archive/tar` 解包，拒绝绝对路径、`..`、符号链接与硬链接、设备文件、超长路径，防止 tar slip。
4. 解包到 `tmp/<uuid>/`，校验 manifest 后原子移动到 `apps/<id>/<version>/` 或 `plugins/<id>/<version>/`。
5. 在同一个 `store.Tx` 内写入应用/插件及工具记录；任一步失败都清理临时目录并回滚记录。
6. 升级成功后只保留当前版本与上一个版本，更早的版本目录在安装完成后清理；数据库只指向当前版本。
7. 旧版本删除失败（如 Windows 文件占用）不阻塞安装，记录日志并在下次启动时重试。

卸载：

- `keepData=false`：删除包目录与 `data/` 目录，删除数据库记录。
- `keepData=true`：只删除包目录，保留 `data/`。
- 删除失败不应导致数据库记录残留，需先记录状态再删除文件。

#### 数据大小

`DataSize` 由 `filepath.WalkDir` 统计 `data/` 目录，安装、写入、清理后调用 `store.Apps.SetDataSize` 更新。store 不遍历目录，首版只在 `ClearData` 与卸载时重算。

### 5.6 生命周期与日志

```go
type Lifecycle struct {
    ctx    context.Context
    cancel context.CancelFunc
    closers []func(context.Context) error
}

func (l *Lifecycle) Add(name string, close func(context.Context) error)
func (l *Lifecycle) Close() error // 反向顺序，单个失败不阻断其余
```

- `cmd/desktop` 创建根 context，所有流式调用继承该 context。
- 退出顺序：取消根 context → 等待运行中的 run 收敛 → 关闭 store → 刷新日志。
- 日志使用标准库 `log/slog`，输出到 `logs/app.log` 与 stderr；按大小滚动首版不做，只保留最近一个文件。
- 日志字段固定包含 `component`、`conversationId`、`runId`，便于排查。

## 6. 与现有 contract 的接入点

| contract 能力 | 基础设施支撑 |
| --- | --- |
| `AssistantService.Sessions/Messages` | `store.Conversations`（`kind=assistant`）+ `store.Messages` |
| `AgentService.Agents/Groups/Sessions/Messages` | `store.Agents`、`store.Groups`、`store.Conversations`、`store.Messages` |
| `AssistantService.Send/Stop` | `llm.Provider` + `store.Runs` + 根 context |
| `AppCatalog.Apps/Install/Uninstall/ClearData/MarkUsed` | `store.Apps` + `package.Installer` |
| `PluginService.Plugins/Install/Uninstall/SetAuthorizedAgents` | `store.Plugins` + `package.Installer` |
| `AppTool.Dangerous` / `PluginTool.Dangerous` | `tool.Registry` + `store` 授权数据 + 审批事件 |

接入原则：

- 先让应用服务改调 `store`，`contract` 签名保持不变。
- 流式接口后续再补 `Run` 与 `Seq`，不在基础设施层第一次落地时同时改 UI。
- 内存 `backend` 实现保留，作为行为对照与契约测试的参照。

## 7. 实施阶段

### 阶段 0：骨架与依赖

交付：

- `platform/paths`、`platform/lifecycle`、`platform/id`、`platform/logging`
- `infra/config` 加载与校验，包含 `model.apiKey` 脱敏
- 引入 `modernc.org/sqlite`，执行 `go mod tidy`（已确认）

退出条件：

- `go build ./...` 通过
- 应用启动时能创建数据目录与日志文件
- 退出时生命周期正常关闭

### 阶段 1：Store 与迁移

交付：

- `store/store.go`、`store/tx.go`、`store/migrate.go`、`store/migrations/0001_init.sql`
- 8 个子 store 与 `store/seed.go`
- 临时 SQLite 下的 store 契约测试

退出条件：

- 迁移可在空库与已有库上重复执行
- 事务回滚、级联删除、序号分配有测试覆盖
- `go test ./... -race`、`go vet ./...` 通过

### 阶段 2：会话与消息落盘

交付：

- 助手与 Agent 服务改调 `store`，内存数据不再作为唯一数据源
- 启动时通过 `store.Runs.MarkInterrupted` 收敛残留状态
- 消息序号与排序稳定

退出条件：

- 重启后会话、消息、置顶、群聊成员完整保留
- 并发发送、停止、删除消息通过 `-race` 测试
- UI 侧无需改动

### 阶段 3：模型网关

交付：

- `infra/llm` 的 OpenAI 兼容实现与假实现
- 超时、重试、取消、错误分类
- 配置与密钥接入

退出条件：

- 假实现下流式、停止、失败重试行为与现有 mock 一致
- 真实实现可用配置切换，密钥不出现在日志与 contract 返回值中
- 取消后底层请求确实结束

### 阶段 4：工具注册与授权

交付：

- 统一 `ToolSpec`、`Registry`、`Executor` 外壳
- 授权校验、危险操作审批事件、审计日志
- App / Plugin 工具映射到注册表

退出条件：

- 未授权工具调用被拒绝且错误可读
- 危险工具在确认前不执行
- 执行超时与 panic 不会拖垮进程

### 阶段 5：包管理

交付：

- manifest 解析与校验
- 本地目录 / tar 与远程下载安装
- 原子安装、回滚、卸载保留数据
- 升级后保留一个上一版本，清理更早版本
- 内置应用/插件的首次安装
- 数据大小统计

退出条件：

- tar slip、超大包、非法 manifest 均有测试覆盖
- 安装失败不留下半成品目录与数据库记录
- `keepData` 语义与现有 UI 行为一致

### 阶段 6：收尾

交付：

- 数据目录备份与损坏恢复说明
- 启动自检（目录可写、数据库可打开、迁移版本）
- 日志轮转与安装包资源清理策略

退出条件：

- 冷启动、异常退出、升级后启动三种场景通过人工验证
- 本文档更新为实际实现状态

## 8. 测试与验收

自动化：

```bash
gofmt -l .
go vet ./...
go build ./...
go test ./... -race
```

测试重点：

- Store 契约测试：使用 `t.TempDir()` 下的真实 SQLite，不维护内存双实现
- 迁移测试：空库升级、重复执行、失败中止
- 事务测试：跨聚合回滚、级联删除、`Append` 序号唯一
- 包安全测试：路径穿越、绝对路径、超大文件、非法 manifest
- 网关测试：首 token 前重试、首 token 后不重试、取消、错误分类
- 并发测试：同会话单 run、停止与删除竞态

人工验收（不由代理自动执行）：

- 重启后数据完整
- 生成中退出应用，再次启动无卡死状态
- 安装/卸载应用与插件，数据目录变化符合预期

## 9. 风险与已确认决策

| 事项 | 建议 | 状态 |
| --- | --- | --- |
| SQLite 驱动 | `modernc.org/sqlite`，纯 Go | 已确认，阶段 0 引入 |
| Store 方案 | 单门面 + 子 store + Tx 克隆 | 已确认 |
| 数据根目录 | `os.UserHomeDir()/.bowknot-workbench`，支持 `BOWKNOT_HOME` 覆盖 | 已确认 |
| 密钥存储 | 首版明文写入 `config.json` 的 `model.apiKey`，配套脱敏与文件权限 | 已确认 |
| 远程包格式 | tar + `manifest.json`，首版不做签名校验 | 已确认 |
| 种子数据 | 开发版导入演示数据；正式版内置应用与插件 | 已确认 |
| 旧版本包清理 | 升级后保留一个上一版本，清理更早版本 | 已确认 |

主要风险：

- `store` 单入口容易膨胀：用一聚合一文件约束，业务规则留在 app 层。
- SQLite 单连接在长事务下会阻塞读取：写操作保持短事务，流式生成期间节流落库。
- 事务误包外部 I/O 会导致锁等待：事务内禁止网络、解包、模型调用。
- 真实模型的流式协议差异较大，网关需要保留按供应商扩展的入口。
- Windows 路径长度与文件占用会影响卸载；卸载前需关闭相关文件句柄。
- 远程包下载引入网络与安全风险，首版必须限制体积并做路径校验。
- API Key 明文存放在 `config.json`，必须在日志、错误和 contract 返回值三处做脱敏。
- 旧版本删除在 Windows 上可能因文件占用失败，只能延迟重试，不能作为安装失败处理。

## 10. 完成定义

基础设施层完成时，应满足：

1. 删除 `internal/backend` 的内存数据后，应用仍能正常启动并保留用户数据。
2. 所有结构化数据访问都经 `store.Store`，`database/sql` 不出现在 `internal/store` 之外。
3. 所有外部能力（模型、工具、包安装）都通过接口注入，可替换为测试实现。
4. 应用退出不遗留 goroutine、不遗留未完成的 run、数据库正常关闭。
5. `go vet`、`go build`、`go test -race` 全部通过。
6. 本文档中的决策项已全部落地，并同步更新到 `AGENTS.md` 的必要约定中。

## 11. 实施状态（2026-11-16 更新）

阶段 0-6 已全部实现，当前状态：

> 基础设施之后仍未实现的部分见 [remaining-work.md](./remaining-work.md)，实施顺序见 [next-plan.md](./next-plan.md)。

| 阶段 | 状态 | 说明 |
| --- | --- | --- |
| 0 骨架与依赖 | 完成 | `platform/{paths,lifecycle,id,logging}`、`infra/config`（含 MaskKey）、`modernc.org/sqlite` |
| 1 Store 与迁移 | 完成 | `store` 单门面 + 8 子 store + `seed.go`，`schema_migrations` 记录版本；`-race` 因本机无 C 编译器未跑，普通 `go test` 通过 |
| 2 会话与消息落盘 | 完成 | 助手/Agent 服务改调 store，启动收敛 `runs` 中断状态，contract 未变 |
| 3 模型网关 | 完成 | `infra/llm`：OpenAI 兼容 SSE + Fake，首 token 前指数退避重试（最多 3 次），六类错误分类，ctx 取消关闭底层连接；`baseUrl` 为空回退 Fake |
| 4 工具注册与授权 | 完成 | `infra/tool`：Registry + 授权校验（app_tool.enabled / plugin_grant）+ Approver 审批 + 超时/panic/输出限制 + 审计日志；首版 in-process 占位执行器 |
| 5 包管理 | 完成 | `infra/package`（包名 pkgs）：manifest 校验、tar slip 防护、下载上限、原子安装/回滚、保留上一版本、卸载 keepData、数据大小；`internal/builtin` embed 首装 |
| 6 收尾 | 完成 | 启动自检（目录可写探测）、数据库版本高于程序时拒绝打开、日志按 10MB 轮转保留 5 份、启动清空 `tmp/` 残留；备份与恢复说明见 `docs/operations.md` |

与计划的偏差与补充说明：

- `infra/package` 目录名为 `package`、包名为 `pkgs`（`package` 是 Go 关键字，导入处写 `pkgs "…/infra/package"`）。
- Store 新增 `Apps.Upsert` / `Plugins.Upsert`，供包管理升级路径复用（Insert 语义保持不变）。
- 应用层请求模型名经过 `app.requestModel` 映射：UI 的演示模型 ID（bk-pro 等）落空、由网关回退 `model.defaultModel`，待真实模型列表接入 UI 后移除。
- `internal/backend` 内存实现按计划 §6 保留为行为对照；`AppCatalog` / `PluginService` 尚未切换到 `store.Apps` + `pkgs.Installer`，是后续接入点（contract 已具备替换条件）。
- 人工验收项（冷启动、异常退出、升级后启动）按约定不由代理执行，见 §8。
