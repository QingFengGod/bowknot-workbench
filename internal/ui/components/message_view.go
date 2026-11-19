package components

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/format"
)

// messageMarkdown 沿用默认主题，只把行高调得更接近 Web 端正文。
var messageMarkdown = &markdown.Options{
	Theme: &markdown.Theme{LineHeight: 1.7},
}

// MessageViewProps 描述一条对话消息的渲染。
type MessageViewProps struct {
	Message contract.ChatMessage
	// AvatarText 是助手头像里的文字，用户消息不显示。
	AvatarText string
	// ShowRetry 控制「重答」入口，通常只给最后一条已完成的助手回复。
	ShowRetry bool
	// Steps 是实时累积的思考与工具调用，仅当前会话内有效。
	Steps []AgentStep
	// StepOpen 返回时间线步骤的折叠状态指针，由页面保证地址稳定。
	StepOpen func(index int) *bool
	// ThinkingOpen 是历史消息思维链块的折叠状态。
	ThinkingOpen *bool
	// ToolsOpen 是历史消息工具调用分组的折叠状态。
	ToolsOpen *bool
	OnStop    func()
	OnRetry   func()
	OnDelete  func()
	// OnMemory 为「已记住」标记的点击回调，打开记忆面板。
	OnMemory func()
}

// MessageView 按角色分发到用户消息或 Agent 消息组件，
// 页面只关心一份属性，后续新增消息形态时在这里扩展。
func MessageView(c *ui.Context, p MessageViewProps) ui.Element {
	if p.Message.Role == contract.RoleUser {
		return UserMessage(c, UserMessageProps{
			Message:  p.Message,
			OnDelete: p.OnDelete,
		})
	}

	return AgentMessage(c, AgentMessageProps{
		Message:      p.Message,
		AvatarText:   p.AvatarText,
		Steps:        p.Steps,
		StepOpen:     p.StepOpen,
		ThinkingOpen: p.ThinkingOpen,
		ToolsOpen:    p.ToolsOpen,
		ShowRetry:    p.ShowRetry,
		OnStop:       p.OnStop,
		OnRetry:      p.OnRetry,
		OnDelete:     p.OnDelete,
		OnMemory:     p.OnMemory,
	})
}

// messageMeta 渲染气泡下方的时间、情绪与记忆标记。
func messageMeta(c *ui.Context, m contract.ChatMessage, isUser bool, onMemory func()) {
	t := c.Theme()

	row := ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center)
	if isUser {
		row.Justify(ui.End)
	}
	row.Children(func() {
		ui.Text(c, format.Clock(m.CreatedAt)).FontSize(11).TextColor(t.TextMuted)
		if m.Emotion != "" {
			ui.Text(c, "情绪："+emotionLabel(m.Emotion)).FontSize(11).TextColor(t.TextMuted)
		}
		if m.MemoryWritten {
			if onMemory != nil {
				mark := ui.ButtonBase(c).
					Height(20).
					PaddingX(6).
					Radius(4).
					AlignItems(ui.Center).
					Justify(ui.Center)
				if mark.Hovered() {
					mark.Background(t.Accent.Alpha(0.1))
				}
				mark.Children(func() {
					ui.Text(c, "已记住").FontSize(11).TextColor(t.Accent)
				})
				if mark.Tooltip("查看长期记忆").Clicked() {
					onMemory()
				}
			} else {
				ui.Text(c, "已记住").FontSize(11).TextColor(t.Accent)
			}
		}
	})
}

// messageActions 渲染悬浮时出现的操作栏。
func messageActions(c *ui.Context, m contract.ChatMessage, showRetry bool, onRetry, onDelete func()) {
	if TextButton(c, "复制", false).Clicked() && strings.TrimSpace(m.Content) != "" {
		c.Services().WriteClipboard(m.Content)
		c.Toast("已复制")
	}
	if showRetry && m.Status != contract.MessageStreaming && onRetry != nil {
		if TextButton(c, "重答", false).Clicked() {
			onRetry()
		}
	}
	if onDelete != nil && TextButton(c, "删除", true).Clicked() {
		onDelete()
	}
}

func emotionLabel(value string) string {
	switch value {
	case "happy":
		return "开心"
	case "shy":
		return "害羞"
	case "angry":
		return "生气"
	case "sad":
		return "难过"
	case "surprised":
		return "惊讶"
	case "neutral":
		return "平静"
	default:
		return value
	}
}
