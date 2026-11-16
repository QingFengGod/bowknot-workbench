package shell

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeAppEntry(t *testing.T) {
	cases := []struct {
		name    string
		entry   string
		want    string
		wantErr bool
	}{
		{name: "根目录入口", entry: "index.html", want: "index.html"},
		{name: "子目录入口", entry: "web/index.html", want: "web/index.html"},
		{name: "Windows 分隔符", entry: `web\index.html`, want: "web/index.html"},
		{name: "空入口", entry: "  ", wantErr: true},
		{name: "绝对路径", entry: "/index.html", wantErr: true},
		{name: "路径穿越", entry: "../secret.html", wantErr: true},
		{name: "嵌套穿越", entry: "web/../../secret.html", wantErr: true},
		{name: "当前目录", entry: ".", wantErr: true},
		{name: "盘符路径", entry: `C:\secret.html`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := safeAppEntry(tc.entry)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("safeAppEntry(%q) = %q, want error", tc.entry, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("safeAppEntry(%q) error = %v", tc.entry, err)
			}
			if got != tc.want {
				t.Fatalf("safeAppEntry(%q) = %q, want %q", tc.entry, got, tc.want)
			}
		})
	}
}

func TestAppLauncherServe(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>ok</h1>"), 0o644); err != nil {
		t.Fatalf("写入入口文件失败: %v", err)
	}

	l := newAppLauncher()
	l.apps["quick-note"] = &appRuntime{dir: dir}

	// 原始协议地址与 Windows WebView2 的映射地址都应命中同一个应用。
	for _, target := range []string{
		"bowknot-app://quick-note/",
		"http://quick-note.bowknot-app.localhost/",
	} {
		rec := httptest.NewRecorder()
		l.serve(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", target, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "ok") {
			t.Fatalf("%s: body = %q", target, rec.Body.String())
		}
	}

	// 未登记的应用目录不能被读取。
	rec := httptest.NewRecorder()
	l.serve(rec, httptest.NewRequest(http.MethodGet, "bowknot-app://other/index.html", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知应用 status = %d, want 404", rec.Code)
	}
}
