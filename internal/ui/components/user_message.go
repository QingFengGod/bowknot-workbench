package components

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"bowknot-workbench/internal/contract"
)

// UserMessageProps 描述一条用户消息。用户消息基本都是纯文本，不承载思考与工具；
// 附件以图片缩略图与文件条的形式展示在正文之前。
type UserMessageProps struct {
	Message  contract.ChatMessage
	Images   []MessageImage
	OnOpen   func(id string)
	OnDelete func()
}

// UserMessage 是右侧对齐的用户消息：头像位留空，正文按 markdown 渲染。
func UserMessage(c *ui.Context, p UserMessageProps) ui.Element {
	m := p.Message

	return MessageItem(c, MessageItemProps{
		IsUser: true,
		Body: func(c *ui.Context) {
			files := fileAttachments(m.Artifacts)
			if len(p.Images) == 0 && len(files) == 0 {
				if strings.TrimSpace(m.Content) != "" {
					markdown.View(c, m.Content, messageMarkdown(c, m, p.OnOpen))
				}
				return
			}
			// 有附件时才用列布局，避免纯文本气泡被拉宽。
			ui.Column(c).Gap(6).AlignItems(ui.End).Children(func() {
				AttachmentImages(c, p.Images, p.OnOpen)
				attachmentChips(c, files, p.OnOpen)
				if strings.TrimSpace(m.Content) != "" {
					markdown.View(c, m.Content, messageMarkdown(c, m, p.OnOpen))
				}
			})
		},
		Meta: func(c *ui.Context) {
			messageMeta(c, m, true, nil)
		},
		Actions: func(c *ui.Context) {
			userMessageActions(c, m, p.OnDelete)
		},
	})
}

// attachmentChips 用附件卡片展示非图片附件，与输入区保持同一样式。
func attachmentChips(c *ui.Context, items []contract.ArtifactBrief, onOpen func(id string)) {
	if len(items) == 0 {
		return
	}
	ui.Row(c).Wrap().Gap(6).Children(func() {
		for _, item := range items {
			item := item
			var open func()
			if onOpen != nil {
				open = func() { onOpen(item.ID) }
			}
			AttachmentChip(c, AttachmentChipProps{
				Name:   item.Title,
				Kind:   item.Kind,
				Size:   item.Size,
				OnOpen: open,
			})
		}
	})
}

// fileAttachments 过滤掉图片，图片由缩略图行展示，避免同一条附件出现两次。
func fileAttachments(artifacts []contract.ArtifactBrief) []contract.ArtifactBrief {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]contract.ArtifactBrief, 0, len(artifacts))
	for _, item := range artifacts {
		if item.Kind == contract.ArtifactImage {
			continue
		}
		out = append(out, item)
	}
	return out
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
