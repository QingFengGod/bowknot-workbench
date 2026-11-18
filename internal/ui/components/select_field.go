package components

import (
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/theme"
)

// SelectOption 是下拉选择中的一项。
type SelectOption struct {
	Value       string
	Label       string
	Description string
}

// SelectFieldProps 描述设置表单里的下拉选择。
type SelectFieldProps struct {
	Value       string
	Placeholder string
	Options     []SelectOption
	// Open 是面板开关，由调用方持有并保证每帧指向同一地址。
	Open *bool
	// Width 为 0 时撑满可用宽度。
	Width    float32
	OnSelect func(value string)
}

// SelectField 是设置表单里的下拉选择：自绘触发按钮 + Popover 面板，
// 尺寸使用固定值，不随 Theme.Spacing 变化。
func SelectField(c *ui.Context, p SelectFieldProps) ui.Element {
	t := c.Theme()

	label := p.Placeholder
	if label == "" {
		label = "请选择"
	}
	for _, option := range p.Options {
		if option.Value == p.Value {
			label = option.Label
			break
		}
	}

	trigger := ui.ButtonBase(c).
		Height(32).
		PaddingX(10).
		Radius(theme.RadiusSM).
		BorderWidth(1).
		BorderColor(t.Border).
		Background(t.Surface).
		AlignItems(ui.Center).
		Justify(ui.SpaceBetween).
		Gap(8)
	if p.Width > 0 {
		trigger.Width(p.Width)
	} else {
		trigger.FillWidth()
	}
	if trigger.Hovered() {
		trigger.Background(t.SurfaceHover)
	}
	trigger.Children(func() {
		ui.Text(c, label).FontSize(13).MaxLines(1)
		Icon(c, IconChevron, 12).Rotate(90)
	})
	if trigger.Clicked() {
		*p.Open = true
	}

	Popover(c, trigger, p.Open, func() {
		if len(p.Options) == 0 {
			ui.Text(c, "暂无可选项").FontSize(12).TextColor(t.TextMuted).Padding(t.Space(2))
			return
		}
		for _, option := range p.Options {
			selected := option.Value == p.Value
			PopoverItem(c, PopoverItemProps{
				Label:    option.Label,
				Selected: selected,
				OnClick: func() {
					if p.OnSelect != nil {
						p.OnSelect(option.Value)
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
						ui.Text(c, option.Label).FontSize(13).TextColor(color).MaxLines(1)
						if option.Description != "" {
							ui.Text(c, option.Description).FontSize(11).TextColor(ct.TextMuted).MaxLines(1)
						}
					})
				},
			})
		}
	})
	return trigger
}
