package components

import (
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/theme"
)

// Popover 是自绘弹出层：anchor 为触发元素，open 由调用方持有并保证每帧
// 指向同一地址（通常放在页面结构体字段里）；content 在面板内构建内容。
// 基于底层 PopoverBase 原语自行绘制外观，不随 Theme.Spacing 变化。
func Popover(c *ui.Context, anchor ui.Element, open *bool, content func()) ui.Element {
	t := c.Theme()

	var panel ui.Element
	ui.PopoverBase(c, anchor, open, func(p ui.Element) {
		panel = p.
			MinWidth(140).
			Background(t.Surface).
			Radius(theme.RadiusSM).
			BorderWidth(1).
			BorderColor(t.Border).
			Shadow(0, 0, 12, 0, theme.ShadowColor.Alpha(0.2)).
			Padding(t.Space(1.5)).
			GapY(t.Spacing)

		content()
	})
	return panel
}

// PopoverItem 是弹出面板中的可点击行：选中项以品牌色底与文字标记。
// 尺寸使用固定值，不依赖 Theme.Spacing；点击后由调用方决定后续动作
// （通常更新状态并关闭面板）。
func PopoverItem(c *ui.Context, label string, selected bool, onClick func()) {
	t := c.Theme()

	btn := ui.Box(c).
		FillWidth().
		PaddingY(t.Space(2)).
		PaddingX(t.Space(3)).
		Radius(theme.RadiusXS).
		Label(label)

	if selected {
		btn.Background(t.Accent.Alpha(0.12))
	} else if btn.Hovered() {
		btn.Background(t.SurfaceHover)
	}

	btn.Children(func() {
		color := t.Text
		if selected {
			color = t.Accent
		}
		ui.Text(c, label).FontSize(14).TextColor(color).MaxLines(1)
	})

	if btn.Clicked() {
		onClick()
	}
}
