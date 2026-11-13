package components

import "github.com/egoist/mygo/ui"

// MessageItemProps 描述一条对话消息的外壳；正文、元信息与操作都由插槽提供。
type MessageItemProps struct {
	IsUser bool
	// Avatar 渲染助手侧头像，用户消息不调用。
	Avatar func(c *ui.Context)
	// Sender 非空时显示在气泡上方，用于群聊标记发言人。
	Sender string
	// Body 渲染气泡内容：正文、工具调用、思维链等。
	Body func(c *ui.Context)
	// Meta 渲染气泡下方的时间等元信息。
	Meta func(c *ui.Context)
	// Actions 渲染悬浮时出现的操作栏。
	Actions func(c *ui.Context)
}

// MessageItem 是消息列表的一行：左右对齐、头像位、气泡外观与悬浮操作栏。
// 只负责布局与交互，具体内容由插槽决定，便于后续接入非 markdown 的消息形态。
// 返回行的根元素，供调用方做 ScrollIntoView 等定位。
func MessageItem(c *ui.Context, p MessageItemProps) ui.Element {
	t := c.Theme()

	item := ui.Column(c).FillWidth().Gap(3)
	if p.IsUser {
		item.AlignItems(ui.End)
	}
	hovered := item.Hovered()

	item.Children(func() {
		row := ui.Row(c).FillWidth().Gap(t.Space(3)).AlignItems(ui.Start)
		if p.IsUser {
			row.Justify(ui.End)
		}
		row.Children(func() {
			if !p.IsUser && p.Avatar != nil {
				p.Avatar(c)
			}

			column := ui.Column(c).Gap(3)
			if p.IsUser {
				column.MaxWidth(620).AlignItems(ui.End)
			} else {
				column.Grow(1).MaxWidth(760)
			}

			column.Children(func() {
				if p.Sender != "" {
					ui.Text(c, p.Sender).FontSize(11).Bold().TextColor(t.TextMuted)
				}

				bubble := ui.Column(c).Radius(12).Gap(0)
				if p.IsUser {
					bubble.Padding(t.Space(3)).Background(t.Accent.Alpha(0.1))
				} else {
					bubble.FillWidth()
				}
				bubble.Children(func() {
					if p.Body != nil {
						p.Body(c)
					}
				})

				if p.Meta != nil {
					p.Meta(c)
				}
				if p.Actions != nil {
					actions := ui.Row(c).Gap(2).AlignItems(ui.Center)
					if p.IsUser {
						actions.Justify(ui.End)
					}
					if !hovered {
						actions.Opacity(0).IgnoreInput(true)
					}
					actions.Children(func() {
						p.Actions(c)
					})
				}
			})
		})
	})
	return item
}
