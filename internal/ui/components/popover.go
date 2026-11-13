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
		panel = theme.PanelShadow.Apply(c, p.
			MinWidth(140).
			Background(t.Surface).
			Radius(theme.Radius).
			BorderWidth(1).
			BorderColor(t.Border).
			Padding(t.Space(1.5)).
			GapY(t.Spacing))

		content()
	})
	return panel
}

// PopoverItemProps 描述弹出面板中的可点击行。
type PopoverItemProps struct {
	// Label 供无障碍与自动化识别使用，不参与绘制。
	Label    string
	Selected bool
	// Danger 让悬浮底色转为危险色，通常与 Content 里的红色文字搭配。
	Danger bool
	// Disabled 置灰整行并忽略点击。
	Disabled bool
	OnClick  func()
	// Content 渲染行内内容；selected 与行的选中态一致，调用方可据此调整文字颜色。
	Content func(c *ui.Context, selected bool)
}

// PopoverItem 只负责悬浮、选中、危险与禁用等行级样式以及整行点击，
// 行内内容全部由 Content 插槽提供。尺寸使用固定值，不依赖 Theme.Spacing；
// 点击后由调用方决定后续动作（通常更新状态并关闭面板）。
func PopoverItem(c *ui.Context, p PopoverItemProps) {
	t := c.Theme()

	row := ui.Box(c).
		FillWidth().
		PaddingY(t.Space(2)).
		PaddingX(t.Space(3)).
		Radius(theme.RadiusSM).
		Label(p.Label)

	switch {
	case p.Selected:
		row.Background(t.Accent.Alpha(0.12))
	case p.Disabled:
	case p.Danger && row.Hovered():
		row.Background(t.Danger.Alpha(0.1))
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}

	row.Children(func() {
		if p.Content != nil {
			p.Content(c, p.Selected)
		}
	})

	if p.Disabled {
		row.Opacity(0.5).IgnoreInput(true)
		return
	}
	if row.Clicked() && p.OnClick != nil {
		p.OnClick()
	}
}
