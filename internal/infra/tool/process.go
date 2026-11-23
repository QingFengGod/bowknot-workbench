// 插件进程执行器：按 manifest 声明的运行方式，以 stdio JSON-RPC 调用插件。
// 首版只支持 native（每插件一个懒启动的子进程）；bun 运行方式在此处显式拒绝。
package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/store"
)

const (
	defaultIdleTimeout   = 60 * time.Second
	maxRestartsPerMinute = 3
	initializeTimeout    = 10 * time.Second
	// mcpProtocolVersion 是宿主与插件握手时声明的协议版本。
	mcpProtocolVersion = "2024-11-05"
)

// ProcessOptions 控制插件进程执行器，零值字段使用默认值。
type ProcessOptions struct {
	Store  *store.Store
	Logger *slog.Logger
	// IdleTimeout 是插件进程的空闲回收时间，默认 60s。
	IdleTimeout time.Duration
}

// ProcessExecutor 按插件维度管理子进程并转发工具调用。
type ProcessExecutor struct {
	opts  ProcessOptions
	mu    sync.Mutex
	procs map[string]*pluginProcess
}

func NewProcessExecutor(opts ProcessOptions) *ProcessExecutor {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = defaultIdleTimeout
	}
	return &ProcessExecutor{opts: opts, procs: make(map[string]*pluginProcess)}
}

// Execute 只接管插件工具；应用工具与内置工具继续交给占位执行器，
// 保持首版行为不变。
func (e *ProcessExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	if call.Tool.Source != SourcePlugin {
		return InProcess{}.Execute(ctx, call)
	}
	proc, err := e.process(ctx, call.Tool.SourceID)
	if err != nil {
		return Result{}, err
	}
	return proc.call(ctx, call)
}

// StopPlugin 结束指定插件的进程；安装、卸载与升级前调用，
// 避免 Windows 下可执行文件被占用导致替换失败。
func (e *ProcessExecutor) StopPlugin(pluginID string) {
	e.mu.Lock()
	proc := e.procs[pluginID]
	delete(e.procs, pluginID)
	e.mu.Unlock()
	if proc != nil {
		proc.stop()
	}
}

// Close 结束全部插件进程，在应用退出时调用。
func (e *ProcessExecutor) Close(ctx context.Context) error {
	e.mu.Lock()
	procs := make([]*pluginProcess, 0, len(e.procs))
	for _, proc := range e.procs {
		procs = append(procs, proc)
	}
	e.procs = make(map[string]*pluginProcess)
	e.mu.Unlock()

	for _, proc := range procs {
		proc.stop()
	}
	return nil
}

func (e *ProcessExecutor) process(ctx context.Context, pluginID string) (*pluginProcess, error) {
	e.mu.Lock()
	if proc, ok := e.procs[pluginID]; ok {
		e.mu.Unlock()
		return proc, nil
	}
	e.mu.Unlock()

	plugin, err := e.opts.Store.Plugins.Get(ctx, pluginID)
	if err != nil {
		return nil, fmt.Errorf("读取插件 %s 失败：%w", pluginID, err)
	}
	if plugin.Runtime.Type == "" {
		return nil, fmt.Errorf("插件 %s 未声明运行方式，请升级到支持插件运行的版本", plugin.ID)
	}
	if plugin.Runtime.Type != "native" {
		return nil, fmt.Errorf("插件 %s 的运行方式 %s 尚未接入", plugin.ID, plugin.Runtime.Type)
	}

	command := plugin.Runtime.Commands[platformKey()]
	if command == "" {
		return nil, fmt.Errorf("插件 %s 不支持当前平台 %s", plugin.ID, platformKey())
	}

	proc := &pluginProcess{
		pluginID: plugin.ID,
		path:     filepath.Join(plugin.InstallDir, filepath.FromSlash(command)),
		// 数据目录跨版本共享，与安装器清理数据目录的约定保持一致。
		dataDir: filepath.Join(filepath.Dir(plugin.InstallDir), "data"),
		logger:  e.opts.Logger,
		timeout: e.opts.IdleTimeout,
		env:     pluginEnv(plugin),
	}

	e.mu.Lock()
	// 并发首次调用时以先写入的实例为准，避免启动两个进程。
	if existing, ok := e.procs[pluginID]; ok {
		e.mu.Unlock()
		return existing, nil
	}
	e.procs[pluginID] = proc
	e.mu.Unlock()
	return proc, nil
}

// pluginProcess 是一个插件的常驻子进程，调用按插件串行执行。
type pluginProcess struct {
	pluginID string
	path     string
	dataDir  string
	env      []string
	logger   *slog.Logger
	timeout  time.Duration

	// mu 串行化同一插件的调用与进程状态变更。
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	decoder *json.Decoder
	nextID  int64
	// exited 由等待协程置位，避免调用方读到已经关闭的管道。
	exited   bool
	restarts []time.Time
	lastUsed time.Time
	idle     *time.Timer
	closed   bool
}

func (p *pluginProcess) call(ctx context.Context, call Call) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return Result{}, fmt.Errorf("插件 %s 已卸载或应用正在退出", p.pluginID)
	}
	if err := p.ensureStarted(ctx); err != nil {
		return Result{}, err
	}

	args, err := argumentsObject(call.Arguments)
	if err != nil {
		return Result{}, err
	}
	raw, err := p.exchange(ctx, "tools/call", map[string]any{
		"name":      call.Tool.Name,
		"arguments": args,
	})
	if err != nil {
		return Result{}, err
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return Result{}, fmt.Errorf("解析插件 %s 的工具结果失败：%w", p.pluginID, err)
	}

	texts := make([]string, 0, len(result.Content))
	for _, item := range result.Content {
		if item.Type == "" || item.Type == "text" {
			texts = append(texts, item.Text)
		}
	}
	content := strings.Join(texts, "\n")
	if result.IsError {
		if content == "" {
			content = "插件返回了错误"
		}
		return Result{}, fmt.Errorf("%s", content)
	}
	return Result{Content: content}, nil
}

// ensureStarted 在需要时拉起插件进程并完成 initialize 握手。
// 调用方必须持有 p.mu。
func (p *pluginProcess) ensureStarted(ctx context.Context) error {
	if p.cmd != nil && !p.exited {
		p.touch()
		return nil
	}
	p.reset()

	if err := p.checkRestartBudget(); err != nil {
		return err
	}
	if err := os.MkdirAll(p.dataDir, 0o755); err != nil {
		return fmt.Errorf("创建插件数据目录失败：%w", err)
	}

	cmd := exec.Command(p.path)
	cmd.Dir = p.dataDir
	cmd.Env = p.env
	hideProcessWindow(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("打开插件进程输入失败：%w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("打开插件进程输出失败：%w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("打开插件进程错误输出失败：%w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动插件 %s 失败：%w", p.pluginID, err)
	}

	p.cmd = cmd
	p.stdin = stdin
	p.decoder = json.NewDecoder(bufio.NewReaderSize(stdout, 64*1024))
	p.exited = false
	p.lastUsed = time.Now()

	go p.scanStderr(stderr)
	go p.wait(cmd)

	initCtx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()
	raw, err := p.exchange(initCtx, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"clientInfo": map[string]any{
			"name":    "bowknot-workbench",
			"version": "1.0.0",
		},
		"capabilities": map[string]any{},
	})
	if err != nil {
		p.kill()
		return fmt.Errorf("插件 %s 初始化失败：%w", p.pluginID, err)
	}
	// initialize 结果只用于确认协议可用，字段本身先行忽略。
	_ = raw
	return nil
}

// exchange 发送一次 JSON-RPC 请求并等待同 ID 的响应，跳过服务端通知。
func (p *pluginProcess) exchange(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if p.stdin == nil || p.decoder == nil {
		return nil, errors.New("插件进程未启动")
	}
	p.nextID++
	requestID := p.nextID
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, fmt.Errorf("编码插件请求失败：%w", err)
	}
	payload = append(payload, '\n')
	if _, err := p.stdin.Write(payload); err != nil {
		p.kill()
		return nil, fmt.Errorf("写入插件进程失败：%w", err)
	}

	// 读管道无法直接响应 ctx，超时或取消时主动结束进程，让 Decode 返回。
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			p.kill()
		case <-done:
		}
	}()

	for {
		var message struct {
			ID     *int64          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Method string `json:"method"`
		}
		if err := p.decoder.Decode(&message); err != nil {
			p.exited = true
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("插件 %s 进程已断开：%w", p.pluginID, err)
		}
		// 服务端通知没有 id，与本次请求无关，直接跳过。
		if message.ID == nil {
			continue
		}
		if *message.ID != requestID {
			continue
		}
		if message.Error != nil {
			return nil, fmt.Errorf("插件 %s 返回错误（code=%d）：%s", p.pluginID, message.Error.Code, message.Error.Message)
		}
		return message.Result, nil
	}
}

func (p *pluginProcess) touch() {
	p.lastUsed = time.Now()
	if p.idle != nil {
		p.idle.Stop()
	}
	p.idle = time.AfterFunc(p.timeout, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.cmd != nil && time.Since(p.lastUsed) >= p.timeout {
			p.kill()
			p.reset()
		}
	})
}

// checkRestartBudget 限制一分钟内的重启次数，防止崩溃循环拖垮宿主。
func (p *pluginProcess) checkRestartBudget() error {
	now := time.Now()
	recent := p.restarts[:0]
	for _, at := range p.restarts {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	p.restarts = recent
	if len(p.restarts) >= maxRestartsPerMinute {
		return fmt.Errorf("插件 %s 一分钟内崩溃 %d 次，已暂停启动", p.pluginID, len(p.restarts))
	}
	p.restarts = append(p.restarts, now)
	return nil
}

func (p *pluginProcess) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		p.logger.Warn("插件进程退出", "plugin", p.pluginID, "error", err)
	}
	p.mu.Lock()
	if p.cmd == cmd {
		p.exited = true
		p.stdin = nil
		p.decoder = nil
	}
	p.mu.Unlock()
}

func (p *pluginProcess) scanStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)
	for scanner.Scan() {
		p.logger.Info("插件输出", "plugin", p.pluginID, "text", scanner.Text())
	}
}

// kill 终止子进程；调用方负责在合适时机重置状态。
func (p *pluginProcess) kill() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
}

func (p *pluginProcess) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.kill()
	p.reset()
}

// reset 清空进程状态，保留重启记录。
func (p *pluginProcess) reset() {
	if p.idle != nil {
		p.idle.Stop()
		p.idle = nil
	}
	p.cmd = nil
	p.stdin = nil
	p.decoder = nil
}

// argumentsObject 把模型给出的参数文本解析为 JSON 对象。
func argumentsObject(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("工具参数不是合法的 JSON 对象：%w", err)
	}
	return out, nil
}

// pluginEnv 构造插件进程的环境变量白名单，避免把模型密钥等敏感值传给插件。
func pluginEnv(plugin domain.Plugin) []string {
	allowed := []string{
		"PATH", "HOME", "USERPROFILE", "TMPDIR", "TEMP", "TMP",
		"SystemRoot", "ComSpec", "LANG", "LC_ALL",
		// 网络类插件需要沿用系统代理才能访问外网。
		"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY",
		"https_proxy", "http_proxy", "all_proxy", "no_proxy",
	}
	env := make([]string, 0, len(allowed)+3)
	for _, key := range allowed {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	env = append(env,
		"BOWKNOT_PLUGIN_ID="+plugin.ID,
		"BOWKNOT_PLUGIN_DIR="+plugin.InstallDir,
		"BOWKNOT_PLUGIN_DATA="+filepath.Join(filepath.Dir(plugin.InstallDir), "data"),
	)
	return env
}

// platformKey 返回当前平台在 manifest 中的标识，与安装期校验保持一致。
func platformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}
