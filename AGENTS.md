# AGENTS.md

本文件是给 AI 编码代理的项目约定。开始任务前请先阅读，改动时遵循以下规则。

## 1. 项目概览

bowknot-workbench 是一个本地优先的 AI 助理与多 Agent 协作工作台，为 **mygo 原生 Go 桌面端**。

- 原生 UI 优先：立即模式 UI，无 WebView，页面状态存放在页面结构体字段中
- 后端与 UI 分离：UI 只调用 `internal/contract` 接口，不感知数据来源；mock 实现位于 `internal/backend`，后续替换为 SQLite / 真实服务
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
cmd/desktop/main.go          启动入口，组装后端服务注入壳层
internal/contract/           接口与 DTO，一个模块一个文件（app / assistant / agent…）
internal/backend/            内存 mock 实现，模拟流式回复与演示数据
internal/ui/
├── shell/                   窗口外壳：自绘标题栏、一级导航、页面注册与切换
├── pages/                   原生页面，一页一个目录（apps / assistant / agents / plugins / aigc）
├── components/              公共组件与图标（Button、Card、Avatar、Drawer、Icon 等）
├── theme/                   品牌主题与明暗色 token
└── format/                  时间、相对时间、文件大小格式化
```

- 新页面实现后注册进 `shell.New` 的 `native` 表，路由与一级导航对齐
- 页面间不互相 import；共享逻辑下沉到 `components` 或 `contract`

## 4. 编码规范

- 注释、UI 文案使用中文；注释只解释「为什么」，不复述代码
- 颜色、圆角、间距优先使用 `c.Theme()` 的 token（`t.Accent`、`t.Surface` 等），不写死颜色值
- 图标统一走 `components.Icon` 与 `components.IconName`，新增图标使用 lucide 风格 SVG（24 viewBox、stroke 2）
- 错误信息返回给 UI 层展示时使用中文
- 并发：mock 服务的流式事件用带缓冲 channel + goroutine，锁内不做阻塞操作，回调前先释放锁
- 单个函数只做一件事；视图构建函数过长时拆成私有方法

## 5. 质量校验

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

## 6. 验收与沟通

- 完成任务后说明改了什么、涉及哪些文件、如何验证，不夸大结果
- 无法验证的部分明确告知，不假设通过
- 遇到阻塞先说明原因并给出可选方案，不要反复重试同一条失败路径
