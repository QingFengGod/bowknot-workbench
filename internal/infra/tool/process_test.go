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

// buildFilesystemPlugin 编译内置 fs 插件，返回安装目录与可执行文件名。
// 进程执行器只认 manifest 声明的二进制，测试用真实编译产物验证协议。
func buildFilesystemPlugin(t *testing.T, root string) (string, string) {
	t.Helper()
	name := "bowknot-filesystem"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir := filepath.Join(root, "plugins", "file-system", "1.0.2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建安装目录失败：%v", err)
	}
	target := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", target, "../../../cmd/plugin-filesystem")
	build.Dir = "."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("编译 fs 插件失败：%v\n%s", err, output)
	}
	return dir, name
}

func TestProcessExecutorCallsNativePlugin(t *testing.T) {
	root := t.TempDir()
	installDir, binary := buildFilesystemPlugin(t, root)

	st, err := store.Open(context.Background(), store.Options{
		Path:   filepath.Join(root, "test.db"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close(context.Background()) })

	plugin := domain.Plugin{
		ID:         "file-system",
		Name:       "文件系统",
		Version:    "1.0.2",
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
	result, err := executor.Execute(ctx, Call{
		Tool: ToolSpec{
			ID:       "plugin:file-system/listFiles",
			Name:     "listFiles",
			Source:   SourcePlugin,
			SourceID: "file-system",
		},
		Arguments: `{"path":"."}`,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Content, "目录：") {
		t.Fatalf("listFiles 结果 = %q", result.Content)
	}

	// 越权路径必须被插件自身拒绝。
	if _, err := executor.Execute(ctx, Call{
		Tool: ToolSpec{
			ID:       "plugin:file-system/readFile",
			Name:     "readFile",
			Source:   SourcePlugin,
			SourceID: "file-system",
		},
		Arguments: `{"path":"/etc/passwd"}`,
	}); err == nil || !strings.Contains(err.Error(), "超出插件授权范围") {
		t.Fatalf("越权读取 error = %v", err)
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
