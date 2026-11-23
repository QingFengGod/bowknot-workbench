// 内置网页搜索插件：以 stdio JSON-RPC 暴露 searchWeb。
// 默认解析 Bing 中文结果，失败时回退 DuckDuckGo；也可以在插件数据目录的
// config.json 里配置博查或 Tavily 的 apiKey 切换到 API 搜索。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxOutputBytes = 32 * 1024
	defaultCount   = 5
	maxCount       = 10
	userAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

type rpcRequest struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      int64     `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError"`
}

// config 是插件数据目录下可选的 config.json，用于切换到需要 Key 的搜索接口。
type config struct {
	Provider string `json:"provider"` // auto / bing / duckduckgo / bocha / tavily
	APIKey   string `json:"apiKey"`
}

type searchItem struct {
	Title   string
	URL     string
	Snippet string
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	writer := bufio.NewWriter(os.Stdout)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			logf("忽略无法解析的请求：%v", err)
			continue
		}
		// 通知（无 id）不需要响应。
		if req.ID == nil {
			continue
		}
		resp := handle(req)
		data, err := json.Marshal(resp)
		if err != nil {
			logf("编码响应失败：%v", err)
			continue
		}
		writer.Write(data)
		writer.WriteByte('\n')
		writer.Flush()
	}
}

func handle(req rpcRequest) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: *req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "bowknot-websearch", "version": "1.0.0"},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolDeclarations()}
	case "tools/call":
		result, err := callTool(req.Params)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = result
	default:
		resp.Error = &rpcError{Code: -32601, Message: "未实现的方法：" + req.Method}
	}
	return resp
}

func toolDeclarations() []map[string]any {
	return []map[string]any{{
		"name":        "searchWeb",
		"description": "搜索公开网页，返回标题、链接与摘要；中文查询优先使用 Bing 中文结果",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "搜索关键词"},
				"count": map[string]any{"type": "integer", "description": "返回条数，1-10，默认 5"},
				"site":  map[string]any{"type": "string", "description": "可选，限定站点域名，例如 example.com"},
			},
			"required": []string{"query"},
		},
	}}
}

func callTool(raw json.RawMessage) (callResult, error) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return callResult{}, fmt.Errorf("参数解析失败：%w", err)
	}
	if params.Name != "searchWeb" {
		return callResult{}, fmt.Errorf("未知工具：%s", params.Name)
	}
	text, err := searchWeb(params.Arguments, loadConfig())
	if err != nil {
		return callResult{}, err
	}
	if len(text) > maxOutputBytes {
		text = truncateUTF8(text, maxOutputBytes) + "\n…（结果超过上限，已截断）"
	}
	return callResult{Content: []textContent{{Type: "text", Text: text}}}, nil
}

// searchWeb 解析参数后按配置选择搜索源；auto 模式先 Bing 后 DuckDuckGo。
func searchWeb(args map[string]any, cfg config) (string, error) {
	query := strings.TrimSpace(stringArg(args, "query"))
	if query == "" {
		return "", fmt.Errorf("query 不能为空")
	}
	count := intArg(args, "count")
	if count <= 0 {
		count = defaultCount
	}
	if count > maxCount {
		count = maxCount
	}
	if site := strings.TrimSpace(stringArg(args, "site")); site != "" {
		query += " site:" + site
	}

	items, err := search(query, count, cfg)
	if err != nil {
		return "", err
	}
	return formatResults(query, items), nil
}

func search(query string, count int, cfg config) ([]searchItem, error) {
	providers := []string{cfg.Provider}
	if cfg.Provider == "" || cfg.Provider == "auto" {
		providers = []string{"bing", "duckduckgo"}
	}

	var lastErr error
	for index, provider := range providers {
		items, err := runProvider(provider, query, count, cfg)
		if err == nil && len(items) > 0 {
			return items, nil
		}
		if err == nil {
			err = fmt.Errorf("%s 没有返回结果", provider)
		}
		lastErr = err
		// 显式指定的搜索源失败时直接报错，避免用户以为 Key 已生效。
		if len(providers) == 1 || index == len(providers)-1 {
			break
		}
		logf("%s 搜索失败，尝试下一个搜索源：%v", provider, err)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的搜索源")
	}
	return nil, fmt.Errorf("搜索失败：%w", lastErr)
}

func runProvider(provider, query string, count int, cfg config) ([]searchItem, error) {
	switch provider {
	case "bing", "":
		return searchBing(context.Background(), query, count)
	case "duckduckgo":
		return searchDuckDuckGo(context.Background(), query, count)
	case "bocha":
		return searchBocha(context.Background(), query, count, cfg.APIKey)
	case "tavily":
		return searchTavily(context.Background(), query, count, cfg.APIKey)
	default:
		return nil, fmt.Errorf("未知的搜索源 %q（可选 auto/bing/duckduckgo/bocha/tavily）", provider)
	}
}

var (
	bingBlock   = regexp.MustCompile(`(?is)<li class="b_algo".*?</li>`)
	bingLink    = regexp.MustCompile(`(?is)<h2[^>]*>\s*<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	bingSnippet = regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)

	ddgLink    = regexp.MustCompile(`(?is)<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippet = regexp.MustCompile(`(?is)<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)

	tagPattern = regexp.MustCompile(`(?s)<[^>]*>`)
)

func searchBing(ctx context.Context, query string, count int) ([]searchItem, error) {
	endpoint := "https://cn.bing.com/search?q=" + url.QueryEscape(query) + "&setlang=zh-Hans&count=" + strconv.Itoa(count)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.6")
	body, err := fetch(req)
	if err != nil {
		return nil, err
	}

	blocks := bingBlock.FindAllString(body, -1)
	items := make([]searchItem, 0, len(blocks))
	for _, block := range blocks {
		link := bingLink.FindStringSubmatch(block)
		if len(link) < 3 {
			continue
		}
		item := searchItem{
			Title: cleanText(link[2]),
			URL:   html.UnescapeString(link[1]),
		}
		if snippet := bingSnippet.FindStringSubmatch(block); len(snippet) >= 2 {
			item.Snippet = cleanText(snippet[1])
		}
		if item.Title == "" || !strings.HasPrefix(item.URL, "http") {
			continue
		}
		items = append(items, item)
		if len(items) >= count {
			break
		}
	}
	return items, nil
}

func searchDuckDuckGo(ctx context.Context, query string, count int) ([]searchItem, error) {
	form := url.Values{"q": {query}, "kl": {"cn-zh"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://html.duckduckgo.com/html/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := fetch(req)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(body, `<div class="result__body">`)
	items := make([]searchItem, 0, len(parts))
	for _, part := range parts[1:] {
		link := ddgLink.FindStringSubmatch(part)
		if len(link) < 3 {
			continue
		}
		item := searchItem{
			Title: cleanText(link[2]),
			URL:   decodeDuckDuckGoURL(html.UnescapeString(link[1])),
		}
		if snippet := ddgSnippet.FindStringSubmatch(part); len(snippet) >= 2 {
			item.Snippet = cleanText(snippet[1])
		}
		if item.Title == "" || !strings.HasPrefix(item.URL, "http") {
			continue
		}
		items = append(items, item)
		if len(items) >= count {
			break
		}
	}
	return items, nil
}

func searchBocha(ctx context.Context, query string, count int, apiKey string) ([]searchItem, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("博查搜索需要在插件数据目录的 config.json 中配置 apiKey")
	}
	payload, _ := json.Marshal(map[string]any{
		"query":   query,
		"count":   count,
		"summary": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.bochaai.com/v1/web-search", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	body, err := fetch(req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			WebPages struct {
				Value []struct {
					Name    string `json:"name"`
					URL     string `json:"url"`
					Snippet string `json:"snippet"`
					Summary string `json:"summary"`
				} `json:"value"`
			} `json:"webPages"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("解析博查结果失败：%w", err)
	}
	items := make([]searchItem, 0, len(resp.Data.WebPages.Value))
	for _, value := range resp.Data.WebPages.Value {
		snippet := strings.TrimSpace(value.Summary)
		if snippet == "" {
			snippet = value.Snippet
		}
		items = append(items, searchItem{Title: value.Name, URL: value.URL, Snippet: snippet})
		if len(items) >= count {
			break
		}
	}
	return items, nil
}

func searchTavily(ctx context.Context, query string, count int, apiKey string) ([]searchItem, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("Tavily 搜索需要在插件数据目录的 config.json 中配置 apiKey")
	}
	payload, _ := json.Marshal(map[string]any{
		"api_key":      strings.TrimSpace(apiKey),
		"query":        query,
		"max_results":  count,
		"search_depth": "basic",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	body, err := fetch(req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("解析 Tavily 结果失败：%w", err)
	}
	items := make([]searchItem, 0, len(resp.Results))
	for _, result := range resp.Results {
		items = append(items, searchItem{Title: result.Title, URL: result.URL, Snippet: result.Content})
		if len(items) >= count {
			break
		}
	}
	return items, nil
}

func fetch(req *http.Request) (string, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求搜索服务失败：%w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("读取搜索结果失败：%w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("搜索服务返回状态 %d", resp.StatusCode)
	}
	return string(data), nil
}

func formatResults(query string, items []searchItem) string {
	if len(items) == 0 {
		return fmt.Sprintf("没有找到与「%s」相关的结果。", query)
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "搜索「%s」共 %d 条结果：\n", query, len(items))
	for index, item := range items {
		fmt.Fprintf(&builder, "%d. %s\n   %s\n", index+1, item.Title, item.URL)
		if snippet := truncateUTF8(item.Snippet, 300); snippet != "" {
			fmt.Fprintf(&builder, "   %s\n", snippet)
		}
	}
	return strings.TrimSpace(builder.String())
}

// cleanText 去掉搜索结果里的标签与多余空白，保留可读文本。
func cleanText(raw string) string {
	text := tagPattern.ReplaceAllString(raw, " ")
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(text), " ")
}

// decodeDuckDuckGoURL 还原 DuckDuckGo 的中转链接。
func decodeDuckDuckGoURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if !strings.Contains(raw, "duckduckgo.com/l/") {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if target := parsed.Query().Get("uddg"); target != "" {
		return target
	}
	return raw
}

// loadConfig 读取插件数据目录下的可选配置；文件缺失时使用免密钥默认值。
func loadConfig() config {
	cfg := config{Provider: "auto"}
	data := os.Getenv("BOWKNOT_PLUGIN_DATA")
	if data == "" {
		return cfg
	}
	raw, err := os.ReadFile(filepath.Join(data, "config.json"))
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		logf("config.json 解析失败，使用默认搜索源：%v", err)
		return config{Provider: "auto"}
	}
	cfg.Provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	if cfg.Provider == "" {
		cfg.Provider = "auto"
	}
	return cfg
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func intArg(args map[string]any, key string) int {
	switch value := args[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case string:
		parsed, _ := strconv.Atoi(value)
		return parsed
	default:
		return 0
	}
}

// truncateUTF8 按字节截断并回退到完整 UTF-8 边界。
func truncateUTF8(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	cut := text[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func logf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "[websearch] "+format+"\n", values...)
}
