package tool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"bowknot-workbench/internal/store"
)

const (
	defaultTimeout   = 30 * time.Second
	defaultMaxOutput = 64 * 1024
)

// Options 控制 Service 的行为，零值字段使用默认值。
type Options struct {
	Store *store.Store
	// Executor 是真实执行通道，为 nil 时使用 InProcess 占位实现。
	Executor Executor
	// Approver 为 nil 时危险工具一律拒绝执行。
	Approver Approver
	Logger   *slog.Logger
	// Transport 传给执行器的通道标记，默认 in-process。
	Transport string
	// Timeout 是单次执行的超时上限，默认 30s。
	Timeout time.Duration
	// MaxOutput 限制结果输出的字节数，默认 64KB。
	MaxOutput int
}

// Service 是工具执行的统一外壳：注册表解析 → 授权校验 → 危险审批 →
// 带超时的执行（捕获 panic、限制输出）→ 审计日志。
type Service struct {
	registry *Registry
	opts     Options
}

func NewService(opts Options) *Service {
	if opts.Executor == nil {
		opts.Executor = InProcess{}
	}
	if opts.Transport == "" {
		opts.Transport = TransportInProcess
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.MaxOutput <= 0 {
		opts.MaxOutput = defaultMaxOutput
	}
	return &Service{registry: NewRegistry(), opts: opts}
}

// Registry 暴露注册表，供安装流程同步条目。
func (s *Service) Registry() *Registry {
	return s.registry
}

// SyncApps 用 store 中的应用工具全量替换注册表中 app 来源的条目。
func (s *Service) SyncApps(ctx context.Context) error {
	apps, err := s.opts.Store.Apps.List(ctx)
	if err != nil {
		return fmt.Errorf("同步应用工具失败：%w", err)
	}
	var specs []ToolSpec
	for _, app := range apps {
		for _, t := range app.Tools {
			specs = append(specs, AppSpec(app.ID, t))
		}
	}
	s.registry.ReplaceSource(SourceApp, specs)
	return nil
}

// SyncPlugins 用 store 中的插件工具全量替换注册表中 plugin 来源的条目。
func (s *Service) SyncPlugins(ctx context.Context) error {
	plugins, err := s.opts.Store.Plugins.List(ctx)
	if err != nil {
		return fmt.Errorf("同步插件工具失败：%w", err)
	}
	var specs []ToolSpec
	for _, plugin := range plugins {
		for _, t := range plugin.Tools {
			specs = append(specs, PluginSpec(plugin.ID, t))
		}
	}
	s.registry.ReplaceSource(SourcePlugin, specs)
	return nil
}

// Execute 按注册表解析工具并执行；授权与确认不通过时返回可读错误。
func (s *Service) Execute(ctx context.Context, toolID, agentID, arguments string) (Result, error) {
	start := time.Now()

	spec, ok := s.registry.Lookup(toolID)
	if !ok {
		s.audit(start, ToolSpec{ID: toolID}, agentID, "not_found")
		return Result{}, fmt.Errorf("%w：未注册的工具 %s", ErrNotFound, toolID)
	}

	call := Call{Tool: spec, AgentID: agentID, Arguments: arguments, Transport: s.opts.Transport}

	if err := s.authorize(ctx, spec, agentID); err != nil {
		s.audit(start, spec, agentID, "denied")
		return Result{}, err
	}

	if spec.Dangerous {
		approved := s.opts.Approver != nil && s.opts.Approver(ctx, call)
		if !approved {
			s.audit(start, spec, agentID, "rejected")
			return Result{}, fmt.Errorf("%w：用户未确认危险工具 %s 的执行", ErrCanceled, spec.ID)
		}
	}

	result, err := s.run(ctx, call)
	if err != nil {
		s.audit(start, spec, agentID, "failed")
		return Result{}, err
	}
	s.audit(start, spec, agentID, "ok")
	return result, nil
}

// authorize 按来源校验：应用工具看 app_tool.enabled 全局开关，
// 插件工具看 plugin_grant 是否授予该 Agent。
func (s *Service) authorize(ctx context.Context, spec ToolSpec, agentID string) error {
	switch spec.Source {
	case SourceApp:
		app, err := s.opts.Store.Apps.Get(ctx, spec.SourceID)
		if err != nil {
			return fmt.Errorf("查询应用 %s 失败：%w", spec.SourceID, err)
		}
		for _, t := range app.Tools {
			if t.Name != spec.Name {
				continue
			}
			if !t.Enabled {
				return fmt.Errorf("%w：应用 %s 的工具 %s 已停用，请在应用页开启后重试", ErrDisabled, spec.SourceID, spec.Name)
			}
			return nil
		}
		return fmt.Errorf("%w：应用 %s 未提供工具 %s", ErrNotFound, spec.SourceID, spec.Name)

	case SourcePlugin:
		plugin, err := s.opts.Store.Plugins.Get(ctx, spec.SourceID)
		if err != nil {
			return fmt.Errorf("查询插件 %s 失败：%w", spec.SourceID, err)
		}
		provided := false
		for _, t := range plugin.Tools {
			if t.Name == spec.Name {
				provided = true
				break
			}
		}
		if !provided {
			return fmt.Errorf("%w：插件 %s 未提供工具 %s", ErrNotFound, spec.SourceID, spec.Name)
		}
		for _, id := range plugin.AgentIDs {
			if id == agentID {
				return nil
			}
		}
		return fmt.Errorf("%w：Agent %s 未被授权使用插件 %s 的工具 %s", ErrNotAuthorized, agentID, spec.SourceID, spec.Name)

	default:
		return fmt.Errorf("未知工具来源：%s", spec.Source)
	}
}

// run 带超时执行并在 goroutine 中捕获 panic，保证执行异常不拖垮进程。
// 执行器忽略 ctx 时 goroutine 会残留到其自然结束，这是 in-process 通道的已知限制。
func (s *Service) run(ctx context.Context, call Call) (Result, error) {
	runCtx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()

	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{err: fmt.Errorf("工具执行 panic：%v", r)}
			}
		}()
		result, err := s.opts.Executor.Execute(runCtx, call)
		done <- outcome{result: result, err: err}
	}()

	select {
	case o := <-done:
		if o.err != nil {
			return Result{}, fmt.Errorf("工具 %s 执行失败：%w", call.Tool.ID, o.err)
		}
		o.result.Content = truncateOutput(o.result.Content, s.opts.MaxOutput)
		return o.result, nil
	case <-runCtx.Done():
		if ctx.Err() == nil {
			return Result{}, fmt.Errorf("工具 %s 执行超时（上限 %s）", call.Tool.ID, s.opts.Timeout)
		}
		return Result{}, fmt.Errorf("%w：工具 %s 的执行已取消", ErrCanceled, call.Tool.ID)
	}
}

func (s *Service) audit(start time.Time, spec ToolSpec, agentID, status string) {
	if s.opts.Logger == nil {
		return
	}
	s.opts.Logger.Info("工具执行审计",
		"component", "tool",
		"tool", spec.ID,
		"source", spec.Source,
		"sourceId", spec.SourceID,
		"agent", agentID,
		"status", status,
		"durationMs", time.Since(start).Milliseconds(),
	)
}

// truncateOutput 按字节截断并回退到完整 UTF-8 边界。
func truncateOutput(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	cut := text[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n…（输出超过上限，已截断）"
}

// IsCanceled 判断错误是否属于用户拒绝或调用取消。
func IsCanceled(err error) bool {
	return errors.Is(err, ErrCanceled)
}
