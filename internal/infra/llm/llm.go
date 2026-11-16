// Package llm 是模型网关：把「一次对话生成」抽象为可取消的流，屏蔽供应商差异。
// 网关不依赖 store，持久化由上层应用服务完成；错误信息不包含 API Key。
package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Message 是发给模型的一条对话消息，Role 使用 OpenAI 约定的 user / assistant / system。
type Message struct {
	Role    string
	Content string
}

// ChatRequest 描述一次对话生成请求。
type ChatRequest struct {
	// Model 为空时由 Provider 回退到配置的 defaultModel。
	Model  string
	System string
	// Messages 按时间顺序排列，最后一条通常是最新的用户消息。
	Messages []Message
}

// Usage 是一次生成的 token 用量，上游未返回时为 nil。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ToolCall 是模型请求的工具调用，本阶段仅作为预留字段，不做解析与执行。
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Chunk 是流式输出的一次快照。Seq 由 Provider 递增分配，为后续工具调用、
// 审批事件预留。Delta 为空且没有 Usage、ToolCalls 时只携带结束或错误语义。
type Chunk struct {
	Seq       int64
	Delta     string
	ToolCalls []ToolCall
	Usage     *Usage
	Done      bool
	Err       error
}

// ModelInfo 是供应商侧可见的模型。
type ModelInfo struct {
	ID   string
	Name string
}

// Provider 是模型网关的统一入口。
//
// 约定：Stream 返回 error 时表示尚未产生任何输出内容，调用方可以安全重试；
// 流开始后的错误通过 Chunk.Err 传递，Provider 不会重试。正常结束时通道内
// 一定有一个 Done=true 的 Chunk，随后关闭通道。
type Provider interface {
	Stream(ctx context.Context, req ChatRequest) (<-chan Chunk, error)
	Models(ctx context.Context) ([]ModelInfo, error)
}

// ErrKind 归一化后的错误分类。
type ErrKind string

const (
	KindAuth      ErrKind = "auth"       // 密钥无效或未授权
	KindRateLimit ErrKind = "rate_limit" // 触发限流
	KindTimeout   ErrKind = "timeout"    // 请求或读取超时
	KindNetwork   ErrKind = "network"    // 连接失败、连接中断
	KindServer    ErrKind = "server"     // 上游返回错误
	KindCanceled  ErrKind = "canceled"   // 调用方主动取消
)

// Error 是网关归一化后的错误，上层按 Kind 映射文案，Message 不含密钥。
type Error struct {
	Kind    ErrKind
	Message string
	Status  int
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("模型请求失败（%s，HTTP %d）：%s", e.Kind, e.Status, e.Message)
	}
	return fmt.Sprintf("模型请求失败（%s）：%s", e.Kind, e.Message)
}

// Retryable 表示该类错误在尚未产生输出时是否值得重试。
func (e *Error) Retryable() bool {
	switch e.Kind {
	case KindRateLimit, KindTimeout, KindNetwork, KindServer:
		return true
	default:
		return false
	}
}

// Retryable 判断 err 是否为可重试的网关错误。
func Retryable(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Retryable()
	}
	return false
}

// IsCanceled 判断 err 是否为调用方主动取消。
func IsCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	var e *Error
	return errors.As(err, &e) && e.Kind == KindCanceled
}

// classifyTransport 把 HTTP 层错误归一化；密钥不会出现在这些错误里。
func classifyTransport(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Kind: KindCanceled, Message: "请求已取消"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: KindTimeout, Message: "请求超时"}
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if errors.Is(urlErr.Err, context.Canceled) {
			return &Error{Kind: KindCanceled, Message: "请求已取消"}
		}
		if errors.Is(urlErr.Err, context.DeadlineExceeded) {
			return &Error{Kind: KindTimeout, Message: "请求超时"}
		}
		var netErr net.Error
		if errors.As(urlErr.Err, &netErr) && netErr.Timeout() {
			return &Error{Kind: KindTimeout, Message: "请求超时"}
		}
		return &Error{Kind: KindNetwork, Message: "连接模型服务失败：" + urlErr.Op}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &Error{Kind: KindTimeout, Message: "请求超时"}
	}
	if errors.Is(err, errConnInterrupted) {
		return &Error{Kind: KindNetwork, Message: "模型连接中断"}
	}
	return &Error{Kind: KindNetwork, Message: "网络错误：" + err.Error()}
}

// errConnInterrupted 标记流式读取中途连接失败。
var errConnInterrupted = errors.New("连接中断")

// classifyStatus 按 HTTP 状态码归一化非 200 响应，body 已做截断。
func classifyStatus(status int, body string) *Error {
	kind := KindServer
	switch {
	case status == 401 || status == 403:
		kind = KindAuth
	case status == 408:
		kind = KindTimeout
	case status == 429:
		kind = KindRateLimit
	case status >= 500:
		kind = KindServer
	}
	message := strings.TrimSpace(body)
	if message == "" {
		message = fmt.Sprintf("上游返回 HTTP %d", status)
	}
	return &Error{Kind: kind, Message: message, Status: status}
}
