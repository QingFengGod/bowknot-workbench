package components

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/theme"
)

// Page 是原生页面的统一容器：铺满内容区，负责背景、内边距与间距。
func Page(c *ui.Context, children func()) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Padding(24).Gap(16).Children(children)
}

// HeaderProps 描述页面头部。
type HeaderProps struct {
	Title    string
	Subtitle string
	// Count 小于 0 时不显示数量徽标。
	Count int
}

// PageHeader 绘制页面头部：标题、数量、说明与右侧操作区。
func PageHeader(c *ui.Context, p HeaderProps, actions func()) {
	t := c.Theme()

	ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(16).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, p.Title).FontSize(22).Bold()
			if p.Count >= 0 {
				Chip(c, fmt.Sprint(p.Count), ToneNeutral)
			}
			if p.Subtitle != "" {
				ui.Text(c, p.Subtitle).FontSize(12).TextColor(t.TextMuted)
			}
		})

		if actions != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(actions)
		}
	})
}

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

// CardProps 描述卡片容器。
type CardProps struct {
	Padding   float32
	Gap       float32
	Hoverable bool
}

// Card 是内容卡片，Hoverable 时指针悬停显示品牌色描边。
func Card(c *ui.Context, p CardProps, children func()) ui.Element {
	t := c.Theme()

	if p.Padding == 0 {
		p.Padding = 14
	}
	if p.Gap == 0 {
		p.Gap = 8
	}

	card := ui.Column(c).
		FillWidth().
		Padding(p.Padding).
		Gap(p.Gap).
		Radius(theme.Radius).
		Background(t.Surface).
		BorderWidth(1)

	if p.Hoverable && card.Hovered() {
		card.BorderColor(t.Accent)
	} else {
		card.BorderColor(t.Border)
	}

	card.Children(children)
	return card
}

// Tone 是标签的语义色。
type Tone int

const (
	ToneNeutral Tone = iota
	ToneBrand
	ToneDanger
	ToneSuccess
)

// Chip 是小号状态标签。
func Chip(c *ui.Context, text string, tone Tone) {
	t := c.Theme()

	bg, fg := t.SurfaceHover, t.TextMuted
	switch tone {
	case ToneBrand:
		bg, fg = t.Accent.Alpha(0.12), t.Accent
	case ToneDanger:
		bg, fg = t.Danger.Alpha(0.12), t.Danger
	case ToneSuccess:
		bg, fg = t.Success.Alpha(0.12), t.Success
	}

	ui.Box(c).Padding(2, 8).Radius(999).Background(bg).Shrink(0).Children(func() {
		ui.Text(c, text).FontSize(11).TextColor(fg).MaxLines(1)
	})
}

// Avatar 是实体头像占位：圆角方块加名称首字。
func Avatar(c *ui.Context, text string, size float32) {
	t := c.Theme()

	ui.Box(c).
		Width(size).
		Height(size).
		Radius(size * 0.3).
		Background(t.Accent.Alpha(0.12)).
		Shrink(0).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Children(func() {
			ui.Text(c, firstRune(text)).FontSize(size * 0.42).Bold().TextColor(t.Accent)
		})
}

// EmptyProps 描述空状态。
type EmptyProps struct {
	Icon  IconName
	Title string
	Hint  string
	// Action 为空时不显示操作按钮。
	Action func()
}

// EmptyState 是带图标、标题、说明与可选操作的居中空状态。
func EmptyState(c *ui.Context, p EmptyProps) {
	t := c.Theme()

	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
		ui.Box(c).
			Width(76).
			Height(76).
			Radius(38).
			Background(t.Accent.Alpha(0.1)).
			TextColor(t.Accent).
			AlignItems(ui.Center).
			Justify(ui.Center).
			Children(func() {
				Icon(c, p.Icon, 32)
			})

		ui.Text(c, p.Title).FontSize(16).Bold()
		ui.Text(c, p.Hint).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(380)

		if p.Action != nil {
			ui.Box(c).Height(6)
			p.Action()
		}
	})
}

// SectionTitle 是弹窗或详情里的分节标题。
func SectionTitle(c *ui.Context, title string, trailing string) {
	t := c.Theme()

	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(13).Bold()
		if trailing != "" {
			ui.Text(c, trailing).FontSize(11).TextColor(t.TextMuted)
		}
	})
}

// InfoRow 是「名称 — 值」的信息行。
func InfoRow(c *ui.Context, label string, value string) {
	t := c.Theme()

	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, value).FontSize(12).TextColor(t.Text)
	})
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}
