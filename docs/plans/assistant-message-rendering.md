# 助手消息渲染实施计划

| 项目 | 内容 |
| --- | --- |
| 文档状态 | 待评审 |
| 创建日期 | 2026-11-10 |
| 所属模块 | 助手 `/assistant` |
| 上游文档 | [01-assistant.md](../product-requirements/01-assistant.md) |
| 当前阶段 | 前端 UI 先行 |

## 1. 目标与边界

### 1.1 目标

把助手页消息区实现为 **`parts` 数据模型 + 块级增量 Markdown + 非文本块组件注册表**，流式输出过程稳定在 60fps。

### 1.2 边界

- 只做前端，流式数据由 mock 驱动器产生，不接后端
- 不引入真正的 schema 协议、JSON mode
- 不做虚拟滚动、高亮 worker、图片/文件消息

### 1.3 前置清理

[index.vue](../../web/src/views/assistant/index.vue) 中 `onCreate` 里未使用的 `date` 变量需要删除，否则 `bun run build` 会因 `vue-tsc -b` 报 TS6133 失败。

## 2. 文件落点

按 AGENTS.md 的分层约定：

```text
web/src/
├── types/chat.ts                              新增 · 消息与流式事件类型
├── utils/markdown.ts                          新增 · 切块 / 渲染 / sanitize（纯函数）
├── utils/perf.ts                              新增 · DEV 环境计时
├── composables/
│   ├── useAutoScroll.ts                       新增 · 吸底与「回到底部」
│   └── useMessageStream.ts                    新增 · 流式驱动（mock / 未来接真实）
├── components/shared/
│   ├── Markdown/MarkdownBlock.vue             新增 · 单块渲染
│   └── Message/
│       ├── MessageList.vue                    新增 · 列表 + 滚动
│       ├── MessageItem.vue                    新增 · 单条消息外壳
│       ├── MessageParts.vue                   新增 · part 分发
│       ├── MessageActions.vue                 新增 · 复制/重答/删除/播报
│       ├── CodeBlock.vue                      新增 · 代码块 + 复制
│       └── partRegistry.ts                    新增 · 非文本块注册表
└── views/assistant/
    ├── mock.ts                                新增 · mock 会话与长文本
    ├── type.ts                                扩展 · 保留会话类型
    └── index.vue                              改造 · 两行两列 + 接入消息区
```

消息渲染放在 `components/shared/` 而非 `views/assistant/`，因为群聊与子 Agent 会复用同一套渲染。

## 3. 数据契约

### 3.1 消息实体

```ts
type MessageStatus = 'sending' | 'streaming' | 'done' | 'failed' | 'stopped'

type TextPart = { type: 'text'; text: string }
type CodePart = { type: 'code'; lang?: string; code: string; done?: boolean }
type ToolPart = {
  type: 'tool'
  id: string
  name: string
  status: 'running' | 'done' | 'error'
  args?: unknown
  result?: unknown
}
type EmotionPart = { type: 'emotion'; emotion: string; action?: string }
type CardPart = { type: 'card'; component: string; props: Record<string, unknown> }

type MessagePart = TextPart | CodePart | ToolPart | EmotionPart | CardPart

type ChatMessage = {
  id: string
  conversationId: string
  role: 'user' | 'assistant'
  parts: MessagePart[]
  status: MessageStatus
  createdAt: number
  error?: string
  memoryWritten?: boolean
}
```

### 3.2 流式事件

按 **part 级全量替换**：

```ts
type StreamEvent =
  | { type: 'start'; message: ChatMessage }
  | { type: 'update'; index: number; part: MessagePart }   // 整块覆盖，不做 delta 拼接
  | { type: 'end'; status: 'done' | 'stopped' }
  | { type: 'error'; message: string }
```

选择全量替换的理由：不需要前端拼接字符串、不需要顺序保护，与「已完成块缓存」天然契合。

## 4. 分阶段计划

### P0 · 数据模型与 mock

- 产出：`types/chat.ts`、`views/assistant/mock.ts`
- 内容：
  - 上述类型定义
  - 3 组会话 mock：短对话、3000 字长回答、错误与停止场景
- 验收：
  - [ ] `vue-tsc -b` 通过
  - [ ] 无 `any`，公共类型集中在 `types/`

### P1 · MarkdownBlock（核心）

- 产出：`utils/markdown.ts`、`components/shared/Markdown/MarkdownBlock.vue`
- 关键实现：
  1. `splitBlocks(text)` 按空行、围栏代码、表格切块，返回 `{ id, content, done }`
  2. 已完成块渲染一次后缓存为 HTML 字符串，只有 `done === false` 的末块重算
  3. 未闭合语法降级：围栏数为奇数时补齐；表格在流式中降级为文本
  4. 渲染节流 50–80ms，用 rAF 对齐
  5. 所有 HTML 出口统一 `DOMPurify.sanitize`，`markdown-it` 配置 `html: false`
- 验收：
  - [ ] 3000 字 mock 流下单帧渲染 < 2ms（`utils/perf.ts` 在 DEV 打印）
  - [ ] 长文中途插入、回退不闪烁、不丢内容

### P2 · 消息容器

- 产出：`MessageList.vue`、`MessageItem.vue`、`MessageParts.vue`、`MessageActions.vue`、`CodeBlock.vue`、`useAutoScroll.ts`
- 关键实现：
  - 吸底：生成中自动跟随；用户上滚则暂停，并显示「回到底部」
  - 操作栏 hover 显示：复制 / 重答 / 删除 / 播报（重答仅最后一条）
  - 状态：`streaming` 显示停止按钮，`failed` 显示错误与重试，`stopped` 保留已生成内容
  - 改造 `index.vue` 为两行两列 grid（顶部栏跨两列，消息区与会话区并排）
- 验收：
  - [ ] 切换会话、上滚、停止、失败重试均符合 PRD 5.2
  - [ ] 流式过程中不出现滚动抖动

### P3 · 流式驱动器（可与 P1 并行）

- 产出：`composables/useMessageStream.ts`
- 关键实现：定速吐字，按块边界触发 `update`；暴露 `start / stop / regenerate`
- 验收：
  - [ ] mock 流可完整跑通 `start → streaming → done`
  - [ ] 停止与错误分支可复现

### P4 · 非文本块（接口留好，实现从简）

- 产出：`partRegistry.ts`，`emotion` 写入消息元数据，`tool` 占位卡片
- 关键实现：
  - 高亮选型在本阶段前确定（shiki 按需语言 vs highlight.js），P1/P2 的代码块先用等宽 `<pre>` + 复制
  - 新增一种 part 只需在注册表登记，不改 `MessageParts.vue`
- 验收：
  - [ ] 注册表新增 part 类型无需改动分发组件
  - [ ] 未注册类型有兜底占位，不报错

## 5. 依赖变更

| 包 | 用途 | 体积（gzip 量级） |
| --- | --- | --- |
| `markdown-it` + `@types/markdown-it` | Markdown 解析 | ~15KB |
| `dompurify` | XSS 过滤 | ~7KB |

加在根 `package.json` 的 `catalog`，使用 `bun add`。高亮库在 P4 再评估，不提前安装。

## 6. 性能策略

| 手段 | 说明 |
| --- | --- |
| 块级缓存 | 已完成块只渲染一次，后续流式只重算末块 |
| 渲染节流 | rAF + 最小间隔 50–80ms |
| 代码高亮延后 | 流式期间纯 `<pre>`，`done` 后异步高亮 |
| 静态 vnode | 历史消息使用 `v-memo` 跳过重建 |
| 离屏跳过 | 长列表可加 `content-visibility: auto` |
| 基准固定 | 3000 字 + 3 个代码块，`performance.mark` 单帧计时 |

## 7. 风险

| 风险 | 处理 |
| --- | --- |
| `v-html` 引发 XSS | 所有 HTML 出口统一走 `sanitize`，禁止直出 |
| 流式语法抖动 | 未闭合围栏、表格降级处理，避免解析器抛错 |
| 滚动抖动 | 吸底仅在处于底部时跟随，用户上滚立即让出控制权 |
| 一次性做太多 | P4 只留注册表接口，不实现富卡片 |
| 现有 build 失败 | P0 前清理 `index.vue` 未使用变量 |

## 8. 明确不做

- 服务端 schema / JSON mode 协议
- 虚拟滚动（消息数 < 50 时无必要）
- 图片、文件、附件消息
- 富交互卡片（审批、图表）

## 9. 验收方式

- 静态校验：`bun run lint`、`bunx oxfmt --check`、`vue-tsc -b`、`vite build`
- 交互验收：由用户在本地 `bun run dev:web` 手动验证，不主动调用浏览器控制插件
