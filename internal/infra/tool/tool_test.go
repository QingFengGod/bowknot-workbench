package tool

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testStore 返回临时库，并写入一套应用与插件工具用于授权校验。
func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{
		Path:   filepath.Join(t.TempDir(), "test.db"),
		Logger: testLogger(),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close(context.Background()) })

	ctx := context.Background()
	err = st.Tx(ctx, func(tx *store.Store) error {
		if err := tx.Apps.Insert(ctx, domain.App{
			ID:          "app-a",
			Name:        "app-a",
			DisplayName: "演示应用",
			Version:     "1.0.0",
			InstalledAt: time.Now().UnixMilli(),
		}); err != nil {
			return err
		}
		if err := tx.Apps.ReplaceTools(ctx, "app-a", []domain.Tool{
			{Name: "t1", Description: "普通工具", Enabled: true},
			{Name: "t2", Description: "危险工具", Dangerous: true, Enabled: true},
			{Name: "t3", Description: "停用工具", Enabled: false},
		}); err != nil {
			return err
		}

		if err := tx.Plugins.Insert(ctx, domain.Plugin{
			ID:          "plugin-p",
			Name:        "plugin-p",
			Version:     "1.0.0",
			InstalledAt: time.Now().UnixMilli(),
		}); err != nil {
			return err
		}
		if err := tx.Plugins.ReplaceTools(ctx, "plugin-p", []domain.Tool{
			{Name: "p1", Description: "插件工具"},
			{Name: "p2", Description: "插件危险工具", Dangerous: true},
		}); err != nil {
			return err
		}
		return tx.Plugins.SetGrants(ctx, "plugin-p", []string{"agent-1"})
	})
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}
	return st
}

func newTestService(t *testing.T, st *store.Store, mutate func(*Options)) *Service {
	t.Helper()
	opts := Options{Store: st, Logger: testLogger()}
	if mutate != nil {
		mutate(&opts)
	}
	svc := NewService(opts)
	if err := svc.SyncApps(context.Background()); err != nil {
		t.Fatalf("sync apps: %v", err)
	}
	if err := svc.SyncPlugins(context.Background()); err != nil {
		t.Fatalf("sync plugins: %v", err)
	}
	return svc
}

// stubExecutor 记录调用次数并按 fn 行为执行。
type stubExecutor struct {
	mu    sync.Mutex
	calls int
	fn    func(ctx context.Context, call Call) (Result, error)
}

func (e *stubExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	return e.fn(ctx, call)
}

func (e *stubExecutor) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func TestRegistryReplaceAndLookup(t *testing.T) {
	registry := NewRegistry()
	registry.Register(AppSpec("app-a", domain.Tool{Name: "t1"}))

	if _, ok := registry.Lookup("app:app-a/t1"); !ok {
		t.Fatal("注册后应能查到工具")
	}

	registry.ReplaceSource(SourceApp, []ToolSpec{AppSpec("app-b", domain.Tool{Name: "x"})})
	if _, ok := registry.Lookup("app:app-a/t1"); ok {
		t.Fatal("ReplaceSource 后旧来源条目应被清除")
	}
	if _, ok := registry.Lookup("app:app-b/x"); !ok {
		t.Fatal("ReplaceSource 后新条目缺失")
	}

	registry.UnregisterSource(SourceApp, "app-b")
	if len(registry.List()) != 0 {
		t.Fatalf("注销后仍有条目：%+v", registry.List())
	}
}

func TestSyncFromStore(t *testing.T) {
	svc := newTestService(t, testStore(t), nil)

	if _, ok := svc.Registry().Lookup("app:app-a/t1"); !ok {
		t.Fatal("应用工具未同步到注册表")
	}
	if _, ok := svc.Registry().Lookup("plugin:plugin-p/p1"); !ok {
		t.Fatal("插件工具未同步到注册表")
	}
	if _, ok := svc.Registry().Lookup("app:app-a/t2"); !ok {
		t.Fatal("危险工具未同步到注册表")
	}
}

func TestExecuteAppTool(t *testing.T) {
	st := testStore(t)
	svc := newTestService(t, st, nil)

	result, err := svc.Execute(context.Background(), "app:app-a/t1", "main-agent", `{"x":1}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result.Content, "app:app-a/t1") {
		t.Fatalf("占位输出异常：%q", result.Content)
	}
	if !strings.Contains(result.Content, `{"x":1}`) {
		t.Fatalf("参数未透传：%q", result.Content)
	}
}

func TestExecuteUnknownTool(t *testing.T) {
	svc := newTestService(t, testStore(t), nil)

	_, err := svc.Execute(context.Background(), "app:app-a/nope", "main-agent", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("错误 = %v, want ErrNotFound", err)
	}
}

func TestExecuteDisabledToolRejected(t *testing.T) {
	svc := newTestService(t, testStore(t), nil)

	_, err := svc.Execute(context.Background(), "app:app-a/t3", "main-agent", "")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("错误 = %v, want ErrDisabled", err)
	}
	if !strings.Contains(err.Error(), "已停用") {
		t.Fatalf("错误信息不可读：%v", err)
	}
}

func TestPluginAuthorization(t *testing.T) {
	st := testStore(t)
	svc := newTestService(t, st, nil)

	if _, err := svc.Execute(context.Background(), "plugin:plugin-p/p1", "agent-2", ""); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("未授权调用未被拒绝：%v", err)
	} else if !strings.Contains(err.Error(), "未被授权") {
		t.Fatalf("错误信息不可读：%v", err)
	}

	if _, err := svc.Execute(context.Background(), "plugin:plugin-p/p1", "agent-1", ""); err != nil {
		t.Fatalf("已授权调用失败：%v", err)
	}
}

func TestDangerousToolRequiresApproval(t *testing.T) {
	st := testStore(t)
	executor := &stubExecutor{fn: func(ctx context.Context, call Call) (Result, error) {
		return Result{Content: "done"}, nil
	}}

	// 无审批入口：危险工具一律拒绝，且不应触达执行器。
	svc := newTestService(t, st, func(o *Options) { o.Executor = executor })
	if _, err := svc.Execute(context.Background(), "app:app-a/t2", "main-agent", ""); !IsCanceled(err) {
		t.Fatalf("无审批入口时应返回 canceled：%v", err)
	}
	if executor.count() != 0 {
		t.Fatalf("确认前不应执行，调用次数 = %d", executor.count())
	}

	// 审批拒绝：同样不执行。
	denied := Approver(func(ctx context.Context, call Call) bool { return false })
	svc = newTestService(t, st, func(o *Options) { o.Executor = executor; o.Approver = denied })
	if _, err := svc.Execute(context.Background(), "app:app-a/t2", "main-agent", ""); !IsCanceled(err) {
		t.Fatalf("拒绝后应返回 canceled：%v", err)
	}
	if executor.count() != 0 {
		t.Fatalf("拒绝后不应执行，调用次数 = %d", executor.count())
	}

	// 审批通过后执行；普通工具不需要审批。
	approved := Approver(func(ctx context.Context, call Call) bool { return true })
	svc = newTestService(t, st, func(o *Options) { o.Executor = executor; o.Approver = approved })
	if _, err := svc.Execute(context.Background(), "app:app-a/t2", "main-agent", ""); err != nil {
		t.Fatalf("确认后执行失败：%v", err)
	}
	if executor.count() != 1 {
		t.Fatalf("确认后应执行一次，调用次数 = %d", executor.count())
	}
	if _, err := svc.Execute(context.Background(), "app:app-a/t1", "main-agent", ""); err != nil {
		t.Fatalf("普通工具不应触发审批：%v", err)
	}
}

func TestExecuteTimeout(t *testing.T) {
	st := testStore(t)
	executor := &stubExecutor{fn: func(ctx context.Context, call Call) (Result, error) {
		time.Sleep(200 * time.Millisecond)
		return Result{Content: "too late"}, nil
	}}
	svc := newTestService(t, st, func(o *Options) { o.Executor = executor; o.Timeout = 20 * time.Millisecond })

	_, err := svc.Execute(context.Background(), "app:app-a/t1", "main-agent", "")
	if err == nil {
		t.Fatal("执行超时应返回错误")
	}
	if IsCanceled(err) {
		t.Fatalf("超时不应被归类为取消：%v", err)
	}
	if !strings.Contains(err.Error(), "超时") {
		t.Fatalf("错误信息不可读：%v", err)
	}
}

func TestExecutePanicDoesNotKillProcess(t *testing.T) {
	st := testStore(t)
	boom := true
	executor := &stubExecutor{fn: func(ctx context.Context, call Call) (Result, error) {
		if boom {
			panic("executor exploded")
		}
		return Result{Content: "ok"}, nil
	}}
	svc := newTestService(t, st, func(o *Options) { o.Executor = executor })

	_, err := svc.Execute(context.Background(), "app:app-a/t1", "main-agent", "")
	if err == nil || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("panic 应转为错误：%v", err)
	}

	boom = false
	result, err := svc.Execute(context.Background(), "app:app-a/t1", "main-agent", "")
	if err != nil || result.Content != "ok" {
		t.Fatalf("panic 后进程应继续工作：%v, %q", err, result.Content)
	}
}

func TestOutputTruncated(t *testing.T) {
	st := testStore(t)
	executor := &stubExecutor{fn: func(ctx context.Context, call Call) (Result, error) {
		return Result{Content: strings.Repeat("汉", 100)}, nil
	}}
	svc := newTestService(t, st, func(o *Options) { o.Executor = executor; o.MaxOutput = 50 })

	result, err := svc.Execute(context.Background(), "app:app-a/t1", "main-agent", "")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(result.Content) >= 100*3 {
		t.Fatal("输出未被截断")
	}
	if !utf8.ValidString(result.Content) {
		t.Fatal("截断破坏了 UTF-8 边界")
	}
	if !strings.Contains(result.Content, "已截断") {
		t.Fatalf("缺少截断提示：%q", result.Content)
	}
}
