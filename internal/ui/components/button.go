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
			b.Background(t.Danger.Alpha(0.8))
		} else {
			b.Background(t.Danger)
		}
		b.TextColor(ui.Hex("#fff")).BorderWidth(1).BorderColor(t.Border)
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

// TextButton 是消息操作栏里的轻量文字按钮。
func TextButton(c *ui.Context, label string, danger bool) ui.Element {
	t := c.Theme()

	fg := t.TextMuted
	bg := ui.Transparent
	b := ui.ButtonBase(c).
		Height(20).
		PaddingX(6).
		Radius(4).
		AlignItems(ui.Center).
		Justify(ui.Center)
	if b.Hovered() {
		if danger {
			fg = t.Danger
			bg = t.Danger.Alpha(0.1)
		} else {
			fg = t.Accent
			bg = t.Accent.Alpha(0.1)
		}
	}
	b.Background(bg).TextColor(fg)
	b.Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(fg)
	})
	return b
}

// CircleButton 是输入区右侧的圆形图标按钮；enabled 为 false 时半透明且不可点。
func CircleButton(c *ui.Context, icon IconName, bg, fg ui.Color, enabled bool) ui.Element {
	b := ui.ButtonBase(c).
		Width(32).
		Height(32).
		Radius(16).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Background(bg).
		TextColor(fg)
	if !enabled {
		b.Opacity(0.45).Disabled(true)
	}
	b.Children(func() {
		Icon(c, icon, 15)
	})
	return b
}
