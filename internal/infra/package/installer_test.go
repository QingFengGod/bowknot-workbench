package pkgs

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bowknot-workbench/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testEnv 准备临时 store 与目录布局。
func testEnv(t *testing.T) (*Installer, *store.Store, string) {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(context.Background(), store.Options{
		Path:   filepath.Join(root, "test.db"),
		Logger: testLogger(),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close(context.Background()) })

	installer := NewInstaller(Options{
		Store:       st,
		AppsRoot:    filepath.Join(root, "apps"),
		PluginsRoot: filepath.Join(root, "plugins"),
		TempRoot:    filepath.Join(root, "tmp"),
		Logger:      testLogger(),
	})
	return installer, st, root
}

// buildTar 在内存中构造一个包含 manifest 与额外文件的 tar 包。
func buildTar(t *testing.T, manifest string, extra map[string]string) io.Reader {
	t.Helper()
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	write := func(name, body string) {
		t.Helper()
		if err := writer.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     name,
			Size:     int64(len(body)),
			Mode:     0o644,
			ModTime:  time.Now(),
		}); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatalf("write body %s: %v", name, err)
		}
	}
	if manifest != "" {
		write("manifest.json", manifest)
	}
	for name, body := range extra {
		write(name, body)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return bytes.NewReader(buf.Bytes())
}

const validManifest = `{
  "id": "weekly-report",
  "name": "周报助手",
  "version": "1.2.0",
  "description": "根据工作记录生成周报",
  "entry": "index.html",
  "tools": [
    { "name": "generateWeeklyReport", "description": "生成周报草稿" }
  ]
}`

func TestParseManifestValidation(t *testing.T) {
	if _, err := ParseManifest([]byte(validManifest)); err != nil {
		t.Fatalf("合法 manifest 校验失败：%v", err)
	}

	cases := []struct {
		name  string
		input string
	}{
		{"ID 含大写", `{"id":"Weekly","name":"x","version":"1.0.0"}`},
		{"ID 过长", `{"id":"` + strings.Repeat("a", 65) + `","name":"x","version":"1.0.0"}`},
		{"版本非法", `{"id":"demo","name":"x","version":"1.0"}`},
		{"name 为空", `{"id":"demo","name":"","version":"1.0.0"}`},
		{"entry 穿越", `{"id":"demo","name":"x","version":"1.0.0","entry":"../evil.html"}`},
		{"工具名非法", `{"id":"demo","name":"x","version":"1.0.0","tools":[{"name":"9bad"}]}`},
		{"工具名重复", `{"id":"demo","name":"x","version":"1.0.0","tools":[{"name":"t"},{"name":"t"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(tc.input)); err == nil {
				t.Fatalf("%s 应被拒绝", tc.name)
			}
		})
	}
}

func TestExtractTarRejectsUnsafeEntries(t *testing.T) {
	dest := t.TempDir()

	cases := []struct {
		name  string
		build func(*tar.Writer)
	}{
		{"父目录穿越", func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: "../evil.txt", Typeflag: tar.TypeReg, Size: 1})
			_, _ = w.Write([]byte("x"))
		}},
		{"绝对路径", func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: "/etc/evil", Typeflag: tar.TypeReg, Size: 1})
			_, _ = w.Write([]byte("x"))
		}},
		{"反斜杠", func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: `a\..\evil.txt`, Typeflag: tar.TypeReg, Size: 1})
			_, _ = w.Write([]byte("x"))
		}},
		{"符号链接", func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
		}},
		{"硬链接", func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: "hard", Typeflag: tar.TypeLink, Linkname: "evil"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := tar.NewWriter(&buf)
			tc.build(writer)
			writer.Close()
			if err := ExtractTar(bytes.NewReader(buf.Bytes()), dest, MaxPackageSize); err == nil {
				t.Fatalf("%s 应被拒绝", tc.name)
			}
		})
	}

	escaped := filepath.Join(dest, "..", "evil.txt")
	if _, err := os.Stat(escaped); err == nil {
		t.Fatal("穿越文件不应存在")
	}
}

func TestExtractTarSizeLimit(t *testing.T) {
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	body := strings.Repeat("a", 1024)
	for i := 0; i < 4; i++ {
		_ = writer.WriteHeader(&tar.Header{Name: string(rune('a'+i)) + ".bin", Typeflag: tar.TypeReg, Size: int64(len(body))})
		writer.Write([]byte(body))
	}
	writer.Close()

	dest := t.TempDir()
	if err := ExtractTar(bytes.NewReader(buf.Bytes()), dest, 2048); err == nil {
		t.Fatal("超过体积上限应报错")
	}
}

func TestInstallLocalTarApp(t *testing.T) {
	installer, st, root := testEnv(t)

	manifest, err := installer.InstallArchive(context.Background(), KindApp, SourceTypeLocal, "memory.tar",
		buildTar(t, validManifest, map[string]string{"index.html": "<h1>hi</h1>"}))
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if manifest.ID != "weekly-report" || manifest.Version != "1.2.0" {
		t.Fatalf("manifest = %+v", manifest)
	}

	app, err := st.Apps.Get(context.Background(), "weekly-report")
	if err != nil {
		t.Fatalf("app record: %v", err)
	}
	if app.DisplayName != "周报助手" || app.SourceType != SourceTypeLocal {
		t.Fatalf("app = %+v", app)
	}
	if len(app.Tools) != 1 || app.Tools[0].Name != "generateWeeklyReport" || !app.Tools[0].Enabled {
		t.Fatalf("tools = %+v", app.Tools)
	}

	installed := filepath.Join(root, "apps", "weekly-report", "1.2.0")
	if _, err := os.Stat(filepath.Join(installed, "index.html")); err != nil {
		t.Fatalf("包文件未落盘：%v", err)
	}
	if app.InstallDir != installed {
		t.Fatalf("InstallDir = %q, want %q", app.InstallDir, installed)
	}

	// 暂存目录不残留。
	entries, _ := os.ReadDir(filepath.Join(root, "tmp"))
	if len(entries) != 0 {
		t.Fatalf("tmp 目录残留：%+v", entries)
	}

	// 相同版本重复安装被拒绝。
	_, err = installer.InstallArchive(context.Background(), KindApp, SourceTypeLocal, "memory.tar",
		buildTar(t, validManifest, nil))
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("重复安装 = %v, want ErrAlreadyInstalled", err)
	}
}

func TestInstallUpgradeKeepsPreviousVersion(t *testing.T) {
	installer, _, root := testEnv(t)
	ctx := context.Background()

	versions := []string{"1.0.0", "1.1.0", "1.2.0"}
	for _, version := range versions {
		manifest := strings.Replace(validManifest, `"version": "1.2.0"`, `"version": "`+version+`"`, 1)
		if _, err := installer.InstallArchive(ctx, KindApp, SourceTypeLocal, "x.tar",
			buildTar(t, manifest, nil)); err != nil {
			t.Fatalf("install %s: %v", version, err)
		}
	}

	dir := filepath.Join(root, "apps", "weekly-report")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	// 保留当前版本与上一个版本，更早的 1.0.0 被清理。
	if len(got) != 2 || !contains(got, "1.2.0") || !contains(got, "1.1.0") || contains(got, "1.0.0") {
		t.Fatalf("版本目录 = %+v, want [1.1.0 1.2.0]", got)
	}
}

func contains(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

func TestInstallConflictWithOtherKind(t *testing.T) {
	installer, _, _ := testEnv(t)
	ctx := context.Background()

	if _, err := installer.InstallArchive(ctx, KindPlugin, SourceTypeLocal, "p.tar",
		buildTar(t, validManifest, nil)); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	_, err := installer.InstallArchive(ctx, KindApp, SourceTypeLocal, "a.tar",
		buildTar(t, validManifest, nil))
	if err == nil || !strings.Contains(err.Error(), "已被插件占用") {
		t.Fatalf("ID 跨类型冲突未被拒绝：%v", err)
	}
}

func TestInstallRollbackOnInvalidManifest(t *testing.T) {
	installer, st, root := testEnv(t)

	_, err := installer.InstallArchive(context.Background(), KindApp, SourceTypeLocal, "bad.tar",
		buildTar(t, `{"id":"BAD-ID","name":"x","version":"1.0.0"}`, nil))
	if err == nil {
		t.Fatal("非法 manifest 应被拒绝")
	}
	if _, err := st.Apps.Get(context.Background(), "BAD-ID"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("失败安装不应写入数据库记录")
	}
	entries, _ := os.ReadDir(filepath.Join(root, "apps"))
	if len(entries) != 0 {
		t.Fatalf("apps 目录残留：%+v", entries)
	}
	entries, _ = os.ReadDir(filepath.Join(root, "tmp"))
	if len(entries) != 0 {
		t.Fatalf("tmp 目录残留：%+v", entries)
	}
}

func TestUninstallKeepData(t *testing.T) {
	installer, st, root := testEnv(t)
	ctx := context.Background()

	if _, err := installer.InstallArchive(ctx, KindApp, SourceTypeLocal, "x.tar",
		buildTar(t, validManifest, nil)); err != nil {
		t.Fatalf("install: %v", err)
	}
	dataDir := filepath.Join(root, "apps", "weekly-report", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "user.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// keepData=true：只删包目录，保留 data。
	if err := installer.Uninstall(ctx, KindApp, "weekly-report", true); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := st.Apps.Get(ctx, "weekly-report"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("卸载后记录应被删除")
	}
	if _, err := os.Stat(filepath.Join(root, "apps", "weekly-report", "1.2.0")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("包目录应被删除")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "user.json")); err != nil {
		t.Fatal("keepData=true 不应删除 data 目录")
	}

	// 重装后 data 仍在；keepData=false 时整体删除。
	if _, err := installer.InstallArchive(ctx, KindApp, SourceTypeLocal, "x.tar",
		buildTar(t, validManifest, nil)); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if err := installer.Uninstall(ctx, KindApp, "weekly-report", false); err != nil {
		t.Fatalf("uninstall keepData=false: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "apps", "weekly-report")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("keepData=false 应删除整个包目录")
	}
}

func TestClearData(t *testing.T) {
	installer, st, root := testEnv(t)
	ctx := context.Background()

	if _, err := installer.InstallArchive(ctx, KindApp, SourceTypeLocal, "x.tar",
		buildTar(t, validManifest, nil)); err != nil {
		t.Fatalf("install: %v", err)
	}
	dataDir := filepath.Join(root, "apps", "weekly-report", "data")
	os.MkdirAll(dataDir, 0o755)
	os.WriteFile(filepath.Join(dataDir, "cache.bin"), []byte(strings.Repeat("x", 128)), 0o644)

	size, err := MeasureDir(dataDir)
	if err != nil || size != 128 {
		t.Fatalf("MeasureDir = %d, %v", size, err)
	}

	if err := installer.ClearData(ctx, KindApp, "weekly-report"); err != nil {
		t.Fatalf("clear data: %v", err)
	}
	if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("数据目录应被删除")
	}
	app, err := st.Apps.Get(ctx, "weekly-report")
	if err != nil {
		t.Fatal(err)
	}
	if app.DataSize != 0 {
		t.Fatalf("DataSize = %d, want 0", app.DataSize)
	}
	if _, err := os.Stat(filepath.Join(root, "apps", "weekly-report", "1.2.0")); err != nil {
		t.Fatal("ClearData 不应删除包目录")
	}
}

func TestDownloadSizeLimit(t *testing.T) {
	server := newTestServer(t, strings.Repeat("x", 4096))
	dest := filepath.Join(t.TempDir(), "pkg.tar")

	err := Download(context.Background(), server.URL+"/pkg.tar", dest, "", 1024, nil)
	if err == nil {
		t.Fatal("超过体积上限应报错")
	}

	if err := Download(context.Background(), server.URL+"/pkg.tar", dest, "", MaxPackageSize, nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("下载文件缺失：%v", err)
	}
}

func newTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write([]byte(body))
	}))
}
