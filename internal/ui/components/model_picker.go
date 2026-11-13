package components

import (
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/theme"
)

// ModelOption 是模型选择器中的一项。
type ModelOption struct {
	Name        string
	Description string
}

// ModelPickerProps 描述模型选择器。
type ModelPickerProps struct {
	Options  []ModelOption
	Selected string
	// Open 是面板开关，由调用方持有并保证每帧指向同一地址。
	Open *bool
	// OnSelect 在选中模型后回调，面板随后自动关闭。
	OnSelect func(name string)
}

// ModelPicker 是输入区里的模型选择器：按钮展示当前模型与下拉箭头，
// 点击后用主题化的 Popover 展开列表，行内布局交给 PopoverItem 插槽。
func ModelPicker(c *ui.Context, p ModelPickerProps) {
	t := c.Theme()

	trigger := ui.ButtonBase(c).
		Padding(t.Space(1.5), t.Space(3)).
		Radius(theme.RadiusSM).
		AlignItems(ui.Center).
		Justify(ui.Center)
	if trigger.Hovered() {
		trigger.Background(t.SurfaceHover)
	}
	trigger.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, p.Selected)
			Icon(c, IconChevron, 12).Rotate(90)
		})
	})
	if trigger.Clicked() {
		*p.Open = true
	}

	Popover(c, trigger, p.Open, func() {
		for _, option := range p.Options {
			selected := option.Name == p.Selected
			PopoverItem(c, PopoverItemProps{
				Label:    option.Name,
				Selected: selected,
				OnClick: func() {
					if p.OnSelect != nil {
						p.OnSelect(option.Name)
					}
					*p.Open = false
				},
				Content: func(c *ui.Context, selected bool) {
					ct := c.Theme()
					color := ct.Text
					if selected {
						color = ct.Accent
					}
					ui.Column(c).MinWidth(160).Gap(1).Children(func() {
						ui.Text(c, option.Name).FontSize(13).TextColor(color).MaxLines(1)
						if option.Description != "" {
							ui.Text(c, option.Description).FontSize(11).TextColor(ct.TextMuted).MaxLines(1)
						}
					})
				},
			})
		}
	})
}
