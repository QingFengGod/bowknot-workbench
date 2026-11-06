# AGENTS.md

本文件是给 AI 编码代理的项目约定。开始任务前请先阅读，改动时遵循以下规则。

## 1. 项目概览

bowknot-workbench 是一个本地优先的 AI 助理与多 Agent 协作工作台，当前处于**前端 UI 先行**阶段。

- 仓库：Bun workspaces，业务代码在 `web` 工作区
- 技术栈：Vue 3.5（`<script setup lang="ts">`）、TypeScript、Vite 8、UnoCSS、antdv-next、Pinia、vue-router
- 包管理：Bun（不要使用 npm / yarn / pnpm 生成新锁文件）
- 产品文档：`docs/product-requirements/`，实现前先对齐 PRD；PRD 与实现冲突时先反馈

## 2. 必须遵守的协作规则

1. **复用优先，禁止堆砌一次性代码**
   - 可复用的逻辑抽成组合函数，放 `web/src/composables/`
   - 可复用的 UI 抽成组件：基础组件放 `web/src/components/global/`，业务共享组件放 `web/src/components/shared/`
   - 通用函数抽成工具函数，放 `web/src/utils/`（目录不存在时新建）
   - 新增前先搜索现有实现；同一段逻辑出现 3 次以上必须抽离
2. **验收方式**
   - 除非用户明确要求，**不得主动调用浏览器控制插件或电脑控制插件**
   - 默认通过静态检查、类型检查、构建、日志与代码审查完成自验
   - 需要真实交互/截图验收时，先向用户说明并征得同意
3. **样式优先 UnoCSS**
   - 优先使用 UnoCSS class，不要在 `<style>` 里重复写已有工具类
   - 组件内编写 scoped CSS 时优先使用 `@apply` 组合主题工具类，例如 `@apply bk-bg-container bk-shadow b-l-1;`，不要手写 `background` / `box-shadow` / `border` 等主题声明
   - 多个 class 的组合如果重复出现或语义明确，抽成 `web/uno.config.ts` 的 `shortcuts`
   - 只在需要复杂选择器、伪元素、动画、渐变或第三方组件覆盖时写原生 scoped CSS

## 3. 目录约定

```text
web/src/
├── assets/          # 静态资源
├── components/
│   ├── global/      # 全局基础组件，已在 main.ts 统一注册
│   └── shared/      # 业务共享组件
├── composables/     # 组合函数
├── constants/       # 常量（PREFIX = 'bk'）
├── directives/      # 全局指令
├── layout/          # 全局布局
├── router/          # 路由与守卫
├── store/           # Pinia
├── styles/          # reset / icons / transition / animate
├── types/           # 全局类型声明
├── utils/           # 工具函数（按需新建）
└── views/           # 页面，一页一目录，入口 index.vue
```

- 页面组件：`defineOptions({ name: 'XxxPage' })`
- 业务组件：目录与组件同名（如 `Conversations/Conversations.vue`），公共类型放同目录 `type.ts`
- 路径别名：`@` → `web/src`

## 4. 编码规范

- Vue 组件统一使用 `<script setup lang="ts">` + 类型式 `defineProps` / `defineEmits`
- 禁止 `any`；公共类型放组件 `type.ts` 或 `web/src/types/`
- 路由页面使用懒加载，命名 `XxxPage`，路由表维护在 `web/src/router/routes.ts`
- 全局基础组件需在 `web/src/components/global/index.ts` 注册，并在 `web/src/types/vue.d.ts` 的 `GlobalComponents` 中补充类型
- 图标：从 Iconify 取 SVG，写成 `web/src/styles/icons.css` 中的 `.i-*` mask class，同时更新 `web/src/components/global/Icon/type.ts` 的 `IconName`；页面统一用 `<BkIcon name="..." />`
- 注释、UI 文案使用中文；注释只解释“为什么”，不复述代码
- 改动保持最小范围，不顺手重构、不修改与任务无关的文件

## 5. 样式与设计系统

- 基于 antdv-next + `@antdv-next/unocss`，前缀为 `bk`，主题色 `#fb5215`
- 颜色、间距、圆角优先使用 token：`bk-bg-container`、`bk-text-secondary`、`bk-p-sm`、`bk-rd-lg` 等
- scoped CSS 优先用 `@apply` 组合主题工具类，例如 `@apply bk-bg-container bk-shadow b-l-1;`
- `@apply` 无法表达时（复杂选择器、渐变、伪元素等）再用 `var(--bk-color-primary)`、`var(--bk-color-bg-container)` 等 CSS 变量，不要写死颜色
- 边框使用项目自定义规则：`b-all-1`、`b-b-1`、`b-r-1`、`b-l-1`，颜色默认取 `split`
- 现有 shortcuts：`flex-center`、`box-container`、`popover-item`；新增 shortcuts 保持命名语义化
- 必须兼容明暗主题；为动画补充 `prefers-reduced-motion` 降级
- 组件超过约 300 行时考虑拆分；单个函数只做一件事

## 6. 质量校验

按需执行，提交前至少跑 lint：

```bash
bun install            # 仓库根目录
bun run dev:web        # 启动前端
cd web
bun run lint           # oxlint
bun run fmt            # oxfmt 格式化
bun run build          # vue-tsc 类型检查 + vite 构建
```

- `web/src/views/error/notFound/index.vue` 存在历史遗留的 `rotate` 类型错误，未修复前 `build` 可能失败；这是既有问题，不要把它算作本次改动，也不要顺手改无关代码
- 不擅自启动常驻服务；需要时后台运行，任务结束前停止
- 不自动执行 `git commit` / `git push`，除非用户明确要求

## 7. 验收与沟通

- 完成任务后说明改了什么、涉及哪些文件、如何验证，不夸大结果
- 无法验证的部分明确告知，不假设通过
- 遇到阻塞先说明原因并给出可选方案，不要反复重试同一条失败路径
