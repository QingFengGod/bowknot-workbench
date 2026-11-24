# AGENTS.md

本文件是给 AI 编码代理的项目约定。开始任务前请先阅读，改动时遵循以下规则。

## 1. 项目概览

bowknot-workbench 是一个本地优先的 AI 助理与多 Agent 协作工作台，为 **mygo 原生 Go 桌面端**。

- 原生 UI 优先：立即模式 UI，无 WebView，页面状态存放在页面结构体字段中
- 后端与 UI 分离：UI 只调用 `internal/contract` 接口，不感知数据来源；持久化统一走 `internal/store`（SQLite），`internal/backend` 保留为行为对照
- 品牌：主题色 `#fb5215`，主题 token 见 `internal/ui/theme`

## 2. 必须遵守的协作规则

1. **复用优先，禁止堆砌一次性代码**
   - 可复用的 UI 组件放 `internal/ui/components/`
   - 通用格式化函数放 `internal/ui/format/`
   - 新增前先搜索现有实现；同一段逻辑出现 3 次以上必须抽离
2. **验收方式**
   - **不得主动启动桌面窗口或调用浏览器 / 电脑控制插件**，交互效果需人工运行确认
   - 默认通过 `gofmt`、`go vet`、`go build`、`go test` 与代码审查完成自验
3. **立即模式 UI 的约束**
   - 页面状态必须存放在页面结构体的持久字段中，不要在视图构建函数里用临时变量承载跨帧状态（例如折叠开关、滚动位置、流式缓冲）
   - 需要 `*bool` / `*int` 之类指针的场景（如 Collapsible、Checkbox），用包级状态或字段里的指针，确保每帧指向同一地址
   - 后台事件（流式回复等）禁止直接改 UI 状态，必须通过壳层注入的 `Update` 钩子回到主线程
4. **新增依赖须先确认**
   - 引入任何新依赖前，先说明用途与替代方案，得到明确同意后再修改 `go.mod`
   - `go mod tidy` 由代理执行前同样需要说明
5. **改动保持最小范围**，不顺手重构、不修改与任务无关的文件

## 3. 目录约定

```text
cmd/desktop/main.go          启动入口：自检、装配 store/网关/安装器并注入壳层
internal/contract/           接口与 DTO，一个模块一个文件（app / assistant / agent…）
internal/backend/            内存 mock 实现，保留为行为对照（助手/Agent 已切 store）
internal/domain/             领域模型，store 与 app 层共用
internal/store/              SQLite 持久化：单门面 + 8 子 store + Tx 克隆 + 迁移 + 种子
internal/infra/
├── config/                  config.json 加载校验，MaskKey 脱敏
├── llm/                     模型网关：OpenAI 兼容 SSE + Fake，重试/取消/错误分类
├── package/                 包管理（包名 pkgs）：manifest、安全解包、下载、安装卸载
└── tool/                    工具注册表与执行外壳：授权校验、审批、审计
internal/builtin/            embed 的内置应用/插件 tar，首次启动安装
internal/platform/           paths / lifecycle / id / logging（日志 10MB 轮转保留 5 份）
internal/ui/
├── shell/                   窗口外壳：自绘标题栏、一级导航、页面注册与切换
├── pages/                   原生页面，一页一个目录（apps / assistant / agents / plugins / aigc）
├── components/              公共组件与图标（Button、Card、Avatar、Drawer、Icon 等）
├── theme/                   品牌主题与明暗色 token
└── format/                  时间、相对时间、文件大小格式化
docs/                        product-overview.md（产品总览、完成度、待确定项与运行说明）
```

- 新页面实现后注册进 `shell.New` 的 `native` 表，路由与一级导航对齐
- 页面间不互相 import；共享逻辑下沉到 `components` 或 `contract`

## 3.1 基础设施层约定

- `database/sql` 只允许出现在 `internal/store` 内；结构化数据访问一律经 `store.Store`，升级 schema 只新增迁移文件，不改历史迁移
- 事务内禁止网络请求、解包、模型调用等外部 I/O；跨聚合写入走 `store.Tx`
- `llm.Provider` 是模型调用的唯一入口，密钥不出现在日志、错误与 contract 返回值中；`Chunk.Err` 为流中错误，`Stream` 返回的 error 表示尚未产生输出、可安全重试
- 工具调用必须经 `tool.Service.Execute`，不允许绕过授权校验与危险审批直调执行器；`Approver` 为 nil 时危险工具一律拒绝
- 包安装/卸载必须经 `pkgs.Installer`（原子移动 + 同事务写记录）；手动操作 `apps/`、`plugins/` 目录会造成记录与文件不一致
- `infra/*` 包不依赖 `internal/ui` 与 `internal/backend`；依赖方向：`cmd → app → infra/store`

## 4. 编码规范

- 注释、UI 文案使用中文；注释只解释「为什么」，不复述代码
- 颜色、圆角、间距优先使用 `c.Theme()` 的 token（`t.Accent`、`t.Surface` 等），不写死颜色值
- 图标统一走 `components.Icon` 与 `components.IconName`，新增图标使用 lucide 风格 SVG（24 viewBox、stroke 2）
- 错误信息返回给 UI 层展示时使用中文
- 并发：mock 服务的流式事件用带缓冲 channel + goroutine，锁内不做阻塞操作，回调前先释放锁
- 单个函数只做一件事；视图构建函数过长时拆成私有方法

## 5. UI 规范

### 5.1 颜色 token

- 颜色只从 `theme.For(c, mode)` 的结果取，禁止写死色值。
- 主要 token：`Background` / `Surface` / `SurfaceHover` / `SurfacePressed` / `Border` / `Text` / `TextMuted` / `Accent`（品牌色 `#fb5215`）/ `AccentHover` / `AccentPressed` / `AccentText` / `Danger` / `Warning` / `Success`。
- 明暗两套色板都在 `theme.For` 内定义。`Border` 与 `SurfaceHover` 的对比是刻意拉开的（明亮模式 Border 更深、暗黑模式 Border 更亮），不要改成相近值，否则描边会和悬停底色糊在一起。

### 5.2 圆角与间距

- 圆角 token：`RadiusXS` 4、`RadiusSM` 8、`Radius` 12、`RadiusLG` 16；胶囊用 999，小圆点按半径取一半。
  - 小控件（图标按钮、菜单项）用 `RadiusXS` / `RadiusSM`；卡片、浮层、输入区用 `Radius`；大容器用 `RadiusLG`。
- 间距一律用 `t.Space(n)`，基准 `Spacing = 4`：
  - 页面主布局、输入区、弹层内容、侧栏的 padding 与 gap 用 `t.Space(3)`（12px）。
  - `Card` 保持自身默认（padding 14、gap 8）；元素内部紧凑排版（元信息行、操作栏、分组列表）允许固定小值（2 / 3 / 6），不必强行套 `Space`。
- 不要为了视觉微调去改全局 `Theme.Spacing`，它会连带放大所有内置组件。
- 自绘组件的尺寸（弹层、抽屉、头像、输入框）使用固定值或不依赖 `Spacing` 的算法，保证调整全局间距时外观不变。

### 5.3 阴影

- 三档 token：`theme.NormalShadow`（普通悬浮元素）、`theme.CardShadow`（卡片、标题栏、侧边导航）、`theme.PanelShadow`（弹窗、菜单、抽屉）。
- 用法：`theme.PanelShadow.Apply(c, panel)`，返回原元素以便继续链式调用。
- 阴影颜色在 token 里分 `Light` / `Dark` 两套，暗黑模式使用灰色光晕；不要写死纯黑，深色背景上不可见。

### 5.4 字体与图标

- 字号约定：11 辅助信息、12 次要文字与分组标题、13 正文与按钮、14 区块标题、16 弹窗标题；标题统一 `Bold()`，超过一行用 `MaxLines(1)`。
- 图标统一走 `components.Icon(c, components.IconName, size)`：
  - lucide 风格，24 viewBox、stroke 2；
  - 常用尺寸：11（箭头）、13（小操作）、15/16（工具按钮）、24（导航与品牌）；
  - 新增图标命名为 `IconXxx`，集中在 `icons.go`。

### 5.5 组件清单

`internal/ui/components/` 下一个组件一个文件，组件只接收值与指针、不持有跨帧状态：

| 组件 | 用途 | 关键约定 |
| --- | --- | --- |
| `Button` | 主/次/危险/幽灵按钮 | `ButtonVariant` 决定配色，`Width` 为 0 时自适应 |
| `Card` | 内容卡片 | `Hoverable` 时悬停显示品牌色描边 |
| `Chip` / `Tone` | 状态标签 | 固定 11px 字号、胶囊圆角 |
| `Avatar` | 单实体头像 | 圆角方块，取名称首字 |
| `EmptyState` | 空状态 | 图标 + 标题 + 说明 + 可选操作 |
| `SectionTitle` / `InfoRow` | 分节标题 / 名称值信息行 | 纯展示，无交互 |
| `SessionItem` | 会话列表行 | `MenuOpen *bool` 由调用方持有 |
| `MessageItem` | 消息行外壳 | `Avatar` / `Body` / `Meta` / `Actions` 全插槽 |
| `MessageView` | 完整消息渲染 | 组装 `MessageItem`，含停止/重答/删除 |
| `Composer` | 输入区 | `Draft *string`、`FocusRequested *bool`、`Submit` / `Cancel` / `Actions` |
| `ModelPicker` | 模型选择器 | `Open *bool` 由调用方持有 |
| `QuestionNav` | 提问导航 | 依赖消息列表元素做滚动定位 |
| `Popover` / `PopoverItem` | 下拉面板与菜单行 | 自绘外观，替代内置 `Menu` |
| `Modal` | 模态框 | `Open *bool`、`Footer` 插槽、180ms 过渡 |
| `Drawer` | 右侧抽屉 | `Open *bool`、`Footer` 插槽、220ms 过渡 |
| `CollapsibleSection` | 可折叠分组 | `Open *bool` 由调用方持有 |
| `Icon` | 图标 | 只接受 `IconName` 与尺寸 |

### 5.6 弹层与菜单

- 下拉菜单、选择器一律用 `Popover` + `PopoverItem`，**禁止** `ui.Menu` / `PopoverMenuButton`：内置菜单的尺寸跟随 `Theme.Spacing`，间距调整后会整体放大。
- 对话框用 `Modal`，抽屉用 `Drawer`，不要直接调用 `ui.Modal`。
- 所有 `Open *bool` 必须由页面或壳层持久持有；菜单项点击时先置 `false` 再执行动作，并置局部 `handled` 标记，避免同时触发所在行的点击/选中。

### 5.7 动画与布局

- `AnimateTo` 的进度必须挂在**每帧都存在**的元素上：关闭状态下仍要构建到 `progress <= 0.001` 再 return，否则通道在首次打开时才创建，没有过渡。
- 流内元素（侧栏、分栏）不要用残留占位节点来保动画：把通道放到常驻父元素，用进度控制宽高并 `Clip()`，收起后不构建子元素，避免父容器 gap 残留。
- 浮层（`Modal` / `Drawer`）可用 `ui.Overlay` + `Absolute()`，不参与布局流，保留零尺寸锚点可以接受。
- 手写进度插值时，相邻间距也要同步插值（如 `Gap(t.Space(3) * progress)`），否则元素消失那一帧会出现间距跳变。
- `Transition` 的 `Enter` 只在「上一帧已存在的父元素」内生效；`Exit` 副本不接收指针与焦点，需要交互的关闭逻辑必须自己实现。当前浮层动画统一用 `AnimateTo`。
- 列表与网格：N 个条目用 `ui.GridView`（带虚拟化）；页面骨架这类固定分栏用 `ui.Grid` + `ColumnTracks` / `RowTracks`，不要用 `Grid` 渲染动态条目集合。

### 5.8 组件边界

- 页面状态（折叠开关、滚动位置、草稿、菜单开关）留在 `Page` 结构体；组件只接收值、稳定指针与回调。
- 只被单个页面使用、样式很特殊的布局（如助手页右侧会话栏），**宁可写在页面里**，不要为了"看起来像组件"而强行抽象。
- 组件不读取全局变量；需要主题时从 `c.Theme()` 取。

## 6. 质量校验

```bash
gofmt -w <改动文件>   # 提交前必须格式化
go vet ./...          # 静态检查
go build ./...        # 编译
go test ./...         # 单元测试
go tool mygo dev      # 开发模式运行（人工验收用）
go tool mygo build    # 构建
```

- 提交前至少跑 `gofmt` 与 `go vet`
- 窗口交互、渲染效果不做自动化验收，由人工运行确认

## 7. 验收与沟通

- 完成任务后说明改了什么、涉及哪些文件、如何验证，不夸大结果
- 无法验证的部分明确告知，不假设通过
- 遇到阻塞先说明原因并给出可选方案，不要反复重试同一条失败路径
