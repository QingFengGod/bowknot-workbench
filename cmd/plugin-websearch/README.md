# 网页搜索插件

随包内置的 native 插件，以 stdio JSON-RPC（MCP 子集）暴露 `searchWeb`。

默认使用免密钥方案：请求 Bing 中文搜索页并解析结果，失败时回退 DuckDuckGo。
无需任何配置即可使用；结果返回标题、链接与摘要。

## 可选：切换到 API 搜索

在插件数据目录（`<数据根>/plugins/web-search/data/`）新建 `config.json`：

```json
{
  "provider": "bocha",
  "apiKey": "你的博查 API Key"
}
```

支持的 `provider`：

- `auto`（默认）：Bing → DuckDuckGo
- `bing`：只解析 Bing 中文结果
- `duckduckgo`：只解析 DuckDuckGo
- `bocha`：博查 Web Search API，需要 `apiKey`
- `tavily`：Tavily Search API，需要 `apiKey`

修改后下次调用即生效，不需要重装插件。

## 工具

- `searchWeb(query, count?, site?)`：返回最多 10 条结果，默认 5 条

安装后需要在插件页把插件授权给对应 Agent，工具才会出现在模型可调用列表中。
