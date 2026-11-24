# Bowknot Workbench

本地优先的 AI 助理与多 Agent 协作工作台，基于 [mygo](https://github.com/egoist/mygo) 构建的原生 Go 桌面应用。

所有会话、任务、记忆与产物都保存在本机，核心界面是立即模式原生 UI（无 WebView），只有安装的第三方应用在独立沙箱 WebView 中运行。

## 文档

- [产品总览与完成度](docs/product-overview.md)：产品最终形态、各功能模块简介与是否完成、待确定项、运行与数据说明。
- [开发约定](AGENTS.md)：目录结构、编码规范、UI 规范与质量校验要求。

## 当前状态

主助手对话、员工 Agents、群聊协作、任务系统、工具调用与审批、长期记忆、文件库与产物、应用与插件、模型服务、设置中心、桌宠基础能力均已完成。

工作台、AIGC 创作中心、桌宠提醒、多模态输入、消息搜索与导出、数据运维入口、跨平台与自动更新仍处于占位或未开始阶段，详见产品总览。

## 技术栈

- Go 1.27+
- [mygo](https://github.com/egoist/mygo) v0.9.0 原生 UI（立即模式，无 WebView）
- SQLite（`modernc.org/sqlite`，纯 Go 驱动）
- UI 与后端分离：UI 只依赖 `internal/contract`，业务编排在 `internal/app`，持久化统一走 `internal/store`

## 目录结构

```text
cmd/desktop/main.go          启动入口：自检、装配 store/网关/安装器并注入壳层
internal/contract/           接口与 DTO，一个模块一个文件
internal/app/                用例编排：助手、员工、群聊、任务、记忆、产物、应用、插件、设置
internal/domain/             领域模型，store 与 app 共用
internal/store/              SQLite 持久化：门面 + 子 store + 迁移 + 种子
internal/infra/
├── config/                  config.json 加载校验与密钥脱敏
├── llm/                     OpenAI 兼容 SSE + 多 Provider 路由 + Fake
├── package/                 包管理：manifest、安全解包、下载、安装卸载
└── tool/                    工具注册表与执行外壳：授权、审批、审计
internal/builtin/            embed 的内置应用/插件 tar
internal/platform/           paths / lifecycle / id / logging
internal/pet/                桌宠素材规格与图集
internal/tts/                语音合成 Provider 与串行播放
internal/ui/
├── shell/                   窗口外壳：自绘标题栏、一级导航、应用启动器
├── pages/                   原生页面（apps / assistant / agents / tasks / files / plugins / settings / aigc）
├── components/              公共组件（Button、Card、MessageView、Timeline、Modal、Icon 等）
├── theme/                   品牌主题与明暗色 token
└── format/                  时间、相对时间、文件大小格式化
docs/                        产品总览
```

## 快速开始

环境要求：Go 1.27+；Windows 上运行第三方应用需要 WebView2 运行时。

```bash
go tool mygo dev      # 开发模式运行
go tool mygo build    # 构建，产物输出到 build/
```

首次启动会自动创建数据目录 `~/.bowknot-workbench/`（数据库、配置、日志、工作区与安装包），并安装内置示例应用/插件。

## 质量校验

```bash
gofmt -l .            # 格式检查
go vet ./...          # 静态检查
go build ./...        # 编译
go test ./...         # 单元测试
```

窗口交互与视觉效果由人工运行确认，详见 [AGENTS.md](AGENTS.md)。
