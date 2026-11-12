package components

import (
	"time"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/theme"
)

// DrawerProps 描述右侧抽屉。
type DrawerProps struct {
	Open  *bool
	Title string
	// Width 是抽屉宽度，默认 480。
	Width float32
	// Footer 是固定在抽屉底部的操作区，为空时不显示。
	Footer func()
}

// Drawer 是从右侧展开的抽屉：遮罩、固定头部与底部、可滚动内容。
// 点击遮罩、关闭按钮或按 Esc 都会把 Open 置为 false。
// 开关带 220ms 过渡：面板滑入滑出，遮罩淡入淡出。
func Drawer(c *ui.Context, p DrawerProps, body func()) {
	t := c.Theme()

	width := p.Width
	if width <= 0 {
		width = 480
	}

	// 动画挂在一个始终构建的零尺寸锚点上：关闭后仍要继续推进到 0，
	// 所以不能在一开始就 return。锚点位置必须稳定，进度才不会丢。
	anchor := ui.Box(c).Width(0).Height(0)
	target := float32(0)
	if p.Open != nil && *p.Open {
		target = 1
	}
	progress := anchor.AnimateTo("drawer", target, ui.Animation{
		Duration: 220 * time.Millisecond,
		Ease:     ui.EaseOut,
	})
	if progress <= 0.001 {
		return
	}

	ui.Overlay(c, func() {
		ui.Box(c).
			Absolute().
			Left(0).Top(0).Right(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, 0.4)).
			Opacity(progress).
			OnClick(func() { *p.Open = false })

		panel := ui.Column(c).
			Absolute().
			Top(0).Right(0).Bottom(0).
			Width(width).
			Background(t.Background).
			BorderWidth(1, 0, 0, 0).
			BorderColor(t.Border).
			Shadow(-12, 0, 32, 0, ui.RGBA(0, 0, 0, 0.18)).
			Translate((1-progress)*width, 0).
			Opacity(progress)

		if panel.OverlayShortcut(0, ui.KeyEscape) {
			*p.Open = false
		}

		panel.Children(func() {
			ui.Row(c).
				FillWidth().
				Height(56).
				Padding(0, 20).
				BorderWidth(0, 0, 1, 0).
				BorderColor(t.Border).
				AlignItems(ui.Center).
				Justify(ui.SpaceBetween).
				Children(func() {
					ui.Text(c, p.Title).FontSize(16).Bold()

					closeBtn := ui.ButtonBase(c).
						Width(28).
						Height(28).
						Radius(theme.RadiusSM).
						AlignItems(ui.Center).
						Justify(ui.Center).
						Label("关闭")
					if closeBtn.Hovered() {
						closeBtn.Background(t.SurfaceHover)
					}
					closeBtn.TextColor(t.TextMuted)
					closeBtn.Children(func() {
						Icon(c, IconClose, 16)
					})
					if closeBtn.Clicked() {
						*p.Open = false
					}
				})

			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Padding(20).Gap(14).Children(body)
			})

			if p.Footer != nil {
				ui.Row(c).
					FillWidth().
					Padding(14, 20).
					BorderWidth(1, 0, 0, 0).
					BorderColor(t.Border).
					AlignItems(ui.Center).
					Children(p.Footer)
			}
		})
	})
}
