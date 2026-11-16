# 后续实施计划

> 日期：2026-11-16
> 前置：基础设施阶段 0-6 已完成，详见 [infrastructure-plan.md](./infrastructure-plan.md)。
> 记忆系统的详细设计见 [memory-design.md](./memory-design.md)，本文只列实施顺序，不重复表结构。

## 1. 当前基线

已经完成并接入启动链路的部分：

- `internal/store`：SQLite、迁移、8 个子 store、开发版种子数据
- `internal/app`：助手与 Agent 服务，会话、消息、Run 已落盘
- `internal/infra/llm`：OpenAI 兼容 SSE + Fake，错误分类、首 token 前重试
- `internal/infra/tool`：工具注册、授权校验、审批接口、审计
- `internal/infra/package`：tar + manifest、下载校验、原子安装/卸载
- `internal/builtin`：embed 首装入口（目录当前为空）
- `platform`：目录、生命周期、ID、日志轮转

仍然用演示数据或占位的部分：

- `AppCatalog` / `PluginService` 仍是 `internal/backend` 内存实现
- 工具服务已经实现，但没有任何调用方，模型返回的工具调用也被忽略
- 记忆系统只有设计文档，没有表和代码
- 消息仍全量加载，没有分页
- 模型列表仍是硬编码的演示 ID
- 应用启动、AIGC、工作台仍是占位

## 2. 优先级总览

| 优先级 | 阶段 | 目标 |
| --- | --- | --- |
| P0 | A. Apps / Plugins 真实化 | 安装、卸载、授权、工具开关全部落库落盘 |
| P0 | B. 工具调用闭环 | 模型能发起工具调用，经审批后执行并回填结果 |
| P1 | C. 长期记忆 | 按 memory-design.md 实现 M1-M4 |
| P1 | D1. 消息分页 | 会话很长时 UI 不一次性加载全部消息 |
| P2 | D2-D4. 产品补齐 | 模型列表、内置包、应用启动、AIGC/工作台 |
| P2 | D5. 质量与运维 | race 测试、备份清理、索引整理 |

## 3. 阶段 A：Apps / Plugins 真实化（P0）

目标：`apps` 和 `plugins` 页不再依赖 `internal/backend`，所有数据来自 `store` 与 `pkgs.Installer`。

### A1 新增包预览能力

文件：`internal/infra/package/preview.go`

```go
type Preview struct {
    Kind        string
    Manifest    Manifest
    SourceType  string
    SourceValue string
    Size        int64
}

func (i *Installer) Preview(ctx context.Context, kind, sourceType, sourceValue string) (Preview, error)
```

要求：

- 复用 Install 的下载、解包、manifest 校验逻辑，但不写数据库、不移动包目录
- 本地目录、本地 tar、远程 tar 三种来源都要支持
- 远程预览设置超时和大小上限，临时目录用完即删
- 预览结果中的 `Manifest` 用于安装前展示

### A2 实现 AppCatalog

文件：`internal/app/catalog.go`

| contract 方法 | 实现 |
| --- | --- |
| `Apps()` | `store.Apps.List`，映射为 `contract.AppItem`，工具按 `app_tool` 返回 |
| `Preview(source)` | 调 `installer.Preview(KindApp, ...)`，映射为 `contract.AppPreview` |
| `Install(preview)` | 校验 preview 的 ID/版本仍一致，调 `installer.Install(KindApp, ...)`，刷新工具注册表 |
| `Uninstall(id, keepData)` | 调 `installer.Uninstall(KindApp, id, keepData)`，刷新工具注册表 |
| `SetToolEnabled` | 写 `store.Apps.SetToolEnabled`，刷新工具注册表 |
| `ClearData` | 调 `installer.ClearData(KindApp, id)` |
| `MarkUsed` | 写 `store.Apps.MarkUsed` |

注意：

- contract 的 `Apps()` / `Preview()` 没有 error 返回值，store 出错时记录日志并返回空结果或当前缓存，不 panic
- 安装前重新校验 manifest，避免预览后远程包被替换
- `DataSize` 以 store 中记录为准，安装/卸载后刷新

### A3 实现 PluginService

文件：`internal/app/plugins.go`

| contract 方法 | 实现 |
| --- | --- |
| `Plugins()` | `store.Plugins.List`，映射为 `contract.Plugin` |
| `Agents()` | 内置主 Agent + `store.Agents.List`，映射为 `contract.PluginAgent` |
| `Install(source)` | 调 `installer.Install(KindPlugin, ...)`，刷新工具注册表 |
| `Uninstall(id)` | 调 `installer.Uninstall(KindPlugin, id, false)` |
| `SetAuthorizedAgents` | 写 `store.Plugins.SetGrants`，刷新工具注册表 |

### A4 启动组装

修改 `cmd/desktop/main.go`：

- 创建 `tool.Service`，注入 `store` 与执行器
- 创建 `app.NewAppCatalog(db, installer, toolSvc, logger)`
- 创建 `app.NewPluginService(db, installer, toolSvc, logger)`
- 启动时调用 `toolSvc.SyncApps` / `SyncPlugins`
- 替换 `backend.NewAppCatalog` / `backend.NewPluginService`
- `backend` 仅保留给单测或对照，不再出现在启动链路

### A5 测试

- 用 `t.TempDir()` 构造本地 tar 包，覆盖预览、安装、升级、卸载
- 覆盖卸载保留数据、清空数据
- 覆盖插件授权后 `plugin_grant` 落库
- 覆盖安装/卸载后工具注册表同步
- 覆盖预览后包内容变化导致安装被拒绝

验收：

- `apps` / `plugins` 页数据来自 SQLite
- 重启后应用、插件、授权关系完整保留
- 卸载应用时保留/清空数据符合选择
- `go test ./...` 通过

## 4. 阶段 B：工具调用闭环（P0）

目标：模型可以请求工具，工具经授权与审批后执行，结果回填给模型，最终回复正常落库。

### B1 模型请求支持工具声明

涉及：`internal/infra/llm`

- `ChatRequest` 增加 `Tools []ToolSchema`
- `Message` 增加 `ToolCalls []ToolCall` 与 `ToolCallID`，用于多轮工具调用
- OpenAI 实现按协议序列化 `tools`、`tool_calls`、`role=tool`
- Fake 实现支持脚本化返回工具调用，供测试使用

### B2 扩展流式事件与审批

涉及：`internal/contract`、`internal/app`、UI

- `ChatEvent` 增加事件类型：文本、工具调用、工具结果、审批请求、结束
- 新增审批接口，例如 `ResolveApproval(runID, callID string, approved bool)`
- UI 收到审批请求时弹确认框；用户拒绝时回填拒绝结果
- 危险工具没有审批实现时一律拒绝，沿用 `tool.Service` 现有约定

### B3 工具调用循环

修改 `internal/app/chat.go`：

- 收到 `Chunk.ToolCalls` 时先结束当前文本段，写入工具调用事件
- 调 `tool.Service.Execute(ctx, toolID, agentID, arguments)`
- 把工具结果作为 `role=tool` 消息追加到下一轮请求
- 最大循环轮数限制，例如 5 轮，超过后以错误结束，避免死循环
- 停止、失败、超时都要清理 Run 状态并保留已产生内容

### B4 工具调用持久化

新增迁移 `0003_tool_call.sql`：

```sql
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
```

用途：重启后仍能展示历史工具调用与结果，不依赖内存事件。

### B5 测试

- Fake 返回一次工具调用，验证请求、执行、结果回填、最终回复
- 危险工具无审批时拒绝
- 用户拒绝审批时模型收到拒绝结果
- 工具执行超时、panic、输出超限
- 最大轮数保护

验收：

- 模型能完成「调用工具 -> 得到结果 -> 继续回答」的完整链路
- 工具调用历史可在重启后查看
- 插件未授权的 Agent 无法调用工具

## 5. 阶段 C：长期记忆（P1）

按 [memory-design.md](./memory-design.md) 执行，前置依赖只有一个：模型网关补非流式调用。

### C1 非流式模型接口

- 新增 `llm.Completer` 或 `Complete` 方法，返回完整文本
- OpenAI 实现走同一下游接口，Fake 返回固定 JSON
- 供记忆抽取与摘要使用，不影响现有流式接口

### C2 记忆存储

- 迁移 `0002_memory.sql`：`memory`、`memory_digest`、`memory_digest_item`
- `domain` 增加 `Memory`、`MemoryDigest`
- `Store` 增加 `Memories`、`MemoryDigests` 子 store
- 测试覆盖作用域隔离、去重、冲突替代、按日查询

### C3 记忆写入

- 所有写入都由 AI 抽取判断，不做关键词直写
- 回复完成后异步抽取，固定 JSON，每轮最多 3 条
- 只记录用户陈述或确认过的内容，不记录助手猜测
- 写入成功后再把 `message.memory_written` 置为 true

### C4 记忆检索与注入

- 始终注入：置顶、最近 7 天、关键词命中
- 需要回忆旧事时：先查摘要，再回查对应周期的原子记忆
- 固定上下文预算，例如 2000 字，超出按优先级截断
- 注入时带日期

### C5 日摘要

- 按 `day_key` 聚合，按天或积累 N 条后批量生成
- 记忆被修改时摘要标记 dirty，下次重新生成
- 先做日摘要，数据量上来再加月摘要

### C6 记忆管理 UI

- Agent 设置中的「允许长期记忆」开关
- 记忆列表：查看、编辑、删除、置顶
- 「已记住」可查看本次写入的记忆

验收：

- 明确「记住」后重启仍能回忆
- 普通闲聊不会产生大量无用记忆
- 记忆注入量有上限，不随使用时间线性增长
- 删除记忆后不再被检索

## 6. 阶段 D：产品补齐与质量（P1/P2）

### D1 消息分页（P1）

- `MessageStore` 增加 `ListPage(ctx, conversationID, beforeSeq, limit)`
- contract 增加分页读取方法，UI 改为向上滚动加载
- 首次只加载最近 50 条
- 测试 1 万条消息下首屏加载时间与内存占用

### D2 模型列表真实化（P2）

- 移除 `app.chatModels()` 的硬编码演示模型
- 启动时缓存 `provider.Models()` 或读取配置中的模型列表
- 移除 `requestModel` 的演示 ID 映射
- 模型不可用时给出中文错误，而不是静默回退

### D3 内置包与生产种子（P2）

- 往 `internal/builtin/apps`、`internal/builtin/plugins` 放入真实 tar 包
- 生产构建不导入演示会话，只安装内置应用/插件
- 开发版保留 `BOWKNOT_SEED` 开关
- 验证首次启动、重复启动、升级后都不会重复安装

### D 应用启动（P2）

- `apps.Hooks.Open` / `Close` 目前是空实现，点击应用没有反应
- `Manifest.Entry` 已解析但没有写入 `domain.App`，需要补字段与迁移
- 增加应用启动服务：读取 `InstallDir + Entry`，在隔离 WebView 中打开
- 关闭应用时调用 `WebView.Destroy()`，避免内存泄漏
- 第三方应用必须运行在 Untrusted 沙箱，不能进入受信任页面

### D5 AIGC 与工作台（P2）

- AIGC 页目前只有左侧菜单，右侧为占位
- 工作台路由在壳层中仍是占位视图
- 这两个属于产品功能，需要单独定义 contract 与页面，不挂在基础设施计划里

### D6 质量与运维（P2）

- 在支持 CGO 的 CI 环境补 `go test -race`
- `0001_init.sql` 中 `UNIQUE(conversation_id, seq)` 与 `idx_message_conversation` 重复，新增迁移删除重复索引
- 提供数据库备份、清理旧会话、`VACUUM` 的运维入口
- 补充应用/插件真实化后的端到端测试

## 7. 建议顺序

```text
A（Apps/Plugins 真实化）
  ├─> B（工具调用闭环，依赖 A 的工具注册表）
  └─> C（记忆系统，只依赖非流式模型接口）
        └─> D1（分页）与 D2-D5 可并行
```

先做 A 的原因：包管理、工具授权、内置包都依赖应用/插件数据，A 不通，B 的工具授权没有真实数据可校验。

C 可以在 A 之后与 B 并行，因为记忆只依赖模型网关，不依赖工具调用。

## 8. 验证方式

每个阶段至少执行：

```bash
gofmt -l .
go vet ./...
go build ./...
go test ./... -count=1
```

窗口交互、安装动画、审批弹窗等仍由人工运行 `go tool mygo dev` 确认。

## 9. 风险

- 工具事件与审批会改 `contract`，需要 UI 同步改，不能只改后端
- 工具调用循环需要严格限制轮数和输出大小，否则可能死循环或撑爆上下文
- 记忆抽取会增加模型调用成本，必须异步并限制每轮条数
- 远程包预览和安装需要统一超时、大小上限与校验策略
- 应用启动涉及 WebView 沙箱，安全和内存释放要在设计阶段定清楚
