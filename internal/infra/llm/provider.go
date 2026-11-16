package llm

import (
	"context"
	"net/http"
	"time"
)

// Options 描述从配置构造 Provider 所需的字段，与 config.Model 对应但
// 不直接依赖 config 包，由调用方（cmd 层）完成配置到网关的接线。
type Options struct {
	// Provider 取 "openai-compatible" 或 "fake"；BaseURL 为空时回退到 fake，
	// 保证首次启动（未配置任何模型服务）也能以演示模式工作。
	Provider     string
	BaseURL      string
	APIKey       string
	DefaultModel string
	// HTTPClient 供测试注入自定义超时与传输行为，为 nil 时使用默认客户端。
	HTTPClient *http.Client
}

// New 根据 Options 选择实现：可配置的 BaseURL 走 OpenAI 兼容实现，
// 其余情况返回 Fake；真实实现统一包一层重试。
func New(opts Options) Provider {
	if opts.Provider == "fake" || opts.BaseURL == "" {
		return NewFake(FakeOptions{})
	}
	return WithRetry(newOpenAI(opts), RetryOptions{})
}

// RetryOptions 控制首 token 前的重试策略。
type RetryOptions struct {
	// MaxRetries 是失败后的最大重试次数，<=0 时取默认 3。
	MaxRetries int
	// Base 是指数退避的初始间隔，<=0 时取默认 500ms。
	Base time.Duration
}

// retryProvider 包装任意 Provider：只在 Stream 返回错误（即尚未产生任何输出）
// 且错误可重试时重试；流已经开始后的错误由 Chunk.Err 传递，不再重试。
type retryProvider struct {
	inner Provider
	opts  RetryOptions
}

// WithRetry 为 Provider 增加首 token 前的指数退避重试。
func WithRetry(inner Provider, opts RetryOptions) Provider {
	return &retryProvider{inner: inner, opts: opts}
}

func (p *retryProvider) Stream(ctx context.Context, req ChatRequest) (<-chan Chunk, error) {
	var out <-chan Chunk
	err := p.doRetry(ctx, func() error {
		chunks, err := p.inner.Stream(ctx, req)
		if err != nil {
			return err
		}
		out = chunks
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Complete 与非流式实现一样可以安全重试：只有拿到完整结果才算成功。
func (p *retryProvider) Complete(ctx context.Context, req ChatRequest) (string, error) {
	var out string
	err := p.doRetry(ctx, func() error {
		text, err := p.inner.Complete(ctx, req)
		if err != nil {
			return err
		}
		out = text
		return nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

// doRetry 在可重试错误上做指数退避，直到成功或耗尽次数。
func (p *retryProvider) doRetry(ctx context.Context, attempt func() error) error {
	maxRetries := p.opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	base := p.opts.Base
	if base <= 0 {
		base = 500 * time.Millisecond
	}

	for attemptIndex := 0; ; attemptIndex++ {
		err := attempt()
		if err == nil {
			return nil
		}
		if !Retryable(err) || attemptIndex >= maxRetries {
			return err
		}

		backoff := base << attemptIndex
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return &Error{Kind: KindCanceled, Message: "请求已取消"}
		case <-timer.C:
		}
	}
}

func (p *retryProvider) Models(ctx context.Context) ([]ModelInfo, error) {
	return p.inner.Models(ctx)
}
