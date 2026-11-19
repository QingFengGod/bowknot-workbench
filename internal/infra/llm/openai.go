package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// sseReadBuffer 覆盖较长的单行 data 载荷，避免 Scanner 截断。
	sseReadBuffer = 64 * 1024
	// errBodyLimit 限制错误响应体的读取长度，防止上游返回超大 body。
	errBodyLimit = 4 * 1024
	// errMessageLimit 限制进入错误信息的上游文本长度。
	errMessageLimit = 500
	// completeBodyLimit 限制非流式响应的读取长度，避免异常上游撑爆内存。
	completeBodyLimit = 2 << 20
)

// OpenAI 是 OpenAI 兼容 /chat/completions 流式协议的实现。
// Stream 在返回前完成建连并读到首个事件，因此返回 error 时必然尚未产生任何输出。
type OpenAI struct {
	baseURL      string
	apiKey       string
	defaultModel string
	client       *http.Client
}

// newOpenAI 构造 OpenAI 兼容实现；httpClient 为 nil 时使用带超时保护的默认客户端。
func newOpenAI(opts Options) *OpenAI {
	client := opts.HTTPClient
	if client == nil {
		client = defaultHTTPClient()
	}
	return &OpenAI{
		baseURL:      strings.TrimRight(opts.BaseURL, "/"),
		apiKey:       opts.APIKey,
		defaultModel: opts.DefaultModel,
		client:       client,
	}
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			MaxIdleConns:          4,
			IdleConnTimeout:       90 * time.Second,
		},
	}
}

func (p *OpenAI) Stream(ctx context.Context, req ChatRequest) (<-chan Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, classifyTransport(err)
	}
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	if model == "" {
		return nil, &Error{Kind: KindServer, Message: "未配置模型 ID"}
	}

	body, reader, firstData, err := p.start(ctx, req, model)
	if err != nil {
		return nil, err
	}

	out := make(chan Chunk, 16)
	go p.pump(ctx, body, reader, firstData, out)
	return out, nil
}

// Complete 走同一下游 /chat/completions 接口，但要求一次性返回完整文本。
func (p *OpenAI) Complete(ctx context.Context, req ChatRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", classifyTransport(err)
	}
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	if model == "" {
		return "", &Error{Kind: KindServer, Message: "未配置模型 ID"}
	}

	data, err := json.Marshal(buildPayload(req, model, false))
	if err != nil {
		return "", &Error{Kind: KindServer, Message: "构造请求失败"}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", &Error{Kind: KindServer, Message: "构造请求失败"}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", classifyTransport(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, completeBodyLimit))
	if err != nil {
		return "", classifyTransport(err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", classifyStatus(resp.StatusCode, extractErrorMessage(string(raw)))
	}

	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", &Error{Kind: KindServer, Message: "响应数据解析失败"}
	}
	if decoded.Error != nil && decoded.Error.Message != "" {
		return "", &Error{Kind: KindServer, Message: decoded.Error.Message}
	}
	if len(decoded.Choices) == 0 {
		return "", &Error{Kind: KindServer, Message: "模型没有返回内容"}
	}
	return decoded.Choices[0].Message.Content, nil
}

// buildPayload 把请求转换为 OpenAI 兼容载荷，Stream 与 Complete 共用。
func buildPayload(req ChatRequest, model string, stream bool) chatPayload {
	payload := chatPayload{Model: model, Stream: stream}
	if req.System != "" {
		payload.Messages = append(payload.Messages, payloadMessage{Role: "system", Content: req.System})
	}
	for _, item := range req.Messages {
		// 只有完全空白的占位消息才跳过；带工具调用或工具回填的消息必须保留。
		if item.Content == "" && len(item.ToolCalls) == 0 && item.ToolCallID == "" {
			continue
		}
		message := payloadMessage{
			Role:       item.Role,
			Content:    item.Content,
			ToolCallID: item.ToolCallID,
		}
		for _, call := range item.ToolCalls {
			toolCall := payloadToolCall{ID: call.ID, Type: "function"}
			toolCall.Function.Name = call.Name
			toolCall.Function.Arguments = call.Arguments
			message.ToolCalls = append(message.ToolCalls, toolCall)
		}
		payload.Messages = append(payload.Messages, message)
	}
	for _, schema := range req.Tools {
		parameters := json.RawMessage(defaultToolParameters)
		if schema.Parameters != "" {
			parameters = json.RawMessage(schema.Parameters)
		}
		payload.Tools = append(payload.Tools, payloadTool{
			Type: "function",
			Function: payloadToolFunction{
				Name:        schema.Name,
				Description: schema.Description,
				Parameters:  parameters,
			},
		})
	}
	return payload
}

// start 发起请求并同步读取首个 SSE data 载荷，保证「返回 error 时必然尚未产生任何输出」。
// 返回的 reader 与读取首个载荷使用同一个缓冲，避免缓冲数据丢失。
func (p *OpenAI) start(ctx context.Context, req ChatRequest, model string) (io.ReadCloser, *bufio.Reader, string, error) {
	payload := buildPayload(req, model, true)
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, "", &Error{Kind: KindServer, Message: "构造请求失败"}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, nil, "", &Error{Kind: KindServer, Message: "构造请求失败"}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, nil, "", classifyTransport(err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
		return nil, nil, "", classifyStatus(resp.StatusCode, extractErrorMessage(string(raw)))
	}

	reader := bufio.NewReaderSize(resp.Body, sseReadBuffer)
	firstData, err := nextSSEData(reader)
	if err != nil {
		resp.Body.Close()
		if IsCanceled(classifyTransport(err)) || ctx.Err() != nil {
			return nil, nil, "", &Error{Kind: KindCanceled, Message: "请求已取消"}
		}
		return nil, nil, "", classifyTransport(err)
	}
	return resp.Body, reader, firstData, nil
}

// pump 从 SSE 流持续解码增量并写入 out；ctx 取消时立即停止并关闭底层连接。
func (p *OpenAI) pump(ctx context.Context, body io.ReadCloser, reader *bufio.Reader, firstData string, out chan<- Chunk) {
	defer close(out)
	defer body.Close()

	pending := firstData
	readData := func() (string, error) {
		if pending != "" {
			data := pending
			pending = ""
			return data, nil
		}
		return nextSSEData(reader)
	}

	var seq int64
	emit := func(chunk Chunk) bool {
		seq++
		chunk.Seq = seq
		select {
		case out <- chunk:
			return true
		case <-ctx.Done():
			// 取消时尽力投递一条 canceled 错误，让调用方拿到明确语义。
			chunk.Err = &Error{Kind: KindCanceled, Message: "请求已取消"}
			select {
			case out <- chunk:
			default:
			}
			return false
		}
	}

	for {
		if ctx.Err() != nil {
			emit(Chunk{Err: &Error{Kind: KindCanceled, Message: "请求已取消"}})
			return
		}
		data, err := readData()
		if err != nil {
			classified := classifyTransport(err)
			if ctx.Err() != nil {
				classified = &Error{Kind: KindCanceled, Message: "请求已取消"}
			} else if errors.Is(err, io.EOF) {
				// 上游没有发送 [DONE] 就断开，按连接中断处理。
				classified = &Error{Kind: KindNetwork, Message: "模型连接中断"}
			}
			emit(Chunk{Err: classified})
			return
		}
		if data == "[DONE]" {
			emit(Chunk{Done: true})
			return
		}

		payload, perr := parseSSEData(data)
		if perr != nil {
			emit(Chunk{Err: &Error{Kind: KindServer, Message: "响应数据解析失败"}})
			return
		}
		if payload.errMessage != "" {
			emit(Chunk{Err: &Error{Kind: KindServer, Message: payload.errMessage}})
			return
		}
		if !emit(Chunk{Delta: payload.delta, Reasoning: payload.reasoning, ToolCalls: payload.toolCalls, Usage: payload.Usage}) {
			return
		}
	}
}

func (p *OpenAI) Models(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, &Error{Kind: KindServer, Message: "构造请求失败"}
	}
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, classifyTransport(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
	if err != nil {
		return nil, classifyTransport(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, classifyStatus(resp.StatusCode, extractErrorMessage(string(raw)))
	}

	var decoded struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, &Error{Kind: KindServer, Message: "模型列表解析失败"}
	}
	out := make([]ModelInfo, 0, len(decoded.Data))
	for _, item := range decoded.Data {
		if item.ID == "" {
			continue
		}
		out = append(out, ModelInfo{ID: item.ID, Name: item.ID})
	}
	return out, nil
}

// defaultToolParameters 是 manifest 没有声明参数结构时使用的自由对象。
const defaultToolParameters = `{"type":"object","properties":{}}`

type chatPayload struct {
	Model    string           `json:"model"`
	Messages []payloadMessage `json:"messages"`
	Stream   bool             `json:"stream"`
	Tools    []payloadTool    `json:"tools,omitempty"`
}

type payloadMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCalls  []payloadToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

type payloadTool struct {
	Type     string              `json:"type"`
	Function payloadToolFunction `json:"function"`
}

type payloadToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type payloadToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// sseData 是 /chat/completions 流式响应里单个 data 载荷。
type sseData struct {
	Choices []struct {
		Delta struct {
			Content      string `json:"content"`
			Reasoning    string `json:"reasoning_content"`
			ReasoningAlt string `json:"reasoning"`
			ToolCalls    []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`

	delta      string
	reasoning  string
	toolCalls  []ToolCall
	errMessage string
}

func parseSSEData(data string) (*sseData, error) {
	var raw sseData
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return nil, err
	}
	if len(raw.Choices) > 0 {
		raw.delta = raw.Choices[0].Delta.Content
		raw.reasoning = raw.Choices[0].Delta.Reasoning
		if raw.reasoning == "" {
			// 少数兼容实现把思维链放在 reasoning 字段。
			raw.reasoning = raw.Choices[0].Delta.ReasoningAlt
		}
		for _, call := range raw.Choices[0].Delta.ToolCalls {
			raw.toolCalls = append(raw.toolCalls, ToolCall{
				Index:     call.Index,
				ID:        call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			})
		}
	}
	if raw.Error != nil {
		raw.errMessage = raw.Error.Message
	}
	return &raw, nil
}

// nextSSEData 逐行扫描直到拿到下一条 data 载荷；注释行、event 等字段直接跳过。
func nextSSEData(reader *bufio.Reader) (string, error) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "data:")), nil
	}
}

// extractErrorMessage 尽量提取上游 error.message，原始 body 兜底并截断。
func extractErrorMessage(body string) string {
	var wrapped struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &wrapped); err == nil && wrapped.Error.Message != "" {
		body = wrapped.Error.Message
	}
	body = strings.TrimSpace(body)
	if utf8.RuneCountInString(body) > errMessageLimit {
		runes := []rune(body)
		body = string(runes[:errMessageLimit]) + "…"
	}
	if body == "" {
		return "上游无错误详情"
	}
	return body
}
