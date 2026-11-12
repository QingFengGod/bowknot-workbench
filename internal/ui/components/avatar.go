package components

import "github.com/egoist/mygo/ui"

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

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}
