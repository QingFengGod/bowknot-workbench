package components

import (
	"bowknot-workbench/internal/ui/theme"

	"github.com/egoist/mygo/ui"
)

// CollapsibleSectionProps 描述可折叠分组。
type CollapsibleSectionProps struct {
	Title string
	// Open 必须由调用方持久持有，确保每帧指向同一地址。
	Open *bool
}

// CollapsibleSection 是带展开动画的标题分组：点击标题切换展开状态。
func CollapsibleSection(c *ui.Context, p CollapsibleSectionProps, body func()) {
	t := c.Theme()

	parts := ui.CollapsibleBase(c, p.Open)

	trigger := parts.Trigger.
		FillWidth().
		Padding(t.Space(3)).
		Radius(theme.RadiusSM).
		AlignItems(ui.Center).
		Justify(ui.SpaceBetween).
		TextColor(t.TextMuted)
	if trigger.Hovered() {
		trigger.Background(t.SurfaceHover)
	}
	trigger.Children(func() {
		ui.Text(c, p.Title).FontSize(12).Bold().TextColor(t.TextMuted)
		Icon(c, IconChevron, 11).Rotate(90 * parts.Progress())
	})

	panel := parts.Panel(body)
	if panel.Valid() {
		panel.Padding(0, 0, 4, 0)
	}
}
