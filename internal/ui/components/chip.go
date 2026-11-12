package components

import "github.com/egoist/mygo/ui"

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
