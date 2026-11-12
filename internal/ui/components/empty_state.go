package components

import "github.com/egoist/mygo/ui"

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
