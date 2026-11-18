# 产物（Artifact）存储与预览实现计划

> 状态：A1–A3 已实施（2026-11-18）
> 最后更新：2026-11-18
> 关联文档：`docs/agent-collaboration-plan.md`

## 1. 目标

让主助手与员工产出的可复用内容（文档、代码、图片、网页、链接）成为一等对象：

- 有固定的本地存储位置，不依赖聊天记录存活。
- 能在应用内预览；不能原生预览的格式也能用系统应用打开。
- 与任务、消息、员工关联，任务卡片与产物页都能找到。
- 写入经过工具授权与路径校验，Agent 不能任意读写磁盘。

## 2. 已确认的决策

| # | 决策 |
| --- | --- |
| 1 | 首期支持：Markdown / 文本 / 代码、图片、链接、HTML（只展示源码并用浏览器打开） |
| 2 | 额外保留 `file` 兜底类型：PDF、Office、压缩包等只登记信息，预览交给系统应用 |
| 3 | 文件统一托管在应用数据目录 `~/.bowknot-workbench/artifacts/`，SQLite 只存元数据 |
| 4 | 入口包含：任务卡片、消息内产物卡片、右侧预览 Drawer、独立「产物」一级页面 |
| 5 | 不做内嵌 HTML/JS 渲染，不引入 WebView，保持原生 UI |
| 6 | 先落实现计划，再按阶段编码 |

## 3. 设计原则

1. **内容与元数据分离**：文件落盘，SQLite 存路径、类型、大小、关联关系。
2. **写入唯一入口**：只允许通过内置工具 `builtin_save_artifact` 写入，执行层统一校验。
3. **不可变产物**：首期产物创建后内容不可变；修改等于生成新产物（版本化留到后续）。
4. **关联优先**：产物自动挂到触发它的消息与任务上，用户不需要手动整理。
5. **原生预览**：Markdown 复用现有 `ui/markdown`，图片用 `ui.Image`，其余用系统应用打开。

## 4. 数据模型

### 4.1 Artifact 实体

```go
type ArtifactKind string

const (
    ArtifactMarkdown ArtifactKind = "markdown"
    ArtifactText     ArtifactKind = "text"
    ArtifactCode     ArtifactKind = "code"
    ArtifactHTML     ArtifactKind = "html"
    ArtifactImage    ArtifactKind = "image"
    ArtifactLink     ArtifactKind = "link"
    ArtifactFile     ArtifactKind = "file" // PDF / Office / 压缩包等兜底
)

type Artifact struct {
    ID             string
    ConversationID string
    TaskID         string
    MessageID      string
    Title          string
    Kind           ArtifactKind
    MIME           string
    Filename       string
    RelativePath   string // 相对 artifacts 根目录；链接为空
    URI            string // 链接地址；文件产物为空
    Size           int64
    Summary        string
    CreatedAt      int64
}
```

### 4.2 迁移 `0010_artifact.sql`

```sql
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
```

取舍：

- 不对 `conversation/message` 建外键。消息删除后产物仍应可在产物页找到；孤儿清理放到后续阶段。
- `relative_path` 只存相对路径，移动数据目录后仍可通过 `paths.Artifacts` 还原绝对路径。
- `URI` 只用于链接类型，避免把 URL 塞进文件字段。

## 5. 存储与安全

### 5.1 目录布局

```text
~/.bowknot-workbench/
├── artifacts/
│   └── art-<id>/
│       └── <filename>
├── bowknot.db
└── ...
```

- `paths.Paths` 新增 `Artifacts` 字段，`Prepare` 时创建目录。
- 每个产物独占一个目录，避免同名覆盖，也便于未来放缩略图/附件。

### 5.2 写入校验

| 校验项 | 规则 |
| --- | --- |
| 文件名 | 取 `filepath.Base`，去掉路径分隔符与控制字符；清洗后为空则用 `<kind>-<id 前 8 位>` |
| 扩展名 | 与类型匹配；缺失时按类型补 `.md/.txt/.html` 等 |
| 文本大小 | Markdown / 文本 / 代码 / HTML ≤ 2 MiB |
| 图片大小 | ≤ 20 MiB，扩展名限 jpg/jpeg/png/gif/webp，并用 `http.DetectContentType` 复核 |
| 文件大小 | 兜底类型 ≤ 50 MiB |
| 链接 | 只允许 `http` / `https`，其余协议拒绝 |
| 已有文件来源 | `sourcePath` 必须位于 `paths.Root` 内（含 `tmp/`、`apps/`、`plugins/`），拒绝任意系统路径 |
| 原子写 | 先写同目录临时文件，`fsync` 后 `rename`，失败清理临时文件 |

### 5.3 删除

- 删除产物 = 删除记录 + 删除 `artifacts/art-<id>/` 目录。
- 删除前用 `filepath.Rel` 确认路径仍在 artifacts 根目录内，防止误删。
- 删除按钮放在预览 Drawer 与产物页，带确认对话框。

### 5.4 文件缺失

`ArtifactDetail` 读取时检查文件是否存在，返回 `Available`；UI 对缺失产物显示「文件已丢失」并只保留删除入口。

## 6. 工具与写入链路

### 6.1 `builtin_save_artifact`

所有 Agent 可用（主助手、员工），无需审批，但受第 5 节校验约束。

参数：

```json
{
  "type": "object",
  "properties": {
    "title": {"type": "string", "description": "产物标题，便于用户在列表里识别"},
    "kind": {"type": "string", "description": "markdown/text/code/html/image/link/file，可省略并按 filename 推断"},
    "filename": {"type": "string", "description": "文件名，可省略"},
    "content": {"type": "string", "description": "文本内容；link 类型填 URL"},
    "sourcePath": {"type": "string", "description": "已有文件的绝对路径，仅允许数据目录内；image/file 使用"},
    "language": {"type": "string", "description": "代码语言，用于预览"},
    "summary": {"type": "string", "description": "一句话说明，可省略"}
  },
  "required": ["title"]
}
```

校验矩阵：

- `link`：必须提供 `content` 且为 http/https。
- `markdown/text/code/html`：必须提供 `content`。
- `image/file`：必须提供 `sourcePath`。

### 6.2 自动关联

工具执行时通过 `tool.MessageID(ctx)` 读取当前消息，再查库拿到：

- `conversation_id`
- `task_id`（任务线程消息带任务 ID）
- `message_id`

产物写入时自动落这三列，Agent 不需要传任务 ID。

工具返回：

```text
已保存产物「周报初稿」（artifact id：art-xxxx，Markdown，12 KB）。
```

### 6.3 执行层接入

- `internal/infra/tool/artifact.go`：工具 ID、输入结构、`ArtifactSink` 接口与 `ArtifactToolSpec()`。
- `app.ToolExecutor` 注入 `tool.ArtifactSink`（由 `app.ArtifactService` 实现），新增分派分支。
- `main.go` 注册工具规格并注入实现。

### 6.4 提示词

在工具描述中说明：可复用的交付物应保存为产物，回复正文只写摘要，避免同一大段内容既在消息里又在文件里。

## 7. 服务与契约

### 7.1 新增 `internal/contract/artifact.go`

```go
type ArtifactKind string

type ArtifactBrief struct {
    ID        string
    Title     string
    Kind      ArtifactKind
    MIME      string
    Size      int64
    CreatedAt int64
    Available bool
}

type ArtifactDetail struct {
    ArtifactBrief
    ConversationID string
    TaskID         string
    MessageID      string
    Filename       string
    Path           string // 绝对路径；链接为空
    URI            string
    Summary        string
    SourceKind     string // main / agent / group
    SourceName     string
}

type ArtifactFilter struct {
    Kind       ArtifactKind
    AgentID    string
    TaskID     string
    Search     string
    Limit      int
}

type ArtifactService interface {
    Artifacts(filter ArtifactFilter) []ArtifactDetail
    Artifact(id string) ArtifactDetail
    DeleteArtifact(id string) error
    ArtifactRevision() uint64
}
```

### 7.2 `ChatMessage` 扩展

新增：

```go
Artifacts []ArtifactBrief
```

读取消息时统一附加，规则：

1. 按 `message_id` 查产物（普通对话与任务线程内的消息）。
2. 按 `task_id` 查产物（任务汇报写回来源会话时，消息 ID 不同，但任务 ID 相同）。
3. 按产物 ID 去重，按创建时间升序。

`attachArtifacts` 与现有 `attachTaskBriefs` 放在同一层（`internal/app/chat.go`），主助手与 Agents 页的消息读取都调用。

### 7.3 Store 新增 `Artifacts` 子 store

```go
Create(ctx, artifact) error
Get(ctx, id) (domain.Artifact, error)
List(ctx, filter) ([]domain.Artifact, error)
ListByMessageIDs(ctx, ids) ([]domain.Artifact, error)
ListByTaskIDs(ctx, ids) ([]domain.Artifact, error)
Delete(ctx, id) error
```

### 7.4 Service 行为

- `ArtifactRevision()` 在创建、删除时递增；页面轮询刷新。
- 创建成功后不做额外汇报；任务汇报已经由 `ReportService` 负责。
- `DeleteArtifact` 先删文件再删记录；文件删除失败时保留记录并返回中文错误。

## 8. 预览方案

| 类型 | 预览方式 | 打开方式 |
| --- | --- | --- |
| Markdown | `ui/markdown` 渲染 | 系统默认应用 |
| 文本 | 纯文本渲染，超过 256 KB 只显示前 256 KB 并提示 | 系统默认应用 |
| 代码 | 用 `markdown.View` 包成代码块，按 `language` 高亮 | 系统默认应用 |
| HTML | **不渲染**，只显示源码；提示「HTML 仅提供源码预览」 | 默认浏览器 |
| 图片 | `os.ReadFile` → `ui.DecodeImage` → `ui.Image`，按容器宽度等比缩放 | 系统默认应用 |
| 链接 | 显示 URL 与来源，禁止内嵌加载 | 系统浏览器 |
| 文件兜底 | 图标、文件名、大小、类型 | 系统默认应用 |

统一操作：

- 打开：`mygo.Shell.OpenPath(path)`；链接用 `mygo.Shell.OpenExternal(uri)`。
- 在文件夹中显示：`mygo.Shell.ShowItemInFolder(path)`。
- 复制路径：复用 mygo 的剪贴板能力（与现有复制行为一致）。
- 删除：确认后调用 `DeleteArtifact`。

## 9. UI 方案

### 9.1 组件

| 组件 | 用途 |
| --- | --- |
| `components/artifact_card.go` | 消息/任务卡片里的产物行：图标、标题、类型、大小、缺失状态 |
| `components/artifact_preview.go` | 按类型渲染预览内容，接收 `ArtifactDetail` 与打开回调 |
| `components/artifact_drawer.go` | 右侧预览 Drawer：标题、类型、大小、来源、操作栏、预览区 |

- Drawer 的 `Open *bool` 由页面持有；打开对象 ID 也放在页面结构体。
- 预览组件只读文件，不在组件内缓存跨帧数据。

### 9.2 助手页

- 任务卡片底部增加产物行；`ChatMessage.Artifacts` 非空时渲染 `ArtifactCard` 列表。
- 点击产物打开预览 Drawer。
- 任务过程 Drawer 中的消息同样显示产物。

### 9.3 Agents 页

- 直聊消息与任务线程消息显示产物行。
- 任务线程仍保持只读，但可以预览产物。

### 9.4 独立「产物」页面

- 一级导航新增「产物」，路由 `/artifacts`，图标使用 lucide `package`（新增 `IconArtifact`）。
- 页面结构：顶部筛选（全部 / 文档 / 图片 / 链接 / 其它，来源员工下拉）+ 产物列表 + 右侧预览 Drawer。
- 列表项显示标题、类型、来源、大小、时间；支持删除与在文件夹中显示。
- 空状态提示「员工产出交付物后，会自动出现在这里」。

### 9.5 导航与装配

- `shell.Deps` 增加 `Artifacts contract.ArtifactService`。
- 注册 `/artifacts` 页面与一级导航。
- 任务卡片与消息的打开回调由页面注入，跨页跳转不需要新增全局路由能力。

## 10. 分阶段实施

### A1 数据与能力

1. `paths.Paths` 增加 `Artifacts` 并创建目录。
2. 迁移 `0010_artifact.sql`、`domain/artifact.go`、`store/artifact.go`、`store.go` 注册。
3. `contract/artifact.go` 与 `ChatMessage.Artifacts`。
4. `app/artifact.go`：校验、写入、删除、查询、revision、`ArtifactSink` 实现。
5. `infra/tool/artifact.go` + `ToolExecutor` 分派 + `main.go` 注入。
6. `chat.go` 附加产物，更新 store 版本测试。

验收：模型调用 `builtin_save_artifact` 后文件落盘、库里有记录、消息能带出产物摘要。

### A2 预览与入口

1. `artifact_card.go`、`artifact_preview.go`、`artifact_drawer.go`。
2. 助手页任务卡片与消息接入预览。
3. Agents 页消息接入预览。

验收：Markdown/代码/图片能在应用内预览；HTML 只显示源码并可浏览器打开；链接可跳转。

### A3 产物页

1. 新增 `internal/ui/pages/artifacts/`。
2. 注册导航与路由，接入筛选、删除、在文件夹中显示。
3. 页面轮询 `ArtifactRevision` 刷新。

验收：员工完成后无需进入具体会话，即可在产物页找到并预览。

### A4 后续增强（不在本次范围）

- 产物版本化与历史版本对比。
- 用户工作区目录、导出/另存为。
- 图片缩略图与懒加载。
- 全文检索、按任务/员工统计。
- 孤儿文件与失效记录的定期清理。

## 11. 风险与约束

| 风险 | 应对 |
| --- | --- |
| Agent 任意写盘或路径穿越 | 只允许 `builtin_save_artifact` 写入，文件名清洗，来源路径限定数据目录 |
| HTML/JS 注入 | 不内嵌渲染，只展示源码，用系统浏览器打开 |
| 大文件导致 UI 卡顿 | 文本预览截断 256 KB；图片限制 20 MiB；列表只加载元数据 |
| 内容重复（消息正文 + 文件） | 工具描述要求正文只写摘要 |
| 消息删除后产物残留 | 产物独立存在，后续做孤儿清理；当前不级联删除文件 |
| Windows 文件被占用 | 打开/删除失败时返回中文提示，不静默失败 |
| 数据目录迁移 | 只存相对路径，读取时拼接 `paths.Artifacts` |

## 12. 测试计划

- store：产物创建、查询、按消息/任务批量读取、删除、索引过滤。
- service：文件名清洗、路径穿越拒绝、类型推断、大小限制、链接协议校验、来源路径白名单、删除清理目录、revision 递增。
- 消息关联：按 `message_id` 与 `task_id` 附加产物并去重；任务汇报消息能带出任务产物。
- 工具：输入校验矩阵、自动关联会话/任务/消息、错误信息为中文。
- 回归：现有任务、汇报、直聊消息不受影响。
- UI 交互与预览效果按项目约定人工验收。

## 13. 实施状态

| 阶段 | 状态 |
| --- | --- |
| 设计 | 完成 |
| A1 数据与能力 | 完成 |
| A2 预览与入口 | 完成 |
| A3 产物页 | 完成 |
| A4 增强 | 未开始 |
