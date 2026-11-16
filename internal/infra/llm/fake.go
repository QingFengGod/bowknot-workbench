package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// fake 默认输出节奏，与内存 mock 的演示体验保持一致。
const (
	fakeInterval   = 30 * time.Millisecond
	fakeChunkRunes = 3
)

// FakeReply 是一次脚本化回复：正文与可选的工具调用。
// 工具调用会作为一个独立的 Chunk 在正文之后发出。
type FakeReply struct {
	Content   string
	ToolCalls []ToolCall
}

// FakeOptions 控制 Fake 的行为，主要用于演示与测试。
type FakeOptions struct {
	// Interval 是两帧增量之间的间隔，<=0 时取默认 30ms。
	Interval time.Duration
	// ChunkSize 是每帧输出的字符数，<=0 时取默认 3。
	ChunkSize int
	// Failures 让前 N 次 Stream 返回可重试错误，用于验证重试逻辑。
	Failures int
	// Reply 按第 n 次（从 0 开始）调用生成脚本化回复；
	// 为 nil 时使用内置演示文案。调用序号可用于模拟多轮工具调用。
	Reply func(call int, req ChatRequest) FakeReply
	// CompleteReply 按第 n 次 Complete 调用生成完整文本；
	// 为 nil 时返回空 JSON 数组，记忆抽取与摘要拿到的是「没有内容」。
	CompleteReply func(call int, req ChatRequest) string
	// Models 为空时返回内置演示模型列表。
	Models []ModelInfo
}

// Fake 是假实现：按固定节奏把一段演示文案切成增量输出，
// 支持 ctx 取消与失败注入，行为与原内存 mock 的流式体验一致。
type Fake struct {
	mu        sync.Mutex
	opts      FakeOptions
	calls     int
	completes int
}

func NewFake(opts FakeOptions) *Fake {
	return &Fake{opts: opts}
}

// Calls 返回 Stream 被调用的次数，测试用来断言重试行为。
func (f *Fake) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *Fake) Stream(ctx context.Context, req ChatRequest) (<-chan Chunk, error) {
	f.mu.Lock()
	callIndex := f.calls
	f.calls++
	failures := f.opts.Failures
	f.mu.Unlock()

	if callIndex < failures {
		return nil, &Error{Kind: KindServer, Message: "模拟服务暂时不可用"}
	}

	reply := f.replyFor(callIndex, req)
	text := reply.Content
	interval := f.opts.Interval
	if interval <= 0 {
		interval = fakeInterval
	}
	chunkSize := f.opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = fakeChunkRunes
	}
	toolCalls := make([]ToolCall, len(reply.ToolCalls))
	copy(toolCalls, reply.ToolCalls)
	// 脚本里的工具调用是一个完整列表，按下标补齐 Index，避免其他字段残留。
	for i := range toolCalls {
		toolCalls[i].Index = i
	}

	out := make(chan Chunk, 8)
	go func() {
		defer close(out)

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

		runes := []rune(text)
		for i := 0; i < len(runes); i += chunkSize {
			select {
			case <-ctx.Done():
				emit(Chunk{Err: &Error{Kind: KindCanceled, Message: "请求已取消"}})
				return
			case <-time.After(interval):
			}
			end := i + chunkSize
			if end > len(runes) {
				end = len(runes)
			}
			if !emit(Chunk{Delta: string(runes[i:end])}) {
				return
			}
		}
		if len(toolCalls) > 0 {
			if !emit(Chunk{ToolCalls: toolCalls}) {
				return
			}
		}
		emit(Chunk{Done: true})
	}()
	return out, nil
}

// Complete 直接返回脚本化文本，默认为空 JSON 数组。
// 与 Stream 使用独立计数，互不干扰。
func (f *Fake) Complete(ctx context.Context, req ChatRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", &Error{Kind: KindCanceled, Message: "请求已取消"}
	}

	f.mu.Lock()
	callIndex := f.completes
	f.completes++
	reply := f.opts.CompleteReply
	f.mu.Unlock()

	if reply == nil {
		return "[]", nil
	}
	return reply(callIndex, req), nil
}

// Completes 返回 Complete 被调用的次数，测试用来断言抽取与摘要行为。
func (f *Fake) Completes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.completes
}

func (f *Fake) Models(ctx context.Context) ([]ModelInfo, error) {
	if len(f.opts.Models) > 0 {
		return append([]ModelInfo(nil), f.opts.Models...), nil
	}
	return []ModelInfo{
		{ID: "bk-flash", Name: "Bowknot Flash"},
		{ID: "bk-pro", Name: "Bowknot Pro"},
		{ID: "bk-reason", Name: "Bowknot Reason"},
	}, nil
}

func (f *Fake) replyFor(call int, req ChatRequest) FakeReply {
	if f.opts.Reply != nil {
		return f.opts.Reply(call, req)
	}
	return fakeReply(req)
}

// fakeReply 根据最后一条用户消息生成确定的演示文案，与原内存 mock 的模板一致。
func fakeReply(req ChatRequest) FakeReply {
	text := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			text = req.Messages[i].Content
			break
		}
	}
	title := snippet(text, 24)
	hash := 0
	for _, r := range text {
		hash += int(r)
	}
	name := req.Model
	if name == "" {
		name = "默认模型"
	}
	templates := []string{
		fmt.Sprintf("已收到：「%s」。\n\n当前使用 %s，处于本地演示模式，真实模型接入后会在这里返回完整回答。\n\n你可以先验证发送、停止与会话切换的交互。", title, name),
		fmt.Sprintf("关于「%s」，我的理解是这样：\n\n1. 先确认目标和约束\n2. 拆出最小可执行的一步\n3. 完成后再回头复盘\n\n（%s · 演示回复，未接入真实模型）", title, name),
		fmt.Sprintf("收到「%s」。\n\n这是一条演示回复，用于验证消息上屏、流式输出和停止操作。切换模型后，回复里的模型名会同步变化（当前：%s）。", title, name),
	}
	return FakeReply{Content: templates[hash%len(templates)]}
}

func snippet(text string, limit int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
}
