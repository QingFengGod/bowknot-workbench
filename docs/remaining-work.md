# 未完成事项清单

> 核对日期：2026-11-16
> 用途：记录基础设施阶段 0-6 之后仍未实现或仍为占位的部分，方便后续逐项勾选。
> 配套计划见 [next-plan.md](./next-plan.md)。

## 1. 已完成基线

以下能力已经实现，不在本清单重复展开：

- SQLite Store、迁移、8 个子 store、开发版种子
- 助手/Agent 会话与消息落盘、Run 中断收敛
- OpenAI 兼容流式网关 + Fake、错误分类、首 token 前重试
- 工具注册表、授权校验、审批接口、审计日志
- tar + manifest 包管理、下载校验、原子安装/卸载
- 目录、生命周期、ID、日志轮转、启动自检

## 2. 未完成事项

### 2.1 Apps

- [ ] **P0 AppCatalog 真实实现**
  当前 `cmd/desktop/main.go` 仍使用 `backend.NewAppCatalog()`，数据来自内存。
  需要新增 `internal/app/catalog.go`，改为读 `store.Apps`。

- [ ] **P0 包预览接口**
  `pkgs.Installer` 只有安装/卸载，没有预览；`contract.AppCatalog.Preview` 只能由 mock 提供。
  需要新增 `Installer.Preview`，支持本地目录、本地 tar、远程 tar，且不写数据库。

- [ ] **P0 安装/卸载/工具开关/清空数据接入**
  `Install`、`Uninstall`、`SetToolEnabled`、`ClearData`、`MarkUsed` 需要全部走 `pkgs.Installer` 与 `store.Apps`。

- [ ] **P2 应用启动**
  `internal/ui/pages/apps/apps.go` 的 `Hooks.Open` / `Close` 已定义，但壳层注入的是空 `apps.Hooks{}`，点击应用没有反应。
  同时 `Manifest.Entry` 没有写入 `domain.App`，数据库也没有入口字段。
  需要补字段、迁移、启动服务与隔离 WebView。

- [ ] **P2 种子应用与真实安装记录混用**
  `store.seed.go` 会插入演示应用和插件，但没有对应的 `InstallDir` 与包文件。
  切换到真实 `AppCatalog` 后，需要确认这些记录可展示、可卸载、不会误删真实目录。

### 2.2 Plugins

- [ ] **P0 PluginService 真实实现**
  当前 `cmd/desktop/main.go` 仍使用 `backend.NewPluginService()`。
  需要新增 `internal/app/plugins.go`，插件与授权关系改为读 `store.Plugins`。

- [ ] **P0 插件安装/卸载/授权**
  `Install`、`Uninstall`、`SetAuthorizedAgents` 需要走 `pkgs.Installer` 和 `store.Plugins.SetGrants`。

- [ ] **P1 主 Agent ID 统一**
  mock 中主 Agent 的 ID 是 `main-agent`，而 `store.Agents` 里的 Agent ID 由种子/用户创建决定。
  插件授权需要明确主 Agent 的真实 ID，避免授权记录对不上。

- [ ] **P2 插件卸载的数据保留语义**
  `contract.PluginService.Uninstall(id)` 没有 `keepData` 参数。
  需要确定插件卸载是保留还是删除插件数据目录，目前计划默认删除。

### 2.3 工具调用

- [ ] **P0 tool.Service 接入启动链路**
  `internal/infra/tool` 已实现，但没有任何调用方；`main.go` 没有创建 `tool.Service`。
  需要注入应用/插件服务，并在启动和安装/卸载后调用 `SyncApps` / `SyncPlugins`。

- [ ] **P0 模型请求支持工具声明**
  `llm.ChatRequest` 只有 `Model`、`System`、`Messages`，没有 `Tools`。
  OpenAI 实现不会向模型声明可用工具，模型也就不会发起工具调用。

- [ ] **P0 流式事件支持工具调用与审批**
  `contract.ChatEvent` 只有 `Content`、`Status`、`Error`、`Done`，没有工具调用、工具结果、审批请求事件。
  需要扩展事件模型，并增加审批回执接口。

- [ ] **P0 对话循环执行工具**
  `internal/app/chat.go` 目前只转发文本增量，忽略 `llm.Chunk.ToolCalls`。
  需要实现「模型请求工具 -> 授权/审批 -> 执行 -> 结果回填 -> 继续生成」的循环，并限制最大轮数。

- [ ] **P1 工具调用持久化**
  数据库没有 `tool_call` 表，重启后无法恢复历史工具调用与结果。
  需要新增迁移与子 store。

- [ ] **P1 审批 UI**
  `tool.Approver` 是同步回调，目前没有 UI 接入。
  危险工具在接入后如果 `Approver` 为 nil，会一律拒绝；需要补确认弹窗与回执。

- [ ] **P1 真实工具执行器**
  `tool` 包当前只有 in-process 占位执行器。
  应用/插件工具的真实执行方式、进程隔离、超时与输出限制仍需实现。

### 2.4 长期记忆

- [ ] **P1 记忆表与迁移**
  目前只有 `0001_init.sql`，没有 `memory`、`memory_digest`、`memory_digest_item`。

- [ ] **P1 记忆领域模型与子 store**
  `domain` 与 `store` 都没有记忆相关类型，`Store` 没有 `Memories` / `MemoryDigests`。

- [ ] **P1 非流式模型接口**
  `llm.Provider` 只有 `Stream` 和 `Models`，记忆抽取与摘要需要完整文本返回。
  需要新增 `Complete` 或独立的 `Completer` 接口。

- [ ] **P1 AI 记忆抽取**
  目前 `message.memory_written` 只是字段，mock 里硬编码为 true，没有真实写入。
  需要实现回复完成后的异步抽取、去重、冲突替代。

- [ ] **P1 记忆检索与注入**
  尚未实现置顶/最近/关键词命中检索，也没有摘要定位与回查。

- [ ] **P1 日摘要**
  尚未实现按 `day_key` 聚合的摘要生成与 dirty 重建。

- [ ] **P1 记忆管理 UI**
  没有记忆列表、编辑、删除、置顶、开关。

- [ ] **P2 FTS5 / embedding**
  首版关键词检索即可；数据量上来后再评估 FTS5 与向量检索。

### 2.5 数据与性能

- [ ] **P1 消息分页**
  `store.MessageStore` 只有 `List`，一次返回整个会话。
  需要新增 `ListPage`，contract 与 UI 改为向上滚动加载；否则长会话先卡 UI。

- [ ] **P1 首屏只加载最近消息**
  当前 UI 依赖全量 `Messages()`，需要改成最近一页 + 按需加载。

- [ ] **P2 重复索引清理**
  `0001_init.sql` 中 `UNIQUE (conversation_id, seq)` 已生成索引，`idx_message_conversation` 重复。
  需要新增迁移删除重复索引，减少写入开销与磁盘占用。

- [ ] **P2 消息搜索**
  尚未提供跨会话搜索；后续使用 FTS5，不要对 `content` 做全表 `LIKE`。

- [ ] **P2 备份、清理与 VACUUM**
  已有 `docs/operations.md`，但没有应用内运维入口。
  删除旧会话后数据库文件不会自动收缩，需要提供备份、清理、`VACUUM` 操作。

### 2.6 模型与配置

- [ ] **P2 模型列表真实化**
  `internal/app/assistant.go` 的 `Models()` 仍返回硬编码的 `chatModels()`。
  需要接入 `provider.Models()` 或配置中的模型列表。

- [ ] **P2 移除演示模型映射**
  `internal/app/assistant.go` 的 `requestModel()` 仍在把 UI 演示 ID 映射到真实模型。
  模型列表接入后应删除该映射。

- [ ] **P2 生产种子策略**
  `BOWKNOT_SEED` 当前默认导入演示会话；生产构建应只安装内置应用/插件，不导入演示对话。

- [ ] **P2 内置包内容**
  `internal/builtin/apps` 与 `internal/builtin/plugins` 只有 `.gitkeep`，`EnsureInstalled` 实际没有包可装。
  需要放入真实 tar 包并验证首次启动、重复启动、升级幂等。

### 2.7 UI 占位

- [ ] **P2 AIGC 页面**
  `internal/ui/pages/aigc/aigc.go` 明确是「只做左侧菜单，右侧占位」，没有 contract 与后端实现。

- [ ] **P2 工作台页面**
  `/workbench` 在壳层中仍是占位视图，没有原生页面或后端能力。

- [ ] **P2 应用窗口生命周期**
  `apps.Hooks.Open` / `Close` 为空，没有 WebView 创建、关闭、内存释放逻辑。

### 2.8 质量与测试

- [ ] **P1 Apps/Plugins 真实服务测试**
  目前只有 `internal/backend` 的演示数据，没有基于真实 store/installer 的服务测试。

- [ ] **P1 工具调用端到端测试**
  需要覆盖工具调用、审批、拒绝、超时、最大轮数。

- [ ] **P1 记忆系统测试**
  需要覆盖抽取、去重、冲突替代、作用域隔离、摘要回查。

- [ ] **P2 race 测试**
  `go test -race` 在 Windows 纯 Go 环境下无法执行，需要在支持 CGO 的 CI 环境补跑。

- [ ] **P2 人工验收清单**
  冷启动、异常退出、升级启动、安装/卸载、审批弹窗、WebView 关闭释放仍需人工确认。

## 3. 需要先拍板的事项

以下事项不是纯编码，需要先确认，否则实现容易返工：

1. 插件卸载是否保留数据
2. 主 Agent 的固定 ID，以及它与 `store.Agents` 的关系
3. 是否为工具调用新增 `tool_call` 表，还是只在消息内容里展示
4. 生产构建如何区分开发种子与内置包安装
5. 应用启动使用哪种 WebView，沙箱边界与关闭释放方式
6. 记忆抽取使用哪个模型，是否允许额外的模型调用成本

## 4. 维护规则

- 完成一项后勾选，并在提交信息或 `next-plan.md` 中注明验证方式
- 新增未完成项时写清优先级、影响范围和前置依赖
- 与基础设施计划冲突时，以 `infrastructure-plan.md` 的决策为准，并同步更新本清单
