package components

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"bowknot-workbench/internal/contract"
)

// UserMessageProps 描述一条用户消息。用户消息基本都是纯文本，不承载思考与工具。
type UserMessageProps struct {
	Message  contract.ChatMessage
	OnDelete func()
}

// UserMessage 是右侧对齐的用户消息：头像位留空，正文按 markdown 渲染。
func UserMessage(c *ui.Context, p UserMessageProps) ui.Element {
	m := p.Message

	return MessageItem(c, MessageItemProps{
		IsUser: true,
		Body: func(c *ui.Context) {
			if strings.TrimSpace(m.Content) != "" {
				markdown.View(c, m.Content, messageMarkdown)
			}
		},
		Meta: func(c *ui.Context) {
			messageMeta(c, m, true, nil)
		},
		Actions: func(c *ui.Context) {
			userMessageActions(c, m, p.OnDelete)
		},
	})
}

func userMessageActions(c *ui.Context, m contract.ChatMessage, onDelete func()) {
	if TextButton(c, "复制", false).Clicked() && strings.TrimSpace(m.Content) != "" {
		c.Services().WriteClipboard(m.Content)
		c.Toast("已复制")
	}
	if onDelete != nil && TextButton(c, "删除", true).Clicked() {
		onDelete()
	}
}
