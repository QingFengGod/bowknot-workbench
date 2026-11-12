package components

import (
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/theme"
)

// ButtonVariant 是按钮的视觉层级。
type ButtonVariant int

const (
	ButtonPrimary ButtonVariant = iota
	ButtonSecondary
	ButtonDanger
	ButtonGhost
)

// ButtonProps 描述按钮。
type ButtonProps struct {
	Label    string
	Variant  ButtonVariant
	Width    float32
	Disabled bool
}

// Button 是带主题样式的按钮；点击状态用返回元素的 Clicked 查询。
func Button(c *ui.Context, p ButtonProps) ui.Element {
	t := c.Theme()

	b := ui.Button(c, p.Label).Height(32).Radius(theme.RadiusSM).PaddingX(14).Disabled(p.Disabled)

	switch p.Variant {
	case ButtonPrimary:
		if b.Hovered() {
			b.Background(t.AccentHover)
		} else {
			b.Background(t.Accent)
		}
		b.TextColor(t.AccentText).BorderWidth(0)
	case ButtonDanger:
		if b.Hovered() {
			b.Background(t.Danger.Alpha(0.14))
		} else {
			b.Background(t.Surface)
		}
		b.TextColor(t.Danger).BorderWidth(1).BorderColor(t.Border)
	case ButtonGhost:
		b.Background(ui.Transparent).TextColor(t.TextMuted).BorderWidth(0)
	default:
		if b.Hovered() {
			b.Background(t.SurfaceHover)
		} else {
			b.Background(t.Surface)
		}
		b.TextColor(t.Text).BorderWidth(1).BorderColor(t.Border)
	}

	if p.Width > 0 {
		b.Width(p.Width)
	}
	return b
}
