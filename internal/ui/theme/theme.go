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
	th.Radius = Radius
	return &th
}

// ShadowColor 是阴影基色（纯黑），使用时用 .Alpha() 控制深浅，
// 例如卡片 theme.ShadowColor.Alpha(0.08)、浮层 theme.ShadowColor.Alpha(0.16)。
var ShadowColor = ui.RGB(0, 0, 0)
