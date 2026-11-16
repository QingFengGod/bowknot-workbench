package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "sk-secret-key-1234567890"

func sseServer(t *testing.T, status int, body string, capture *http.Request) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			*capture = *r
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestOpenAIStreamParsesSSE(t *testing.T) {
	var payload string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload = readAllBody(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"你\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"！\"}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey, DefaultModel: "dft-model"})
	chunks, err := provider.Stream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "打招呼"}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	list := collectChunks(t, chunks)
	if len(list) != 4 {
		t.Fatalf("chunk 数量 = %d, want 4", len(list))
	}
	var text strings.Builder
	for _, chunk := range list {
		if chunk.Err != nil {
			t.Fatalf("意外错误：%v", chunk.Err)
		}
		text.WriteString(chunk.Delta)
	}
	if text.String() != "你好！" {
		t.Fatalf("合并内容 = %q, want 你好！", text.String())
	}
	last := list[len(list)-1]
	if !last.Done || last.Delta != "" {
		t.Fatalf("结束 chunk 异常：%+v", last)
	}
	if list[2].Usage == nil || list[2].Usage.TotalTokens != 5 {
		t.Fatalf("usage 未解析：%+v", list[2].Usage)
	}
	for i, chunk := range list {
		if chunk.Seq != int64(i+1) {
			t.Fatalf("chunk %d Seq = %d, want %d", i, chunk.Seq, i+1)
		}
	}

	if !strings.Contains(payload, `"model":"dft-model"`) {
		t.Fatalf("未使用默认模型：%s", payload)
	}
	if !strings.Contains(payload, `"content":"打招呼"`) {
		t.Fatalf("用户消息缺失：%s", payload)
	}
	if strings.Contains(payload, testAPIKey) {
		t.Fatal("请求体不应包含密钥")
	}
}

func TestOpenAISerializesToolsAndToolMessages(t *testing.T) {
	var payload string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload = readAllBody(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey, DefaultModel: "dft-model"})
	chunks, err := provider.Stream(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "查一下"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call-1", Name: "app:demo/runTask", Arguments: `{"x":1}`}}},
			{Role: "tool", ToolCallID: "call-1", Content: "done"},
		},
		Tools: []ToolSchema{{Name: "app:demo/runTask", Description: "执行任务"}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	collectChunks(t, chunks)

	for _, want := range []string{
		`"tools":[`,
		`"type":"function"`,
		`"name":"app:demo/runTask"`,
		`"parameters":{"type":"object","properties":{}}`,
		`"tool_calls":[`,
		`"tool_call_id":"call-1"`,
		`"role":"tool"`,
	} {
		if !strings.Contains(payload, want) {
			t.Fatalf("请求体缺少 %s：%s", want, payload)
		}
	}
}

func TestOpenAIStreamParsesToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"app:demo/runTask\",\"arguments\":\"{\\\"x\\\":\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"1}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey, DefaultModel: "dft-model"})
	chunks, err := provider.Stream(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var calls []ToolCall
	for _, chunk := range collectChunks(t, chunks) {
		if chunk.Err != nil {
			t.Fatalf("意外错误：%v", chunk.Err)
		}
		calls = append(calls, chunk.ToolCalls...)
	}
	if len(calls) != 2 {
		t.Fatalf("工具调用片段 = %d, want 2", len(calls))
	}
	if calls[0].Index != 0 || calls[0].ID != "call-1" || calls[0].Name != "app:demo/runTask" {
		t.Fatalf("首个片段异常：%+v", calls[0])
	}
	if calls[1].Index != 0 || calls[1].Arguments != "1}" {
		t.Fatalf("增量片段异常：%+v", calls[1])
	}
}

func TestOpenAISetsAuthorizationHeader(t *testing.T) {
	var captured http.Request
	server := sseServer(t, http.StatusOK, "data: [DONE]\n\n", &captured)
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey})
	if _, err := provider.Stream(context.Background(), ChatRequest{Model: "m"}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer "+testAPIKey {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestOpenAIClassifiesHTTPErrors(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		wantKind ErrKind
	}{
		{http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`, KindAuth},
		{http.StatusForbidden, "forbidden", KindAuth},
		{http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, KindRateLimit},
		{http.StatusInternalServerError, "boom", KindServer},
		{http.StatusBadRequest, `{"error":{"message":"bad request"}}`, KindServer},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("HTTP %d", tc.status), func(t *testing.T) {
			server := sseServer(t, tc.status, tc.body, nil)
			defer server.Close()

			provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey})
			_, err := provider.Stream(context.Background(), ChatRequest{Model: "m"})
			if err == nil {
				t.Fatal("预期返回错误")
			}

			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("错误类型 = %T, want *llm.Error", err)
			}
			if e.Kind != tc.wantKind {
				t.Fatalf("Kind = %s, want %s", e.Kind, tc.wantKind)
			}
			if strings.Contains(err.Error(), testAPIKey) {
				t.Fatal("错误信息不应包含密钥")
			}
		})
	}
}

func TestOpenAIStreamCancelStopsRequest(t *testing.T) {
	serverDone := make(chan time.Time, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"开头\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
			serverDone <- time.Now()
		case <-time.After(3 * time.Second):
			serverDone <- time.Now()
		}
	}))
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey})
	ctx, cancel := context.WithCancel(context.Background())
	chunks, err := provider.Stream(ctx, ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	<-chunks // 等首个增量到达
	cancelAt := time.Now()
	cancel()

	var sawCanceled bool
	for chunk := range chunks {
		if IsCanceled(chunk.Err) {
			sawCanceled = true
		}
	}
	select {
	case at := <-serverDone:
		if at.Sub(cancelAt) > time.Second {
			t.Fatalf("服务端在取消后 %.0fms 才感知断开", float64(at.Sub(cancelAt).Milliseconds()))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("取消后服务端未感知连接断开")
	}
	if !sawCanceled {
		t.Fatal("取消后未收到 canceled 错误")
	}
}

func TestOpenAIStreamMidStreamDisconnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"部分\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// 不发送 [DONE] 直接关闭连接。
	}))
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey})
	chunks, err := provider.Stream(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	list := collectChunks(t, chunks)
	last := list[len(list)-1]
	if last.Err == nil {
		t.Fatalf("连接中断应产生错误，得到 %+v", last)
	}
	var e *Error
	if !errors.As(last.Err, &e) || e.Kind != KindNetwork {
		t.Fatalf("错误 = %v, want network", last.Err)
	}
}

func TestOpenAIStreamHeaderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 50 * time.Millisecond}}
	provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey, HTTPClient: client})
	_, err := provider.Stream(context.Background(), ChatRequest{Model: "m"})
	if err == nil {
		t.Fatal("预期返回超时错误")
	}
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindTimeout {
		t.Fatalf("错误 = %v, want timeout", err)
	}
}

func TestOpenAICompleteReturnsContent(t *testing.T) {
	var payload string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload = readAllBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"[{\"content\":\"偏好中文\"}]"}}]}`)
	}))
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey, DefaultModel: "dft-model"})
	text, err := provider.Complete(context.Background(), ChatRequest{
		System:   "只输出 JSON",
		Messages: []Message{{Role: "user", Content: "抽取记忆"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if text != `[{"content":"偏好中文"}]` {
		t.Fatalf("完整结果 = %q", text)
	}
	if !strings.Contains(payload, `"stream":false`) {
		t.Fatalf("非流式请求应设置 stream=false：%s", payload)
	}
	if !strings.Contains(payload, `"model":"dft-model"`) {
		t.Fatalf("未使用默认模型：%s", payload)
	}
	if strings.Contains(payload, testAPIKey) {
		t.Fatal("请求体不应包含密钥")
	}
}

func TestOpenAICompleteClassifiesHTTPError(t *testing.T) {
	server := sseServer(t, http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`, nil)
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey})
	_, err := provider.Complete(context.Background(), ChatRequest{Model: "m"})
	if err == nil {
		t.Fatal("预期返回错误")
	}
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindAuth {
		t.Fatalf("错误 = %v, want auth", err)
	}
	if strings.Contains(err.Error(), testAPIKey) {
		t.Fatal("错误信息不应包含密钥")
	}
}

func TestOpenAIModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("请求路径 = %s, want /models", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"model-a"},{"id":"model-b"}]}`)
	}))
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: testAPIKey})
	models, err := provider.Models(context.Background())
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	if len(models) != 2 || models[0].ID != "model-a" || models[1].ID != "model-b" {
		t.Fatalf("模型列表 = %+v", models)
	}
}

func TestOpenAIRejectsMissingModel(t *testing.T) {
	provider := newOpenAI(Options{BaseURL: "http://127.0.0.1:1"})
	_, err := provider.Stream(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("未配置模型 ID 时应返回错误")
	}
}

func readAllBody(t *testing.T, r *http.Request) string {
	t.Helper()
	buf := make([]byte, 4096)
	n, _ := r.Body.Read(buf)
	return string(buf[:n])
}
