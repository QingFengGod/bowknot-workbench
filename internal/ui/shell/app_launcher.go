package shell

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/theme"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// appScheme 服务已安装应用的静态文件；每个应用以自己的 host 形成独立来源。
const appScheme = "bowknot-app"

// appLauncher 在独立窗口中打开应用：窗口使用原生 UI 自绘标题栏，
// 内容区是 Untrusted 沙箱 WebView，第三方代码无法调用 Go 能力。
type appLauncher struct {
	mu   sync.Mutex
	apps map[string]*appRuntime
}

type appRuntime struct {
	title string
	dir   string
	win   *mygo.Window
	page  *mygo.WebView
}

func newAppLauncher() *appLauncher {
	return &appLauncher{apps: make(map[string]*appRuntime)}
}

// register 注册应用协议，必须在创建任何使用该协议的窗口之前调用。
func (l *appLauncher) register() error {
	return mygo.Protocol.Handle(appScheme, http.HandlerFunc(l.serve))
}

// serve 只提供已经打开过的应用目录，未注册的 host 直接 404，
// 防止页面借相对路径读取其他应用的安装目录。
func (l *appLauncher) serve(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(r.URL.Hostname())
	// WebView2 把自定义协议映射成 <host>.<scheme>.localhost，这里还原回应用 ID。
	if rest, ok := strings.CutSuffix(id, "."+appScheme+".localhost"); ok && rest != "" {
		id = rest
	}

	l.mu.Lock()
	runtime, ok := l.apps[id]
	l.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	mygo.FileServer(os.DirFS(runtime.dir)).ServeHTTP(w, r)
}

// Open 打开应用窗口；同一应用重复启动时只聚焦已有窗口。
func (l *appLauncher) Open(item contract.AppItem) {
	id := strings.ToLower(item.ID)

	entry, err := safeAppEntry(item.Entry)
	if err != nil {
		mygo.Dialog.Error("无法打开应用", err.Error())
		return
	}
	dir := strings.TrimSpace(item.InstallDir)
	if dir == "" {
		mygo.Dialog.Error("无法打开应用", "应用安装目录缺失，请重新安装该应用。")
		return
	}
	file := filepath.Join(dir, filepath.FromSlash(entry))
	info, err := os.Stat(file)
	if err != nil || info.IsDir() {
		mygo.Dialog.Error("无法打开应用", "入口文件不存在："+entry)
		return
	}

	l.mu.Lock()
	runtime, ok := l.apps[id]
	if ok && runtime.win != nil && !runtime.win.IsDestroyed() {
		win := runtime.win
		l.mu.Unlock()
		win.Focus()
		return
	}
	if !ok {
		// 先把目录登记到协议处理器，页面开始加载时能立即读到文件。
		runtime = &appRuntime{}
		l.apps[id] = runtime
	}
	runtime.dir = dir
	runtime.title = item.DisplayName
	l.mu.Unlock()

	// index.html 直接导航到所在目录，避免文件服务器把 /index.html
	// 重定向成 ./ 后页面停在空白页。
	navPath := "/" + entry
	if path.Base(entry) == "index.html" {
		dir := path.Dir(entry)
		if dir == "." {
			navPath = "/"
		} else {
			navPath = "/" + dir + "/"
		}
	}
	target := (&url.URL{Scheme: appScheme, Host: id, Path: navPath}).String()
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:             item.DisplayName,
		Width:             1080,
		Height:            760,
		MinWidth:          640,
		MinHeight:         480,
		TitleBarStyle:     mygo.TitleBarHidden,
		HideWindowButtons: true,
		BackgroundColor:   "light-dark(#f7f8fa, #101114)",
		Content: ui.View(func(c *ui.Context) {
			l.appView(c, runtime)
		}),
	})
	runtime.win = win

	page, err := win.NewWebView(mygo.WebViewOptions{
		URL:             target,
		Page:            mygo.PageOptions{Untrusted: true},
		BackgroundColor: "light-dark(#ffffff, #17181c)",
	})
	if err != nil {
		mygo.Dialog.Error("无法打开应用", "创建应用页面失败："+err.Error())
		l.closeRuntime(id, runtime)
		return
	}
	runtime.page = page
	// 页面加载失败时给出可读原因，而不是留下空白窗口。
	page.Page().OnDidFailLoad(func(loadErr *mygo.LoadError) {
		mygo.Dialog.Error("应用加载失败", fmt.Sprintf("%s\n%s", loadErr.URL, loadErr.Description))
	})
	win.Invalidate()

	win.OnClosed(func() {
		l.closeRuntime(id, runtime)
	})
}

// Close 关闭应用窗口，供卸载前释放文件与页面。
func (l *appLauncher) Close(id string) {
	id = strings.ToLower(id)
	l.mu.Lock()
	runtime, ok := l.apps[id]
	l.mu.Unlock()
	if !ok {
		return
	}
	l.closeRuntime(id, runtime)
}

// closeRuntime 销毁页面与窗口，并从协议表中移除应用目录。
func (l *appLauncher) closeRuntime(id string, runtime *appRuntime) {
	l.mu.Lock()
	if current, ok := l.apps[id]; ok && current == runtime {
		delete(l.apps, id)
	}
	l.mu.Unlock()

	if runtime.page != nil && !runtime.page.IsDestroyed() {
		runtime.page.Destroy()
	}
	if runtime.win != nil && !runtime.win.IsDestroyed() {
		runtime.win.Destroy()
	}
}

// appView 是与主窗口一致的原生外壳：自绘标题栏 + 应用页面。
func (l *appLauncher) appView(c *ui.Context, runtime *appRuntime) {
	ui.Themed(c, theme.For(c, theme.ModeSystem)).Fill().Children(func() {
		t := c.Theme()
		ui.Column(c).Fill().Background(t.Background).Children(func() {
			appTitleBar(c, runtime)
			if runtime.page != nil {
				ui.WebView(c, runtime.page).Grow(1)
			}
		})
	})
}

// appTitleBar 与主窗口风格一致：左侧 logo 与标题，右侧三色交通灯按钮。
func appTitleBar(c *ui.Context, runtime *appRuntime) {
	t := c.Theme()

	bar := ui.Row(c).
		FillWidth().
		Height(titleBarHeight).
		Shrink(0).
		Background(t.Surface).
		Padding(0, 16).
		AlignItems(ui.Center).
		Justify(ui.SpaceBetween).
		DragWindow()
	theme.NormalShadow.Apply(c, bar)

	bar.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			components.Icon(c, components.IconLogo, 24).TextColor(t.Accent)
			ui.Text(c, runtime.title).FontSize(13).Bold().MaxLines(1)
		})

		active := c.WindowActive()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			trafficLight(c, active, trafficMinimize, func() { runtime.win.Minimize() })
			trafficLight(c, active, trafficMaximize, func() { runtime.win.ToggleMaximize() })
			trafficLight(c, active, trafficClose, func() { runtime.win.Close() })
		})
	})
}

// safeAppEntry 校验入口必须是安装目录内的相对路径。
func safeAppEntry(entry string) (string, error) {
	entry = strings.TrimSpace(strings.ReplaceAll(entry, "\\", "/"))
	if entry == "" {
		return "", errors.New("该应用没有配置启动入口，请重新安装包含入口的应用包。")
	}
	if strings.HasPrefix(entry, "/") {
		return "", errors.New("应用入口路径不合法")
	}
	clean := path.Clean(entry)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("应用入口路径不合法")
	}
	// 兜底拒绝 Windows 盘符与 UNC 路径，确保入口始终在安装目录内。
	if filepath.VolumeName(filepath.FromSlash(clean)) != "" {
		return "", errors.New("应用入口路径不合法")
	}
	return clean, nil
}
