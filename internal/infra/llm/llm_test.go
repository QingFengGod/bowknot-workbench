package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func collectChunks(t *testing.T, chunks <-chan Chunk) []Chunk {
	t.Helper()
	out := make([]Chunk, 0, 16)
	done := make(chan struct{})
	go func() {
		for chunk := range chunks {
			out = append(out, chunk)
		}
		close(done)
	}()
	select {
	case <-done:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("等待流式输出超时")
		return nil
	}
}

func TestFakeStreamsChunksAndDone(t *testing.T) {
	fake := NewFake(FakeOptions{Interval: time.Millisecond, ChunkSize: 5})
	chunks, err := fake.Stream(context.Background(), ChatRequest{
		Model: "bk-flash",
		Messages: []Message{
			{Role: "user", Content: "写一段长一点的演示回复内容"},
		},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	list := collectChunks(t, chunks)
	if len(list) < 2 {
		t.Fatalf("增量数量 = %d, want >= 2", len(list))
	}
	var text strings.Builder
	var lastSeq int64
	for i, chunk := range list {
		if chunk.Err != nil {
			t.Fatalf("意外错误：%v", chunk.Err)
		}
		if chunk.Seq <= lastSeq {
			t.Fatalf("Seq 未递增：chunk %d seq = %d, prev = %d", i, chunk.Seq, lastSeq)
		}
		lastSeq = chunk.Seq
		isLast := i == len(list)-1
		if isLast != chunk.Done {
			t.Fatalf("chunk %d Done = %v", i, chunk.Done)
		}
		text.WriteString(chunk.Delta)
	}
	if text.Len() == 0 {
		t.Fatal("回复内容为空")
	}
	if !strings.Contains(text.String(), "演示回复") {
		t.Fatalf("演示文案缺失：%q", text.String())
	}
}

func TestFakeCancelReturnsCanceled(t *testing.T) {
	fake := NewFake(FakeOptions{Interval: 20 * time.Millisecond, ChunkSize: 1})
	ctx, cancel := context.WithCancel(context.Background())
	chunks, err := fake.Stream(ctx, ChatRequest{Messages: []Message{{Role: "user", Content: "这是一段足够长的文本，用来保证取消发生在输出过程中"}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	select {
	case <-chunks:
	case <-time.After(time.Second):
		t.Fatal("等待首个增量超时")
	}
	cancel()

	canceled := false
	for chunk := range chunks {
		if IsCanceled(chunk.Err) {
			canceled = true
		}
	}
	if !canceled {
		t.Fatal("取消后未收到 canceled 错误")
	}
}

// stubProvider 按预设列表依次返回错误；RepeatLast 时持续返回最后一个错误。
type stubProvider struct {
	mu         sync.Mutex
	calls      int
	errs       []error
	repeatLast bool
}

func (s *stubProvider) Stream(ctx context.Context, req ChatRequest) (<-chan Chunk, error) {
	s.mu.Lock()
	index := s.calls
	s.calls++
	s.mu.Unlock()
	if len(s.errs) > 0 && (index < len(s.errs) || s.repeatLast) {
		if index >= len(s.errs) {
			index = len(s.errs) - 1
		}
		return nil, s.errs[index]
	}
	out := make(chan Chunk, 1)
	out <- Chunk{Done: true}
	close(out)
	return out, nil
}

func (s *stubProvider) Models(ctx context.Context) ([]ModelInfo, error) { return nil, nil }

func TestWithRetryRetriesBeforeFirstToken(t *testing.T) {
	stub := &stubProvider{errs: []error{
		&Error{Kind: KindServer, Message: "第一次失败"},
		&Error{Kind: KindRateLimit, Message: "第二次失败"},
	}}
	provider := WithRetry(stub, RetryOptions{Base: time.Millisecond})

	chunks, err := provider.Stream(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("重试后仍失败：%v", err)
	}
	list := collectChunks(t, chunks)
	if len(list) != 1 || !list[0].Done {
		t.Fatalf("重试成功后输出异常：%+v", list)
	}
	if got := stub.calls; got != 3 {
		t.Fatalf("调用次数 = %d, want 3", got)
	}
}

func TestWithRetryStopsAfterMaxRetries(t *testing.T) {
	stub := &stubProvider{
		errs:       []error{&Error{Kind: KindServer, Message: "持续失败"}},
		repeatLast: true,
	}
	provider := WithRetry(stub, RetryOptions{MaxRetries: 2, Base: time.Millisecond})

	_, err := provider.Stream(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("超出重试次数后未返回错误")
	}
	if got := stub.calls; got != 3 {
		t.Fatalf("调用次数 = %d, want 3（首次 + 2 次重试）", got)
	}
}

func TestWithRetryDoesNotRetryAuth(t *testing.T) {
	stub := &stubProvider{errs: []error{
		&Error{Kind: KindAuth, Message: "密钥无效"},
	}}
	provider := WithRetry(stub, RetryOptions{Base: time.Millisecond})

	_, err := provider.Stream(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("应返回 auth 错误")
	}
	if Retryable(err) {
		t.Fatal("auth 错误不应可重试")
	}
	if got := stub.calls; got != 1 {
		t.Fatalf("调用次数 = %d, want 1", got)
	}
}

func TestWithRetryCancelDuringBackoff(t *testing.T) {
	stub := &stubProvider{errs: []error{
		&Error{Kind: KindServer, Message: "失败"},
	}}
	provider := WithRetry(stub, RetryOptions{Base: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := provider.Stream(ctx, ChatRequest{})
	if !IsCanceled(err) {
		t.Fatalf("退避期间取消应返回 canceled，得到 %v", err)
	}
}

func TestNewSelectsImplementation(t *testing.T) {
	if _, ok := New(Options{}).(*Fake); !ok {
		t.Fatal("未配置 baseUrl 时应返回 Fake")
	}
	if _, ok := New(Options{Provider: "fake"}).(*Fake); !ok {
		t.Fatal("provider=fake 时应返回 Fake")
	}
	if provider := New(Options{BaseURL: "http://127.0.0.1:1"}); provider == nil {
		t.Fatal("配置 baseUrl 后应返回真实实现")
	}
}

func TestErrorClassificationHelpers(t *testing.T) {
	if Retryable(&Error{Kind: KindAuth}) {
		t.Fatal("auth 不应可重试")
	}
	for _, kind := range []ErrKind{KindRateLimit, KindTimeout, KindNetwork, KindServer} {
		if !Retryable(&Error{Kind: kind}) {
			t.Fatalf("%s 应可重试", kind)
		}
	}
	if !IsCanceled(&Error{Kind: KindCanceled}) {
		t.Fatal("canceled 判断失败")
	}
	if !IsCanceled(context.Canceled) {
		t.Fatal("context.Canceled 应被识别为取消")
	}
	if IsCanceled(errors.New("其他错误")) {
		t.Fatal("普通错误不应被识别为取消")
	}
}
