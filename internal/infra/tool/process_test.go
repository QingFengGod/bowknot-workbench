package tool

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/store"
)

// buildWebSearchPlugin 编译内置搜索插件，返回安装目录与可执行文件名。
// 进程执行器只认 manifest 声明的二进制，测试用真实编译产物验证协议。
func buildWebSearchPlugin(t *testing.T, root string) (string, string) {
	t.Helper()
	name := "bowknot-websearch"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir := filepath.Join(root, "plugins", "web-search", "1.0.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建安装目录失败：%v", err)
	}
	target := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", target, "../../../cmd/plugin-websearch")
	build.Dir = "."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("编译搜索插件失败：%v\n%s", err, output)
	}
	return dir, name
}

func TestProcessExecutorCallsNativePlugin(t *testing.T) {
	root := t.TempDir()
	installDir, binary := buildWebSearchPlugin(t, root)

	st, err := store.Open(context.Background(), store.Options{
		Path:   filepath.Join(root, "test.db"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close(context.Background()) })

	plugin := domain.Plugin{
		ID:         "web-search",
		Name:       "网页搜索",
		Version:    "1.0.0",
		InstallDir: installDir,
		Runtime: domain.Runtime{
			Type:     "native",
			Commands: map[string]string{platformKey(): binary},
		},
	}
	if err := st.Plugins.Insert(context.Background(), plugin); err != nil {
		t.Fatalf("insert plugin: %v", err)
	}

	executor := NewProcessExecutor(ProcessOptions{Store: st, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	t.Cleanup(func() { _ = executor.Close(context.Background()) })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 空关键词会在插件内直接报错，用来验证 stdio JSON-RPC 往返而不依赖网络。
	_, err = executor.Execute(ctx, Call{
		Tool: ToolSpec{
			ID:       "plugin:web-search/searchWeb",
			Name:     "searchWeb",
			Source:   SourcePlugin,
			SourceID: "web-search",
		},
		Arguments: `{"query":""}`,
	})
	if err == nil || !strings.Contains(err.Error(), "query 不能为空") {
		t.Fatalf("searchWeb 校验 error = %v", err)
	}
}

func TestProcessExecutorRejectsBunRuntime(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(context.Background(), store.Options{
		Path:   filepath.Join(root, "test.db"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close(context.Background()) })

	if err := st.Plugins.Insert(context.Background(), domain.Plugin{
		ID:         "js-plugin",
		Name:       "JS 插件",
		Version:    "1.0.0",
		InstallDir: root,
		Runtime:    domain.Runtime{Type: "bun", Entry: "main.js"},
	}); err != nil {
		t.Fatalf("insert plugin: %v", err)
	}

	executor := NewProcessExecutor(ProcessOptions{Store: st})
	t.Cleanup(func() { _ = executor.Close(context.Background()) })

	_, err = executor.Execute(context.Background(), Call{
		Tool: ToolSpec{
			ID:       "plugin:js-plugin/run",
			Name:     "run",
			Source:   SourcePlugin,
			SourceID: "js-plugin",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "尚未接入") {
		t.Fatalf("bun 插件 error = %v", err)
	}
}
