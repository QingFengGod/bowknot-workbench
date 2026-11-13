package theme

import "github.com/egoist/mygo/ui"

// 品牌色与圆角 token：品牌色 #fb5215，圆角与间距参考常规桌面应用。
var (
	Brand        = ui.Hex("#fb5215")
	BrandHover   = ui.Hex("#ff6a33")
	BrandPressed = ui.Hex("#d9430f")
)

// 圆角 token
const (
	RadiusXS float32 = 4
	RadiusSM float32 = 8
	Radius   float32 = 12
	RadiusLG float32 = 16
)

// Mode 是应用外观模式：跟随系统、暗黑或明亮。
type Mode int

const (
	ModeSystem Mode = iota
	ModeDark
	ModeLight
)

// For 返回指定外观模式下的品牌主题副本。
// 配合 ui.Themed 局部使用；强制暗黑/明亮时以对应系统主题为基础，
// 保证 Theme.Dark 标记与色板一致，覆盖窗口的系统外观。
func For(c *ui.Context, mode Mode) *ui.Theme {
	base := c.Theme()
	switch mode {
	case ModeDark:
		base = ui.DarkTheme()
	case ModeLight:
		base = ui.LightTheme()
	}

	th := *base

	if th.Dark {
		th.Background = ui.Hex("#101114")
		th.Surface = ui.Hex("#17181c")
		th.SurfaceHover = ui.Hex("#1f2026")
		th.SurfacePressed = ui.Hex("#26272e")
		// 边框明显亮于 SurfaceHover，避免悬停背景与描边糊在一起。
		th.Border = ui.Hex("#4a4e58")
		th.Text = ui.Hex("#f2f3f5")
		th.TextMuted = ui.Hex("#9aa0a6")
	} else {
		th.Background = ui.Hex("#f7f8fa")
		th.Surface = ui.Hex("#ffffff")
		th.SurfaceHover = ui.Hex("#f2f3f5")
		th.SurfacePressed = ui.Hex("#e9ecf2")
		// 边框明显深于 SurfaceHover，保证卡片、导航描边可辨。
		th.Border = ui.Hex("#d3d9e2")
		th.Text = ui.Hex("#1f2329")
		th.TextMuted = ui.Hex("#8a9099")
	}

	th.Accent = Brand
	th.AccentHover = BrandHover
	th.AccentPressed = BrandPressed
	th.AccentText = ui.Hex("#ffffff")
	th.InactiveAccent = Brand
	th.Danger = ui.Hex("#d92d20")
	th.Warning = ui.Hex("#f79009")
	th.Success = ui.Hex("#12b76a")
	th.Selection = Brand.Alpha(0.18)
	th.Focus = Brand
	return &th
}

// Shadow 描述一层阴影的偏移、模糊、扩散，以及明暗两种颜色。
// 用法：theme.PanelShadow.Apply(c, ui.Box(c)...)。
// 阴影颜色无法交给 mygo 按明暗自动解析，因此由 Apply 依据当前主题挑选。
type Shadow struct {
	X, Y, Blur, Spread float32
	// Light 用于明亮模式；Dark 用于暗黑模式，深色背景上纯黑不可见，改用灰色。
	Light, Dark ui.Color
}

// Color 返回当前外观模式对应的阴影颜色。
func (s Shadow) Color(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return s.Dark
	}
	return s.Light
}

// Apply 把阴影套在 el 上并原样返回，便于继续链式调用。
func (s Shadow) Apply(c *ui.Context, el ui.Element) ui.Element {
	return el.Shadow(s.X, s.Y, s.Blur, s.Spread, s.Color(c))
}

// 阴影 token，按层级从低到高。
var (
	// NormalShadow 用于悬浮的普通元素，如按钮、标签。
	NormalShadow = Shadow{
		Blur:  6,
		Light: ui.RGBA(0, 0, 0, 0.12),
		Dark:  ui.RGBA(0x9a, 0xa0, 0xa6, 0.36),
	}
	// CardShadow 用于内容卡片、标题栏、侧边导航等成块表面。
	CardShadow = Shadow{
		Y: 2, Blur: 10,
		Light: ui.RGBA(0, 0, 0, 0.12),
		Dark:  ui.RGBA(0x9a, 0xa0, 0xa6, 0.16),
	}
	// PanelShadow 用于弹窗、弹出菜单、抽屉等浮层。
	PanelShadow = Shadow{
		Y: 4, Blur: 16,
		Light: ui.RGBA(0, 0, 0, 0.16),
		Dark:  ui.RGBA(0x9a, 0xa0, 0xa6, 0.22),
	}
)
