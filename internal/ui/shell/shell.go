package shell

import (
	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/pages"
	"bowknot-workbench/internal/ui/pages/agents"
	"bowknot-workbench/internal/ui/pages/aigc"
	"bowknot-workbench/internal/ui/pages/apps"
	"bowknot-workbench/internal/ui/pages/assistant"
	"bowknot-workbench/internal/ui/pages/plugins"
	"bowknot-workbench/internal/ui/theme"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Deps 是原生壳依赖的接口集合，具体实现由 cmd/desktop 注入。
type Deps struct {
	App       contract.AppService
	Plugins   contract.PluginService
	Apps      contract.AppCatalog
	Assistant contract.AssistantService
	Agents    contract.AgentService
	Memory    contract.MemoryService
}

type navItem struct {
	Route string
	Label string
	Icon  components.IconName
}

// 一级导航顺序。
var navItems = []navItem{
	{Route: "/workbench", Label: "工作台", Icon: components.IconWorkbench},
	{Route: "/apps", Label: "Apps", Icon: components.IconApps},
	{Route: "/assistant", Label: "助手", Icon: components.IconAssistant},
	{Route: "/agents", Label: "agents", Icon: components.IconAgents},
	{Route: "/plugins", Label: "插件", Icon: components.IconPlugins},
	{Route: "/aigc/generate", Label: "AIGC", Icon: components.IconAIGC},
}

// Shell 是原生窗口外壳：自绘标题栏、左侧一级导航与原生页面。
type Shell struct {
	deps Deps
	win  *mygo.Window
	// launcher 管理应用窗口与 bowknot-app 协议。
	launcher *appLauncher

	active  string
	native  map[string]pages.Page
	current pages.Page
	// mode 是外观模式：跟随系统 / 暗黑 / 明亮，由标题栏弹出框切换。
	mode theme.Mode
	// themeMenuOpen 是主题弹出框的开关状态，指针跨帧保持稳定。
	themeMenuOpen bool
}

// New 组装原生壳。所有一级页面均为原生实现，未实现的显示占位。
func New(deps Deps) *Shell {
	s := &Shell{
		deps:     deps,
		active:   "/apps",
		mode:     theme.ModeSystem,
		launcher: newAppLauncher(),
	}
	s.native = map[string]pages.Page{
		"/apps": apps.New(deps.Apps, apps.Hooks{
			Open:          s.launcher.Open,
			Close:         s.launcher.Close,
			SelectPackage: selectAppPackage,
		}),
		"/assistant":     assistant.New(deps.Assistant, deps.Memory, assistant.Hooks{Update: s.update}),
		"/agents":        agents.New(deps.Agents, deps.Memory, agents.Hooks{Update: s.update}),
		"/aigc/generate": aigc.New(),
		"/plugins":       plugins.New(deps.Plugins),
	}
	s.current = s.native[s.active]
	return s
}

// selectAppPackage 用系统文件选择器挑选 tar 应用包，取消时返回空路径。
func selectAppPackage() (string, error) {
	files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title: "选择应用包",
		Filters: []mygo.FileFilter{
			{Name: "应用包 (*.tar)", Extensions: []string{"tar"}},
		},
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}
	return files[0], nil
}

func (s *Shell) Start() {
	// 自定义协议必须先于使用它的窗口注册。
	if err := s.launcher.register(); err != nil {
		mygo.Dialog.Error("应用协议注册失败", err.Error())
	}
	s.win = mygo.NewWindow(mygo.WindowOptions{
		Title:     "Bowknot Workbench",
		Width:     1360,
		Height:    860,
		MinWidth:  960,
		MinHeight: 600,
		StateKey:  "main",
		// 隐藏系统标题栏与窗口控制按钮，全部由视图自绘；
		// 窗口阴影、圆角与缩放边缘仍由系统保留。
		TitleBarStyle:     mygo.TitleBarHidden,
		HideWindowButtons: true,
		BackgroundColor:   "light-dark(#f7f8fa, #101114)",
		Content:           ui.View(s.view),
	})
}

// titleBarHeight 是自绘标题栏的高度。
const titleBarHeight = 50

// view 是窗口根视图：自绘标题栏 + 左侧导航 + 页面内容。
func (s *Shell) view(c *ui.Context) {
	ui.Themed(c, theme.For(c, s.mode)).Fill().Children(func() {
		t := c.Theme()

		ui.Column(c).Gap(t.Space(3)).Fill().Children(func() {
			s.titleBar(c)
			ui.Row(c).Gap(t.Space(3)).Padding(0, t.Space(3), t.Space(3), t.Space(3)).Grow(1).Background(t.Background).Children(func() {
				s.rail(c)
				s.content(c)
			})
		})
	})
}

// titleBar 是自绘标题栏：左侧 logo 与标题，右侧 macOS 交通灯样式的
// 窗口控制按钮（纯色圆点，无图标）。整栏可拖动移动窗口。
func (s *Shell) titleBar(c *ui.Context) {
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
			ui.Text(c, "Bowknot Workbench").FontSize(13).Bold().MaxLines(1)
		})

		active := c.WindowActive()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			s.themeButton(c)
			trafficLight(c, active, trafficMinimize, func() { s.win.Minimize() })
			trafficLight(c, active, trafficMaximize, func() { s.win.ToggleMaximize() })
			trafficLight(c, active, trafficClose, func() { s.win.Close() })
		})
	})
}

// themeButton 是标题栏右侧的主题切换按钮：图标随当前模式变化，
// 点击弹出三选一的 popover 菜单（跟随系统 / 暗黑模式 / 明亮模式）。
func (s *Shell) themeButton(c *ui.Context) {
	t := c.Theme()

	iconName := components.IconMonitor
	hint := "主题：跟随系统"
	switch s.mode {
	case theme.ModeDark:
		iconName = components.IconMoon
		hint = "主题：暗黑模式"
	case theme.ModeLight:
		iconName = components.IconSun
		hint = "主题：明亮模式"
	}

	btn := ui.ButtonBase(c).
		Width(28).
		Height(28).
		Radius(theme.RadiusSM).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Tooltip(hint)
	if btn.Hovered() {
		btn.Background(t.SurfaceHover)
	}
	btn.Children(func() {
		components.Icon(c, iconName, 15).TextColor(t.Text)
	})

	if btn.Clicked() {
		s.themeMenuOpen = true
	}

	components.Popover(c, btn, &s.themeMenuOpen, func() {
		items := []struct {
			label string
			mode  theme.Mode
		}{
			{"跟随系统", theme.ModeSystem},
			{"暗黑模式", theme.ModeDark},
			{"明亮模式", theme.ModeLight},
		}
		for _, it := range items {
			components.PopoverItem(c, components.PopoverItemProps{
				Label:    it.label,
				Selected: s.mode == it.mode,
				OnClick: func() {
					s.mode = it.mode
					s.themeMenuOpen = false
				},
				Content: func(c *ui.Context, selected bool) {
					color := c.Theme().Text
					if selected {
						color = c.Theme().Accent
					}
					ui.Text(c, it.label).FontSize(14).TextColor(color).MaxLines(1)
				},
			})
		}
	})
}

// trafficLight 是 macOS 交通灯颜色的圆点按钮，无图标；
// 窗口未激活时整组变灰，贴近系统原生表现。
func trafficLight(c *ui.Context, active bool, color ui.Color, action func()) {
	const inactiveGray = uint8(0xb6)

	dot := ui.ButtonBase(c).
		Width(12).
		Height(12).
		Radius(6)
	if active {
		dot.Background(color)
	} else {
		dot.Background(ui.RGB(inactiveGray, inactiveGray, inactiveGray))
	}
	dot.Children(nil)

	if dot.Clicked() {
		action()
	}
}

// 交通灯三色对齐 macOS 原生取值。
var (
	trafficClose    = ui.RGB(0xff, 0x5f, 0x57)
	trafficMinimize = ui.RGB(0xfe, 0xbc, 0x2e)
	trafficMaximize = ui.RGB(0x28, 0xc8, 0x40)
)

func (s *Shell) rail(c *ui.Context) {
	t := c.Theme()

	rail := ui.Column(c).
		Width(86).
		FillHeight().
		Padding(4).
		GapY(4).
		Radius(theme.Radius).
		Background(t.Surface).
		AlignItems(ui.Center)

	theme.NormalShadow.Apply(c, rail)

	rail.Children(func() {
		for _, item := range navItems {
			s.railItem(c, item)
		}
	})
}

func (s *Shell) railItem(c *ui.Context, item navItem) {
	t := c.Theme()
	selected := item.Route == s.active

	btn := ui.ButtonBase(c).
		FillWidth().
		Height(70).
		Radius(theme.Radius).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Label(item.Label).
		Cursor(ui.Cursor(ui.CursorPointer))

	if selected {
		btn.Background(t.Accent.Alpha(0.12)).TextColor(t.Accent)
	} else {
		if btn.Hovered() {
			btn.Background(t.SurfaceHover)
		}
		btn.TextColor(t.Text)
	}

	labelColor := t.Text
	if selected {
		labelColor = t.Accent
	}

	btn.Children(func() {
		ui.Column(c).Gap(3).AlignItems(ui.Center).Children(func() {
			components.Icon(c, item.Icon, 24)
			ui.Text(c, item.Label).FontSize(12).TextColor(labelColor)
		})
	})

	if btn.Clicked() {
		s.navigate(item.Route)
	}
}

func (s *Shell) content(c *ui.Context) {
	t := c.Theme()

	if s.current != nil {
		ui.Box(c).Grow(1).FillHeight().Children(func() {
			s.current.View(c)
		})
		return
	}

	// 未实现页面的占位视图。
	ui.Column(c).Grow(1).FillHeight().AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
		ui.Text(c, "该页面建设中").FontSize(15).Bold()
		ui.Text(c, "工作台将以原生方式实现，敬请期待。").FontSize(12).TextColor(t.TextMuted)
	})
}

// update 在主线程修改页面状态并重绘，供页面接收后台流式事件。
func (s *Shell) update(fn func()) {
	if s.win == nil {
		fn()
		return
	}
	s.win.Update(fn)
}

func (s *Shell) navigate(route string) {
	if route == s.active {
		return
	}
	s.active = route

	if page, ok := s.native[route]; ok {
		s.current = page
	} else {
		s.current = nil
	}
	if s.win != nil {
		s.win.Invalidate()
	}
}
