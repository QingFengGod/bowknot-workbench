package builtin

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	pkgs "bowknot-workbench/internal/infra/package"
	"bowknot-workbench/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// buildTar 构造一个最小可安装的内置包 tar。
func buildTar(t *testing.T, id string) []byte {
	t.Helper()
	manifest := `{"id":"` + id + `","name":"内置` + id + `","version":"1.0.0","tools":[{"name":"run","description":"执行"}]}`
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	if err := writer.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "manifest.json",
		Size:     int64(len(manifest)),
		Mode:     0o644,
		ModTime:  time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testInstaller(t *testing.T, root string) (*pkgs.Installer, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{
		Path:   filepath.Join(root, "test.db"),
		Logger: testLogger(),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close(context.Background()) })

	installer := pkgs.NewInstaller(pkgs.Options{
		Store:       st,
		AppsRoot:    filepath.Join(root, "apps"),
		PluginsRoot: filepath.Join(root, "plugins"),
		TempRoot:    filepath.Join(root, "tmp"),
		Logger:      testLogger(),
	})
	return installer, st
}

func TestEnsureInstalled(t *testing.T) {
	root := t.TempDir()
	installer, st := testInstaller(t, root)
	ctx := context.Background()

	files := fstest.MapFS{
		"apps/demo-app.tar":       &fstest.MapFile{Data: buildTar(t, "demo-app")},
		"plugins/demo-plugin.tar": &fstest.MapFile{Data: buildTar(t, "demo-plugin")},
		"apps/.gitkeep":           &fstest.MapFile{Data: []byte{}},
		"plugins/.gitkeep":        &fstest.MapFile{Data: []byte{}},
	}

	if err := ensureFrom(ctx, st, installer, testLogger(), files); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := st.Apps.Get(ctx, "demo-app"); err != nil {
		t.Fatalf("内置应用未安装：%v", err)
	}
	if _, err := st.Plugins.Get(ctx, "demo-plugin"); err != nil {
		t.Fatalf("内置插件未安装：%v", err)
	}
	value, err := st.Meta.Get(ctx, markerKey)
	if err != nil || value != markerValue {
		t.Fatalf("标记未写入：%q, %v", value, err)
	}
	app, _ := st.Apps.Get(ctx, "demo-app")
	if app.SourceType != pkgs.SourceTypeBuiltin {
		t.Fatalf("SourceType = %q, want builtin", app.SourceType)
	}

	// 第二次调用不重复安装：删除落盘目录后重跑，记录仍在（走标记短路）。
	os.RemoveAll(filepath.Join(root, "apps", "demo-app"))
	if err := ensureFrom(ctx, st, installer, testLogger(), files); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}
