# Bowknot Workbench

本地优先的 AI 助理与多 Agent 协作工作台，基于 [mygo](https://github.com/egoist/mygo) 构建的原生 Go 桌面应用。

## 产品定位

- **主 Agent（助理）**：用户的贴身助理，具备虚拟形象与人设，可对话、可执行任务
- **Agents**：按需创建不同人设的动态 Agent，可单独对话，也可拉群协作，群聊由主 Agent 负责任务派发与汇报
- **长期记忆**：每个 Agent（含主 Agent）都有人设与记忆系统
- **插件**：独立模块，支持从本地或远程安装，按 Agent 配置可用范围
- **Apps**：应用管理，支持安装、能力开关与数据清理
- **AIGC**：生成中心、素材、角色、剧本与提示词等创作能力

## 技术栈

- Go 1.27+
- [mygo](https://github.com/egoist/mygo) v0.9.0 原生 UI（立即模式，无 WebView）
- UI 与后端分离：`internal/contract` 定义接口与 DTO，`internal/backend` 提供内存 mock，后续替换为 SQLite / 真实服务

## 目录结构

```text
cmd/desktop/main.go          启动入口，注入各后端服务
internal/contract/           接口与 DTO（app / apps / plugin / assistant / agent）
internal/backend/            内存版 mock 实现
internal/ui/
├── shell/                   窗口外壳：自绘标题栏 + 一级导航 + 页面容器
├── pages/                   原生页面（apps / assistant / agents / plugins / aigc）
├── components/              公共组件（Button、Card、Avatar、Drawer、Icon 等）
├── theme/                   品牌主题（#fb5215）与明暗色 token
└── format/                  时间、文件大小格式化
```

## 开发

```bash
go tool mygo dev      # 开发模式运行
```

## 构建与检查

```bash
go tool mygo build    # 构建安装包，输出到 build/
gofmt -l .            # 格式检查
go vet ./...          # 静态检查
go test ./...         # 单元测试
```

## 约定

开发前请先阅读 [AGENTS.md](AGENTS.md)，遵循项目的协作规则与编码规范。
