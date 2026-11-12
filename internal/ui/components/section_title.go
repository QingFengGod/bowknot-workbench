package components

import "github.com/egoist/mygo/ui"

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
