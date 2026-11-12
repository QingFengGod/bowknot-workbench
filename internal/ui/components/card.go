package components

import (
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/theme"
)

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
