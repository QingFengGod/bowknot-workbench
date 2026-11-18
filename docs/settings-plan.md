# 设置与配置中心实施方案

## 1. 背景

当前工作台的配置分散在三类位置，界面上无法统一管理：

| 配置 | 位置 | 现状 |
| --- | --- | --- |
| 模型服务 | `config.json` 单个 `model` | 只能配一个 provider、一个默认模型，且需手改文件 |
| 日志级别 | `config.json` | 只能手改文件 |
| 主题模式 | 外壳字段 `Shell.mode` | 可切换，重启丢失 |
| 主助手人设、Agent 人设与模型 | 弹窗 + 数据库实体表 | 已有局部入口 |
| 记忆总开关、Agent 记忆开关 | `app_meta` + 弹窗 | 已有局部入口 |
| 任务并发 / 超时 / 重试 | `internal/app/task.go` 常量 | 不可配 |
| 群聊最大轮次 | `internal/app/group.go` 常量 | 不可配 |
| 工具调用轮次上限 | `internal/app/chat.go` 常量 | 不可配 |
| 记忆注入条数 / 字数 / 时间窗口 / 摘要间隔 | `internal/app/memory.go` 常量 | 不可配 |
| 数据目录、备份恢复 | 仅 `docs/operations.md` | 无界面 |

问题不只是「缺一个设置页」，而是缺少统一的配置层：没有契约接口、没有默认值与校验、没有统一的生效策略。

## 2. 目标与非目标

### 目标

- 新增一级「设置」页面，覆盖模型服务、对话与提示词、任务调度、群聊与工具、记忆、外观、数据与日志、关于。
- 支持**多个 Provider** 与**多个模型**：Provider 独立配置 BaseURL 与密钥，模型绑定 Provider，网关按模型路由。
- 模型支持标记是否支持图片理解、上下文长度、最大输出长度。
- 配置按「启动依赖程度」分层存储，不把所有东西塞进一个位置。
- 行为参数保存即生效；必须重启的项在界面上明确标注。
- 模型密钥只写不读，界面只显示脱敏值。
- 非法值在保存前拦截，启动时配置损坏不阻塞运行。

### 非目标（本期不做）

- 图片理解、上下文长度、最大输出长度的**运行时行为**：本期只存储与展示，聊天暂不支持上传图片，也不做按 token 截断或 `max_tokens` 下发。
- 备份/恢复/清理的**实际功能**：本期只做页面与信息展示，动作按钮留占位，后续单独实现。
- 多 Provider 协议差异：本期所有 Provider 统一按 `openai-compatible` 协议处理。
- 应用级、插件级独立配置。
- 配置导入导出。

## 3. 存储分层

### 3.1 `config.json`（启动引导类 + 模型服务 + 密钥）

结构调整为多 Provider、多模型：

```json
{
  "logLevel": "info",
  "providers": [
    {
      "id": "p-openai",
      "name": "OpenAI",
      "type": "openai-compatible",
      "baseUrl": "https://api.openai.com/v1",
      "apiKey": "sk-...",
      "enabled": true
    }
  ],
  "models": [
    {
      "key": "p-openai/gpt-4o",
      "providerId": "p-openai",
      "modelId": "gpt-4o",
      "name": "GPT-4o",
      "vision": true,
      "contextTokens": 128000,
      "maxOutputTokens": 16384,
      "enabled": true
    }
  ],
  "defaultModelKey": "p-openai/gpt-4o"
}
```

旧版单 `model` 字段在读取时自动转换：

- 生成一个 `id = "default"`、`name = "默认服务"` 的 Provider；
- 若旧配置有 `defaultModel`，生成一条模型记录，`key = "default/<defaultModel>"`，能力字段留空；
- `defaultModelKey` 指向该模型；
- 下次保存时写回新结构，旧 `model` 字段不再输出。

旧配置 `baseUrl` 为空（演示模式）时，不生成 Provider 与模型，继续回退到 Fake 网关。

原因：日志与模型网关在数据库打开前就要初始化，不能依赖 SQLite；密钥继续留在 `0600` 权限的配置文件里。

### 3.2 `app_meta` 单键 `settings.v1`（运行期行为参数）

一次性写入一份 JSON，保证保存的原子性；新增字段时缺失项用代码默认值补全，不做数据库迁移。

```json
{
  "task": {
    "maxConcurrent": 4,
    "timeoutMinutes": 10,
    "maxAttempts": 3,
    "retryDelaysSeconds": [5, 15]
  },
  "group": {
    "maxRounds": 3,
    "maxToolRounds": 5
  },
  "memory": {
    "injectItems": 12,
    "injectBudgetRunes": 2000,
    "recentDays": 30,
    "digestIntervalHours": 6,
    "contextTaskLimit": 8
  },
  "prompt": {
    "global": ""
  },
  "appearance": {
    "theme": "system"
  }
}
```

### 3.3 沿用现有存储

- `assistant.persona`：主助手人设不变。
- `memory.enabled` / `memory.agent.<id>`：记忆开关继续由 `MemoryService` 读写，设置页调用现有接口，**不复制一份开关到 `settings.v1`**，避免双份真相。
- Agent 的模型、人设、工具授权：仍留在实体表，设置页只提供跳转入口。

## 4. 模型管理设计

### 4.1 标识规则

- Provider：`id` 由 `internal/platform/id` 生成，创建后不可修改；`name` 可改。
- 模型：内部唯一标识 `key = providerId + "/" + modelId`。
  - 上游模型 ID 不允许包含 `/`，避免拼接歧义。
  - 同一上游模型 ID 可以出现在不同 Provider 下，互不冲突。
- Agent 表存的 `model_id` 从「上游模型 ID」迁移为「内部 key」。

### 4.2 Agent 模型迁移

启动时在 App 层做一次对账（不放进 store 迁移，因为需要读取配置）：

1. 若当前没有配置任何模型（演示模式），保持 Agent 原值不动。
2. 若 `agent.model_id` 已是合法 key，跳过。
3. 若能在已配置模型中按上游 ID **唯一匹配**，改写为对应 key。
4. 否则回退到 `defaultModelKey`；没有默认模型时回退到第一个启用模型。

群聊不单独存模型，沿用群主/成员各自的 Agent 配置。

### 4.3 Provider 管理

| 操作 | 规则 |
| --- | --- |
| 新增 | 名称、BaseURL、API Key、启用开关；BaseURL 校验 http/https 且带 host |
| 编辑 | 名称、BaseURL、启用、密钥；密钥留空表示保持原值，另有「清除密钥」动作 |
| 测试连接 | 用草稿参数构造临时网关并拉取一次模型列表，不落盘 |
| 删除 | 该 Provider 下仍有模型时拒绝，提示「请先删除该服务下的 N 个模型」 |
| 停用 | 不删除数据；其下模型视为不可用，选择器中隐藏 |

密钥处理：

- 读取时一律返回 `MaskKey` 后的值，并给出 `HasKey`。
- 日志、错误、契约返回值都不出现明文密钥。
- 测试与拉取使用草稿密钥；草稿为空时回退到已保存的密钥。

### 4.4 模型管理

录入方式两种：

1. **拉取后勾选**：选择已保存的 Provider，后台调用模型列表接口，弹窗展示远端模型；已添加的置灰，勾选后批量添加。新添加的模型能力字段默认为「未标记」，由用户补填。
2. **手动补充**：填写上游模型 ID、显示名、图片理解、上下文长度、最大输出长度。拉取接口不可用或需要自定义名称时使用。

| 字段 | 说明 |
| --- | --- |
| `modelId` | 上游请求使用的模型名，不含 `/` |
| `name` | 界面显示名，空时用 `modelId` |
| `vision` | 是否支持图片理解，本期只展示 |
| `contextTokens` | 上下文长度，0 表示未填写 |
| `maxOutputTokens` | 最大输出长度，0 表示未填写 |
| `enabled` | 停用后不出现在选择器，但配置保留 |

默认模型：

- 全局只有一个 `defaultModelKey`，用于没有显式指定模型的调用（记忆摘要、任务汇报润色）。
- 必须指向已启用的模型；删除或停用默认模型时，自动回退到第一个启用模型，并在界面提示。

### 4.5 网关路由

现有 `llm.Provider` 是单连接实现，新增 `internal/infra/llm/router.go` 做多 Provider 路由：

```go
type Route struct {
    ProviderType string // 固定 openai-compatible
    BaseURL      string
    APIKey       string
    ModelID      string // 上游模型 ID
}

type RouterOptions struct {
    DefaultKey string
    Routes     map[string]Route // key 为内部模型 key
}
```

行为：

- `Stream` / `Complete`：按 `req.Model` 查路由，命中后把 `req.Model` 替换为路由里的上游 `ModelID`，再交给对应连接。
- `req.Model` 为空：使用 `DefaultKey`；没有默认模型时用第一个路由。
- `req.Model` 未命中：返回中文错误「所选模型未配置或已停用」，不静默回退到其它模型。
- `Models`：直接返回配置中已启用的模型，**启动时不再请求外部接口**，避免网络阻塞启动。
- 没有配置任何 Provider：继续返回 Fake 网关，保证首次启动可用。
- 配置在启动时构建，**修改后需重启生效**；界面明确标注。

### 4.6 选择器

`contract.ChatModel` 增加 `ProviderName`、`Vision`、`ContextTokens`、`MaxOutputTokens` 字段：

- ID 使用内部 key；
- 名称显示「模型名」，副标题显示服务商与能力摘要（如「OpenAI · 128K 上下文 · 支持图片」）；
- 各页面模型选择器继续复用 `components.ModelPicker`，无需改交互。

## 5. 运行参数：字段、默认值与范围

### 任务调度

| 字段 | 默认 | 范围 | 说明 |
| --- | --- | --- | --- |
| `maxConcurrent` | 4 | 1–16 | 全局同时执行的后台任务数 |
| `timeoutMinutes` | 10 | 1–120 | 单任务最长执行时间 |
| `maxAttempts` | 3 | 1–5 | 总尝试次数（首次 + 重试） |
| `retryDelaysSeconds` | `[5, 15]` | 每项 0–300 | 第 n 次失败后的退避；长度按 `maxAttempts-1` 截断或补默认值 |

### 群聊与工具

| 字段 | 默认 | 范围 | 说明 |
| --- | --- | --- | --- |
| `maxRounds` | 3 | 1–10 | 单条用户消息触发的成员讨论轮次上限 |
| `maxToolRounds` | 5 | 1–20 | 单次生成内连续工具调用轮数上限 |

### 记忆

| 字段 | 默认 | 范围 | 说明 |
| --- | --- | --- | --- |
| `injectItems` | 12 | 1–50 | 单次注入记忆条数上限 |
| `injectBudgetRunes` | 2000 | 500–8000 | 注入记忆块的字符预算 |
| `recentDays` | 30 | 1–365 | 「最近记忆」时间窗口 |
| `digestIntervalHours` | 6 | 1–168 | 后台摘要任务间隔 |
| `contextTaskLimit` | 8 | 0–30 | 员工直聊时注入的历史任务条数 |

### 全局提示词

| 字段 | 默认 | 上限 | 说明 |
| --- | --- | --- | --- |
| `global` | 空 | 4000 字 | 追加到所有 Agent 系统提示词的自定义内容 |

规则：

- 作用域：主助手、员工、群主。
- 位置：在内置规则与人设之后、记忆检查块之前。
- 不包含：内置工具说明、插件说明、人设字段、群聊协作规则——这些仍由代码生成，不在配置界面展示。
- 为空时不注入，不产生空行。

### 外观

| 字段 | 默认 | 取值 |
| --- | --- | --- |
| `theme` | `system` | `system` / `dark` / `light` |

### 模型字段校验

| 字段 | 规则 |
| --- | --- |
| Provider `name` | 1–40 字 |
| Provider `baseUrl` | 空允许（表示未配置），非空需 http/https 且带 host |
| Provider `type` | 仅 `openai-compatible` |
| 模型 `modelId` | 1–100 字，不含 `/`，同一 Provider 内唯一 |
| 模型 `name` | 0–60 字 |
| `contextTokens` | 0 或 1000–10,000,000 |
| `maxOutputTokens` | 0 或 1–1,000,000；与上下文同时非零时不得超过上下文 |
| `defaultModelKey` | 空或指向已启用模型 |

## 6. 契约设计

新增 `internal/contract/settings.go`，只放 DTO 与接口，不依赖 mygo 或具体实现。

### 6.1 运行设置

```go
type TaskSettings struct {
    MaxConcurrent      int   `json:"maxConcurrent"`
    TimeoutMinutes     int   `json:"timeoutMinutes"`
    MaxAttempts        int   `json:"maxAttempts"`
    RetryDelaysSeconds []int `json:"retryDelaysSeconds"`
}

type GroupSettings struct {
    MaxRounds     int `json:"maxRounds"`
    MaxToolRounds int `json:"maxToolRounds"`
}

type MemorySettings struct {
    InjectItems         int `json:"injectItems"`
    InjectBudgetRunes   int `json:"injectBudgetRunes"`
    RecentDays          int `json:"recentDays"`
    DigestIntervalHours int `json:"digestIntervalHours"`
    ContextTaskLimit    int `json:"contextTaskLimit"`
}

type PromptSettings struct {
    Global string `json:"global"`
}

type AppearanceSettings struct {
    Theme string `json:"theme"`
}

type Settings struct {
    Task       TaskSettings       `json:"task"`
    Group      GroupSettings      `json:"group"`
    Memory     MemorySettings     `json:"memory"`
    Prompt     PromptSettings     `json:"prompt"`
    Appearance AppearanceSettings `json:"appearance"`
}

type SettingsService interface {
    Settings() Settings
    Save(next Settings) error
    Reset(section string) error
}
```

### 6.2 模型配置

```go
type ProviderConfig struct {
    ID      string `json:"id"`
    Name    string `json:"name"`
    Type    string `json:"type"`
    BaseURL string `json:"baseUrl"`
    APIKey  string `json:"apiKey"`  // 始终为脱敏值
    HasKey  bool   `json:"hasKey"`
    ClearKey bool  `json:"clearKey"` // 保存时显式清除密钥
    Enabled bool   `json:"enabled"`
    ModelCount int `json:"modelCount"`
}

type ModelConfig struct {
    Key             string `json:"key"`
    ProviderID      string `json:"providerId"`
    ModelID         string `json:"modelId"`
    Name            string `json:"name"`
    Vision          bool   `json:"vision"`
    ContextTokens   int    `json:"contextTokens"`
    MaxOutputTokens int    `json:"maxOutputTokens"`
    Enabled         bool   `json:"enabled"`
}

type ModelConfigService interface {
    Providers() []ProviderConfig
    // draft.APIKey 为空表示保持原密钥；draft.ClearKey 为 true 时清除。
    SaveProvider(draft ProviderConfig) (string, error)
    DeleteProvider(id string) error
    TestProvider(draft ProviderConfig) (string, error)
    // 使用已保存的凭据拉取上游模型列表。
    FetchModels(providerID string) ([]ChatModel, error)

    Models() []ModelConfig
    SaveModel(draft ModelConfig) error
    DeleteModel(key string) error
    DefaultModel() string
    SetDefaultModel(key string) error
    // 供各页面模型选择器使用，只返回启用且所属 Provider 也启用的模型。
    ChatModels() []ChatModel
}
```

约定：

- `Defaults()` 返回完整默认值，供服务初始化与「恢复默认」使用。
- `Normalize()` 负责裁剪空白、修正越界值；`Validate()` 负责拒绝非法值并返回中文错误。
- 设置页展示与保存都走契约接口，不直接读写 `config.json` 或 `app_meta`。

## 7. 实现结构

新增 `internal/app/settings.go`（运行设置）：

- 持有 `*store.Store`、logger、读写锁与内存快照。
- 启动时读取 `settings.v1`：不存在 → 默认值；解析失败 → 默认值 + 警告日志，且**不覆盖**原值，等待用户保存时再重写。
- `Save`：`Normalize` → `Validate` → 序列化 → `Meta.Set` → 更新快照。
- 暴露只读快照方法给同包服务：`Task()`、`Group()`、`Memory()`、`PromptText()`、`ThemeMode()`。

新增 `internal/app/modelconfig.go`（模型配置）：

- 持有配置文件路径、logger、互斥锁。
- 所有读写都经 `internal/infra/config`，复用 `Load` / `Save` / `MaskKey`。
- 保存 Provider 或模型后返回提示「已保存，重启后生效」。
- `FetchModels` / `TestProvider` 使用临时网关，超时 15 秒，错误信息不透出密钥。
- 保存时整体重写日志级别与模型配置，避免丢失无关字段。

新增 `internal/infra/llm/router.go`：

- 实现 `llm.Provider`，按内部 key 路由。
- 每个 Provider 启动时构建一次连接（`WithRetry` 包装），配置修改重启后重建。

服务注入：

| 服务 | 读取内容 | 生效方式 |
| --- | --- | --- |
| `main` | Provider 与模型列表 | 启动时构建 Router；无配置回退 Fake |
| `AssistantService` / `AgentService` | 启用模型列表 | 启动时 `SetModels` 由 `ChatModels()` 填充，不再请求网关 |
| `TaskService` | 并发、超时、尝试次数、退避 | 每次派发/失败时读取；并发调小时不再出队，直到低于上限 |
| `GroupService` | 群聊轮次 | 每轮开始时读取 |
| `chat` 工具循环 | 工具轮次 | 每次生成开始时读取 |
| `AgentService.groupMainPrompt` | 群聊轮次 | 构建提示词时读取，替换写死的「最多 3 轮」 |
| `MemoryService` | 注入条数/预算/窗口/直聊任务条数 | 每次检索与注入时读取 |
| `MemoryService` 摘要 | 摘要间隔 | 每轮结束后按当前值重新挂定时器，下一周期生效 |
| 提示词构建 | 全局提示词 | 每轮构建系统提示词时读取 |

提示词常量需要拆分：把 `assistantSystemPrompt`、`defaultAgentSystemPrompt`、`groupSystemPrompt` 末尾的 `memoryCheckGuide` 拆出来，改为「基础规则 + 人设 + 全局提示词 + 记忆检查」的顺序拼接，保证全局提示词位置稳定。

Agent 模型对账放在 `AgentService` 初始化或 `main` 装配阶段，读取 `ChatModels()` 后统一改写不合法或旧的 `model_id`。

## 8. UI 设计

### 8.1 入口

- 一级导航新增 `{Route: "/settings", Label: "设置", Icon: components.IconSettings}`，放在「插件」之后。
- `components/icons.go` 新增 lucide 风格 gear 图标。
- `shell.Deps` 增加 `Settings contract.SettingsService` 与 `Models contract.ModelConfigService`。
- 外壳启动时用 `settings.Settings().Appearance.Theme` 初始化主题；标题栏主题菜单切换后写回设置，保持两处同步。

### 8.2 页面结构

```
internal/ui/pages/settings/
├── settings.go     页面骨架：左侧分类 + 右侧滚动表单
├── model.go        服务商与模型管理
├── model_dialogs.go 服务商编辑、模型编辑、拉取模型弹窗
├── prompt.go       对话与提示词
├── task.go         任务调度
├── group.go        群聊与工具
├── memory.go       记忆
├── appearance.go   外观
├── data.go         数据与日志
└── about.go        关于
```

页面状态放在 `Page` 结构体：草稿 `draft contract.Settings`、当前分类、保存状态（`idle/saved/error`）、服务商与模型草稿、各弹窗开关（稳定指针）、拉取结果与加载状态、是否脏。

交互：

- 进入页面时从服务拉取快照填充草稿。
- 「保存」按当前分类校验并保存；失败保留草稿并显示中文错误。
- 「恢复默认」只重置当前分类，需二次确认。
- 模型相关保存后显示「已保存，重启后生效」；日志级别同理。
- 有未保存修改时切换分类，保留草稿，不弹窗打断。

### 8.3 新增组件

按「一组件一文件」放 `internal/ui/components/`：

| 组件 | 用途 |
| --- | --- |
| `SettingsField` | 标签 + 说明 + 控件的统一行布局 |
| `NumberField` | 数字输入，带范围与单位，提交时钳制 |
| `SelectField` | 基于 `Popover` + `PopoverItem` 的下拉选择 |
| `SwitchField` | 开关，复用 `ui.Checkbox` 外观或自绘胶囊 |

文本输入复用 `ui.TextInput` / `ui.TextAreaBase`，不重复造。

### 8.4 模型服务分类

页面自上而下：

1. **服务商列表**：每张卡片显示名称、BaseURL、密钥状态（已配置/未配置）、模型数、启用开关；操作「测试连接」「编辑」「删除」。
2. **顶部操作**：「添加服务商」「拉取模型」「手动添加模型」。
3. **模型列表**：按服务商分组，每条显示显示名、上游 ID、能力标签（图片 / 128K 上下文 / 16K 输出）、默认模型标记、启用开关；操作「编辑」「删除」「设为默认」。
4. **弹窗**：
   - 服务商编辑：名称、BaseURL、API Key（密码框，占位为脱敏值，留空不覆盖）、启用、清除密钥。
   - 模型编辑：上游模型 ID、显示名、图片理解（勾选）、上下文长度、最大输出长度、启用。
   - 拉取模型：后台拉取，列表勾选，已添加项置灰，底部「添加所选」。

异步规则：`FetchModels` 与 `TestProvider` 在后台 goroutine 执行，完成后通过壳层注入的 `Update` 钩子回写页面状态；期间按钮显示「拉取中…」「测试中…」，禁止重复提交。页面需要新增 `Hooks{Update: s.update}`。

### 8.5 其余分类

1. **对话与提示词**：主助手人设入口（跳转助手页弹窗）、全局提示词（多行，带字数统计与上限）。
2. **任务调度**：并发、超时、尝试次数、退避。
3. **群聊与工具**：群聊轮次、工具轮次。
4. **记忆**：总开关（走 `MemoryService`）、注入条数、字数预算、时间窗口、摘要间隔、直聊任务条数。
5. **外观**：跟随系统 / 暗黑 / 明亮，即时生效并持久化。
6. **数据与日志**：数据目录（只读）、数据库与各目录占用（读取文件系统统计）、日志级别（重启生效）；「打开目录」「导出备份」按钮先占位，标注「功能开发中」。
7. **关于**：应用名、版本（`AppService.Info()`）、数据目录、配置路径。

## 9. 校验与容错

- 数字越界：保存时拒绝并提示「并发数需在 1–16 之间」这类中文信息。
- `retryDelaysSeconds` 长度与 `maxAttempts` 不一致时，保存前自动截断/补默认值，并在界面提示。
- 全局提示词超过 4000 字拒绝保存。
- 主题值非法回退 `system`。
- `config.json` 损坏：启动失败并给出中文原因（保持现有行为）；设置页只负责避免写入非法值。
- `settings.v1` 损坏：回退默认值，应用照常启动。
- 模型配置引用失效（Provider 不存在、默认模型不存在、Agent 指向未知 key）：启动时对账修复并在日志中记录。
- 删除 Provider 有模型时拒绝；删除或停用默认模型时自动切换默认。

## 10. 改动文件清单

新增：

- `internal/contract/settings.go`（运行设置 + 模型配置 DTO 与接口）
- `internal/app/settings.go`
- `internal/app/modelconfig.go`
- `internal/app/settings_test.go`
- `internal/app/modelconfig_test.go`
- `internal/infra/llm/router.go`
- `internal/infra/llm/router_test.go`
- `internal/ui/pages/settings/*.go`
- `internal/ui/components/settings_field.go`
- `internal/ui/components/number_field.go`
- `internal/ui/components/select_field.go`
- `internal/ui/components/switch_field.go`
- `docs/settings-plan.md`（本文档）

修改：

- `internal/infra/config/config.go`：新增 `Provider` / `Model` / `DefaultModelKey`，旧 `model` 字段兼容与转换。
- `cmd/desktop/main.go`：创建并注入运行设置与模型配置服务；用 Router 替代单网关；去掉启动时的模型列表网络请求；执行 Agent 模型对账。
- `internal/ui/shell/shell.go`：导航项、依赖、主题初始化与持久化。
- `internal/ui/components/icons.go`：新增 `IconSettings`。
- `internal/contract/assistant.go`：`ChatModel` 增加服务商与能力字段。
- `internal/app/assistant.go`、`internal/app/agent.go`：模型列表来源改为配置；群聊提示词参数化；注入全局提示词。
- `internal/app/task.go`：并发、超时、尝试次数、退避改为读设置。
- `internal/app/group.go`：轮次上限改为读设置。
- `internal/app/chat.go`：工具轮次与提示词常量拆分。
- `internal/app/memory.go`：注入预算与摘要间隔改为读设置。
- `internal/app/models.go` 及 `models_test.go`：`ResolveModels` 的启动拉取逻辑不再使用，改为配置驱动的模型列表并同步测试。

不改：

- `internal/store`：`app_meta` 已足够，无需迁移；Agent 模型改写走现有更新接口。

## 11. 分期与验收

### P1 模型配置与路由

- 重构 `config` 结构并支持旧配置转换。
- 新增 `ModelConfigService` 与 `llm.Router`。
- 启动装配改为配置驱动，完成 Agent 模型对账。
- 单元测试：旧配置转换、key 唯一性、路由命中与未命中、默认模型回退、密钥脱敏与留空保持。
- 验收：`gofmt`、`go vet ./...`、`go build ./...`、`go test ./...` 全部通过。

### P2 服务商与模型管理 UI

- 设置页「模型服务」分类：服务商列表、模型列表、三个弹窗、异步拉取与测试。
- 验收：编译通过；增删改、拉取、默认模型切换、重启生效提示齐全；界面效果人工确认。

### P3 运行设置骨架

- `settings.v1` 服务、设置页分类骨架、任务/群聊/记忆/提示词/外观表单。
- 验收：默认值、校验、保存往返正确；主题持久化。

### P4 服务接入

- 任务、群聊、工具、记忆、全局提示词改为读设置。
- 验收：修改参数后无需重启即生效；任务并发调整不产生重复执行。

### P5 数据与日志、关于、收尾

- 展示数据目录与占用、日志级别、版本信息；备份相关按钮占位。
- 补齐错误文案，更新 `docs/operations.md` 中与设置页相关的说明。
- 验收：数据目录展示正确；占位按钮不触发未实现逻辑；全量 `gofmt` / `vet` / `build` / `test` 通过。

## 12. 风险与待定

- **模型配置重启生效**：Provider 与模型变更不热重建网关，避免运行中的流式请求被替换；界面必须明确提示。
- **模型 key 迁移**：旧 Agent 的 `model_id` 可能无法唯一匹配，回退默认模型需记录日志，避免静默改错。
- **同名模型**：同一上游 ID 出现在多个 Provider 时，界面按服务商分组展示，避免用户误选。
- **能力字段暂无行为**：图片理解只是标记，聊天上传能力后续接入；上下文长度与最大输出长度只展示，不做截断与参数下发。
- **异步拉取**：网络失败需给出中文提示且不阻塞页面；必须经 `Update` 钩子回主线程改状态。
- **数据页占位**：明确标注「功能开发中」，避免用户以为可以备份。
- **后续可扩展**：备份恢复、清理数据、分页大小、产物大小限制、图片上传、多协议 Provider。
