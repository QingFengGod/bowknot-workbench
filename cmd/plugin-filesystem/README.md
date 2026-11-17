# 文件系统插件

随包内置的 native 插件，用于验证「安装 → 授权 → 模型调用 → 真实执行」全链路。

运行方式：宿主按 manifest 的 `runtime.commands` 选择当前平台的可执行文件，
以 stdio JSON-RPC（MCP 子集）调用。访问范围限制在用户主目录与插件数据目录内。

工具：

- `listFiles`：列出目录内容
- `readFile`：读取 UTF-8 文本（上限 512KB）
- `writeFile`：写入文本（上限 1MB，危险工具，执行前需要用户确认）

安装后需要在插件页把插件授权给对应 Agent，工具才会出现在模型可调用列表中。
