package app

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bowknot-workbench/internal/contract"
	pkgs "bowknot-workbench/internal/infra/package"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/store"
)

// catalogEnv 准备应用/插件真实化所需的临时 store、安装器与工具服务。
type catalogEnv struct {
	root      string
	store     *store.Store
	installer *pkgs.Installer
	tools     *tool.Service
	catalog   *AppCatalog
}

func newCatalogEnv(t *testing.T) *catalogEnv {
	t.Helper()
	root := t.TempDir()
	st := openTestStore(t, filepath.Join(root, "test.db"), false)
	installer := pkgs.NewInstaller(pkgs.Options{
		Store:       st,
		AppsRoot:    filepath.Join(root, "apps"),
		PluginsRoot: filepath.Join(root, "plugins"),
		TempRoot:    filepath.Join(root, "tmp"),
		Logger:      testLogger(),
	})
	tools := tool.NewService(tool.Options{Store: st, Logger: testLogger()})
	return &catalogEnv{
		root:      root,
		store:     st,
		installer: installer,
		tools:     tools,
		catalog:   NewAppCatalog(st, installer, tools, testLogger()),
	}
}

// writePackageTo 在指定路径生成一个含 manifest.json 与入口文件的 tar 包。
func writePackageTo(t *testing.T, path, id, name, version string, tools ...pkgs.ManifestTool) {
	t.Helper()
	manifest := pkgs.Manifest{
		ID:          id,
		Name:        name,
		Version:     version,
		Description: "测试包",
		Tools:       tools,
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	write := func(headerName, body string) {
		t.Helper()
		if err := writer.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     headerName,
			Size:     int64(len(body)),
			Mode:     0o644,
			ModTime:  time.Now(),
		}); err != nil {
			t.Fatalf("write header %s: %v", headerName, err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatalf("write body %s: %v", headerName, err)
		}
	}
	write("manifest.json", string(data))
	write("index.html", "<h1>"+name+"</h1>")
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write package: %v", err)
	}
}

func writePackage(t *testing.T, dir, id, name, version string, tools ...pkgs.ManifestTool) string {
	t.Helper()
	path := filepath.Join(dir, id+".tar")
	writePackageTo(t, path, id, name, version, tools...)
	return path
}

func hasRegistryTool(svc *tool.Service, id string) bool {
	_, ok := svc.Registry().Lookup(id)
	return ok
}

func TestAppCatalogPreviewInstallAndUninstall(t *testing.T) {
	env := newCatalogEnv(t)
	tarPath := writePackage(t, t.TempDir(), "weekly-report", "周报助手", "1.2.0",
		pkgs.ManifestTool{Name: "generateWeeklyReport", Description: "生成周报"})

	preview, err := env.catalog.Preview(contract.InstallSource{Type: "local", Value: tarPath})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Manifest.ID != "weekly-report" || preview.Manifest.Name != "周报助手" {
		t.Fatalf("preview manifest = %+v", preview.Manifest)
	}
	if len(preview.Manifest.Tools) != 1 || !preview.Manifest.Tools[0].Enabled {
		t.Fatalf("preview tools = %+v", preview.Manifest.Tools)
	}

	if err := env.catalog.Install(preview); err != nil {
		t.Fatalf("install: %v", err)
	}
	apps := env.catalog.Apps()
	if len(apps) != 1 {
		t.Fatalf("app count = %d, want 1", len(apps))
	}
	app := apps[0]
	if app.DisplayName != "周报助手" || app.IconText != "周" || app.Version != "1.2.0" {
		t.Fatalf("app = %+v", app)
	}
	if len(app.Tools) != 1 || !app.Tools[0].Enabled {
		t.Fatalf("app tools = %+v", app.Tools)
	}
	if !hasRegistryTool(env.tools, "app:weekly-report/generateWeeklyReport") {
		t.Fatal("安装后工具未同步到注册表")
	}

	if err := env.catalog.SetToolEnabled("weekly-report", "generateWeeklyReport", false); err != nil {
		t.Fatalf("set tool enabled: %v", err)
	}
	if env.catalog.Apps()[0].Tools[0].Enabled {
		t.Fatal("工具开关未落库")
	}
	if err := env.catalog.MarkUsed("weekly-report"); err != nil {
		t.Fatalf("mark used: %v", err)
	}
	if env.catalog.Apps()[0].LastUsedAt == 0 {
		t.Fatal("最近使用时间未更新")
	}

	if err := env.catalog.Uninstall("weekly-report", true); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(env.catalog.Apps()) != 0 {
		t.Fatal("卸载后应用记录应被删除")
	}
	if hasRegistryTool(env.tools, "app:weekly-report/generateWeeklyReport") {
		t.Fatal("卸载后工具应从注册表移除")
	}
}

func TestAppCatalogRejectsChangedPackage(t *testing.T) {
	env := newCatalogEnv(t)
	dir := t.TempDir()
	tarPath := writePackage(t, dir, "pomodoro", "番茄钟", "1.0.0")

	preview, err := env.catalog.Preview(contract.InstallSource{Type: "local", Value: tarPath})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	// 预览后来源被替换为另一个版本，安装必须拒绝。
	writePackageTo(t, tarPath, "pomodoro", "番茄钟", "1.1.0")

	if err := env.catalog.Install(preview); err == nil {
		t.Fatal("包内容变化后安装应被拒绝")
	}
	if len(env.catalog.Apps()) != 0 {
		t.Fatal("被拒绝的安装不应写入记录")
	}
}

func TestAppCatalogUpgrade(t *testing.T) {
	env := newCatalogEnv(t)
	dir := t.TempDir()

	tarPath := writePackage(t, dir, "weekly-report", "周报助手", "1.0.0")
	preview, err := env.catalog.Preview(contract.InstallSource{Type: "local", Value: tarPath})
	if err != nil {
		t.Fatalf("preview v1: %v", err)
	}
	if err := env.catalog.Install(preview); err != nil {
		t.Fatalf("install v1: %v", err)
	}

	tarPath = writePackage(t, dir, "weekly-report", "周报助手", "1.1.0")
	preview, err = env.catalog.Preview(contract.InstallSource{Type: "local", Value: tarPath})
	if err != nil {
		t.Fatalf("preview v2: %v", err)
	}
	if err := env.catalog.Install(preview); err != nil {
		t.Fatalf("install v2: %v", err)
	}

	apps := env.catalog.Apps()
	if len(apps) != 1 || apps[0].Version != "1.1.0" {
		t.Fatalf("升级后应用 = %+v", apps)
	}
}

func TestAppCatalogClearData(t *testing.T) {
	env := newCatalogEnv(t)
	tarPath := writePackage(t, t.TempDir(), "weekly-report", "周报助手", "1.2.0")
	preview, err := env.catalog.Preview(contract.InstallSource{Type: "local", Value: tarPath})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if err := env.catalog.Install(preview); err != nil {
		t.Fatalf("install: %v", err)
	}

	dataDir := filepath.Join(env.root, "apps", "weekly-report", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "user.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := env.store.Apps.SetDataSize(context.Background(), "weekly-report", 2); err != nil {
		t.Fatal(err)
	}

	if err := env.catalog.ClearData("weekly-report"); err != nil {
		t.Fatalf("clear data: %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatal("数据目录应被删除")
	}
	apps := env.catalog.Apps()
	if len(apps) != 1 || apps[0].DataSize != 0 {
		t.Fatalf("清除数据后应用 = %+v", apps)
	}
}

func TestAppCatalogPersistsAcrossRestart(t *testing.T) {
	env := newCatalogEnv(t)
	tarPath := writePackage(t, t.TempDir(), "weekly-report", "周报助手", "1.2.0")
	preview, err := env.catalog.Preview(contract.InstallSource{Type: "local", Value: tarPath})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if err := env.catalog.Install(preview); err != nil {
		t.Fatalf("install: %v", err)
	}

	dbPath := filepath.Join(env.root, "test.db")
	if err := env.store.Close(context.Background()); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened := openTestStore(t, dbPath, false)
	installer := pkgs.NewInstaller(pkgs.Options{
		Store:       reopened,
		AppsRoot:    filepath.Join(env.root, "apps"),
		PluginsRoot: filepath.Join(env.root, "plugins"),
		TempRoot:    filepath.Join(env.root, "tmp"),
		Logger:      testLogger(),
	})
	catalog := NewAppCatalog(reopened, installer, tool.NewService(tool.Options{Store: reopened, Logger: testLogger()}), testLogger())

	apps := catalog.Apps()
	if len(apps) != 1 || apps[0].ID != "weekly-report" || apps[0].Version != "1.2.0" {
		t.Fatalf("重启后应用 = %+v", apps)
	}
}
