package components

import "github.com/egoist/mygo/ui"

// InfoRow 是「名称 — 值」的信息行。
func InfoRow(c *ui.Context, label string, value string) {
	t := c.Theme()

	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, value).FontSize(12).TextColor(t.Text)
	})
}
