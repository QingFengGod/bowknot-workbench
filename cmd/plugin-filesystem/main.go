// 内置文件系统插件：以 stdio JSON-RPC 暴露 listFiles / readFile / writeFile。
// 工具清单由 manifest 声明，本程序只负责执行；路径限制在用户主目录与插件数据目录内。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxReadBytes  = 512 * 1024
	maxWriteBytes = 1024 * 1024
	maxListItems  = 500
)

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
			"serverInfo":      map[string]any{"name": "bowknot-filesystem", "version": "1.0.2"},
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
	return []map[string]any{
		toolDecl("listFiles", "列出指定目录下的文件与子目录"),
		toolDecl("readFile", "读取文本文件内容"),
		toolDecl("writeFile", "写入或覆盖文本文件"),
	}
}

func toolDecl(name, description string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func callTool(raw json.RawMessage) (callResult, error) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return callResult{}, fmt.Errorf("参数解析失败：%w", err)
	}
	args := params.Arguments
	switch params.Name {
	case "listFiles":
		text, err := listFiles(stringArg(args, "path"))
		if err != nil {
			return callResult{}, err
		}
		return callResult{Content: []textContent{{Type: "text", Text: text}}}, nil
	case "readFile":
		text, err := readFile(stringArg(args, "path"))
		if err != nil {
			return callResult{}, err
		}
		return callResult{Content: []textContent{{Type: "text", Text: text}}}, nil
	case "writeFile":
		text, err := writeFile(stringArg(args, "path"), stringArg(args, "content"))
		if err != nil {
			return callResult{}, err
		}
		return callResult{Content: []textContent{{Type: "text", Text: text}}}, nil
	default:
		return callResult{}, fmt.Errorf("未知工具：%s", params.Name)
	}
}

func listFiles(path string) (string, error) {
	dir, err := resolvePath(path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("读取目录失败：%w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	lines := make([]string, 0, len(entries)+1)
	lines = append(lines, "目录："+dir)
	for index, entry := range entries {
		if index >= maxListItems {
			lines = append(lines, fmt.Sprintf("…其余 %d 项已省略", len(entries)-maxListItems))
			break
		}
		if entry.IsDir() {
			lines = append(lines, "[目录] "+entry.Name())
			continue
		}
		info, err := entry.Info()
		if err != nil {
			lines = append(lines, "[文件] "+entry.Name())
			continue
		}
		lines = append(lines, fmt.Sprintf("[文件] %s（%d 字节）", entry.Name(), info.Size()))
	}
	return strings.Join(lines, "\n"), nil
}

func readFile(path string) (string, error) {
	target, err := resolvePath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("读取文件失败：%w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s 是目录，请使用 listFiles", target)
	}
	if info.Size() > maxReadBytes {
		return "", fmt.Errorf("文件超过 %d 字节上限，请分段读取", maxReadBytes)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("读取文件失败：%w", err)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("文件不是 UTF-8 文本，暂不支持读取二进制内容")
	}
	return string(data), nil
}

func writeFile(path, content string) (string, error) {
	if len(content) > maxWriteBytes {
		return "", fmt.Errorf("内容超过 %d 字节上限", maxWriteBytes)
	}
	target, err := resolvePath(path)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return "", fmt.Errorf("%s 是目录，无法写入", target)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("写入文件失败：%w", err)
	}
	return fmt.Sprintf("已写入 %s（%d 字节）", target, len(content)), nil
}

// resolvePath 展开 ~ 与相对路径，并限制在用户主目录和插件数据目录内。
func resolvePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("路径不能为空")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法确定用户主目录：%w", err)
	}
	switch {
	case raw == "~":
		raw = home
	case strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, `~\`):
		raw = filepath.Join(home, raw[2:])
	case filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`):
		// Windows 下 "/x" 不算绝对路径，这里显式交给 Abs 解析，
		// 再由授权范围校验拒绝，避免被误当成插件目录内的相对路径。
	default:
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("无法确定工作目录：%w", err)
		}
		raw = filepath.Join(wd, raw)
	}

	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("解析路径失败：%w", err)
	}
	abs = filepath.Clean(abs)

	roots := []string{home}
	if data := os.Getenv("BOWKNOT_PLUGIN_DATA"); data != "" {
		roots = append(roots, data)
	}
	for _, root := range roots {
		if within(root, abs) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("路径 %s 超出插件授权范围（仅允许用户主目录与插件数据目录）", abs)
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func logf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "["+"filesystem"+"] "+format+"\n", values...)
}
