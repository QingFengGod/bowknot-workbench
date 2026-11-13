package components

import (
	"time"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/theme"
)

// ModalProps 描述模态框。
type ModalProps struct {
	Open  *bool
	Title string
	// Width 是面板宽度，默认 440。
	Width float32
	// Footer 是固定在底部的操作区，为空时不显示。
	Footer func()
}

// Modal 是自绘模态框：遮罩淡入、面板缩放淡入，标题栏与操作区各带一条分隔线。
// 点击遮罩、关闭按钮或按 Esc 都会把 Open 置为 false。
// 与 Drawer 一样常驻构建动画锚点，首次打开也有过渡。
func Modal(c *ui.Context, p ModalProps, body func()) {
	t := c.Theme()

	width := p.Width
	if width <= 0 {
		width = 440
	}

	// 零尺寸锚点承载动画通道；关闭后仍要继续推进到 0，所以不能提前 return。
	anchor := ui.Box(c).Width(0).Height(0)
	target := float32(0)
	if p.Open != nil && *p.Open {
		target = 1
	}
	progress := anchor.AnimateTo("modal", target, ui.Animation{
		Duration: 180 * time.Millisecond,
		Ease:     ui.EaseOut,
	})
	if progress <= 0.001 {
		return
	}

	close := func() {
		if p.Open != nil {
			*p.Open = false
		}
	}

	ui.Overlay(c, func() {
		// 遮罩直接按进度调整底色透明度，避免把 Opacity 叠加到面板上。
		back := ui.Box(c).
			Absolute().
			Left(0).Top(0).Right(0).Bottom(0).
			Center().
			Modal().
			Background(ui.RGBA(0, 0, 0, 0.4*progress))
		if back.Clicked() {
			close()
		}

		back.Children(func() {
			panel := ui.Column(c).
				Width(width).
				Background(t.Surface).
				Radius(theme.Radius).
				BorderWidth(1).
				BorderColor(t.Border).
				Opacity(progress).
				Scale(0.96+0.04*progress, 0.96+0.04*progress).
				OnClick(func() {})
			theme.PanelShadow.Apply(c, panel)

			if panel.OverlayShortcut(0, ui.KeyEscape) {
				close()
			}

			panel.Children(func() {
				ui.Row(c).
					FillWidth().
					Padding(t.Space(3)).
					BorderWidth(0, 0, 1, 0).
					BorderColor(t.Border).
					AlignItems(ui.Center).
					Justify(ui.SpaceBetween).
					Children(func() {
						ui.Text(c, p.Title).FontSize(16).Bold().MaxLines(1)

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
							close()
						}
					})

				ui.Column(c).FillWidth().Padding(t.Space(3)).Gap(t.Space(3)).Children(body)

				if p.Footer != nil {
					ui.Row(c).
						FillWidth().
						Padding(t.Space(3)).
						Gap(t.Space(3)).
						BorderWidth(1, 0, 0, 0).
						BorderColor(t.Border).
						AlignItems(ui.Center).
						Justify(ui.End).
						Children(p.Footer)
				}
			})
		})
	})
}
