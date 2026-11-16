// Package lifecycle 统一管理根 context 与退出清理顺序。
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// closeTimeout 限制单个退出清理步骤的耗时，避免关不掉的应用卡死。
const closeTimeout = 5 * time.Second

type hook struct {
	name string
	fn   func(context.Context) error
}

// Lifecycle 持有根 context，并按注册顺序的反序执行清理。
type Lifecycle struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	hooks  []hook
	closed bool
}

func New() *Lifecycle {
	ctx, cancel := context.WithCancel(context.Background())
	return &Lifecycle{ctx: ctx, cancel: cancel}
}

// Context 返回所有后台任务应继承的根 context。
func (l *Lifecycle) Context() context.Context {
	return l.ctx
}

// Add 注册一个清理函数；关闭后再注册会立即执行。
func (l *Lifecycle) Add(name string, fn func(context.Context) error) {
	if fn == nil {
		return
	}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		_ = fn(ctx)
		return
	}
	l.hooks = append(l.hooks, hook{name: name, fn: fn})
	l.mu.Unlock()
}

// Close 取消根 context，并按注册反序执行清理，重复调用安全。
func (l *Lifecycle) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	hooks := append([]hook(nil), l.hooks...)
	l.hooks = nil
	l.mu.Unlock()

	l.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()

	var errs []error
	for i := len(hooks) - 1; i >= 0; i-- {
		if err := hooks[i].fn(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", hooks[i].name, err))
		}
	}
	return errors.Join(errs...)
}
