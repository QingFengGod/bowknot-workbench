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
	OnStop    func()
	OnRetry   func()
	OnDelete  func()
	// OnMemory 为「已记住」标记的点击回调，打开记忆面板。
	OnMemory func()
}

// MessageView 是对话消息的统一渲染入口：外壳、正文、元信息与操作都在这里。
// 后续接入工具调用、思维链等正文形态，只改本文件的 messageBody 即可，
// 所有对话页面自动保持一致。返回消息行元素，便于做提问导航等定位。
func MessageView(c *ui.Context, p MessageViewProps) ui.Element {
	m := p.Message
	isUser := m.Role == contract.RoleUser

	return MessageItem(c, MessageItemProps{
		IsUser: isUser,
		Avatar: func(c *ui.Context) {
			Avatar(c, p.AvatarText, 32)
		},
		Sender: m.SenderName,
		Body: func(c *ui.Context) {
			messageBody(c, m, p.OnStop, p.OnRetry)
		},
		Meta: func(c *ui.Context) {
			messageMeta(c, m, isUser, p.OnMemory)
		},
		Actions: func(c *ui.Context) {
			messageActions(c, m, p.ShowRetry, p.OnRetry, p.OnDelete)
		},
	})
}

// messageBody 渲染气泡正文：正文按 markdown 输出，并附带生成状态。
func messageBody(c *ui.Context, m contract.ChatMessage, onStop, onRetry func()) {
	t := c.Theme()

	if m.Status == contract.MessageStreaming {
		markdown.Stream(c, m.Content, messageMarkdown)
	} else {
		markdown.View(c, m.Content, messageMarkdown)
	}

	if len(m.ToolCalls) > 0 {
		messageToolCalls(c, m.ToolCalls)
	}

	switch m.Status {
	case contract.MessageStreaming:
		ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
			ui.Box(c).Width(6).Height(6).Radius(3).Background(t.Accent)
			ui.Text(c, "正在生成").FontSize(11).TextColor(t.TextMuted)
			if onStop != nil && TextButton(c, "停止生成", false).Clicked() {
				onStop()
			}
		})
	case contract.MessageFailed:
		errText := m.Error
		if errText == "" {
			errText = "生成失败，请稍后重试"
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
			ui.Text(c, errText).FontSize(11).TextColor(t.Danger).MaxLines(2)
			if onRetry != nil && TextButton(c, "重试", false).Clicked() {
				onRetry()
			}
		})
	case contract.MessageStopped:
		ui.Text(c, "已停止生成").FontSize(11).TextColor(t.TextMuted).Margin(8, 0, 0, 0)
	}
}

// messageToolCalls 渲染这条回复触发过的工具调用与结果。
func messageToolCalls(c *ui.Context, calls []contract.ChatToolCall) {
	t := c.Theme()

	ui.Column(c).FillWidth().Gap(6).Margin(10, 0, 0, 0).Children(func() {
		for _, call := range calls {
			statusColor := t.TextMuted
			switch call.Status {
			case "ok":
				statusColor = t.Success
			case "failed":
				statusColor = t.Danger
			case "rejected":
				statusColor = t.Warning
			}

			ui.Box(c).FillWidth().Padding(8).Radius(6).Background(t.SurfaceHover).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, toolCallName(call.ToolID)).FontSize(12).Bold().MaxLines(1)
						ui.Text(c, toolCallStatus(call.Status)).FontSize(11).TextColor(statusColor).MaxLines(1)
					})
					if strings.TrimSpace(call.Arguments) != "" {
						ui.Text(c, "参数："+call.Arguments).FontSize(11).TextColor(t.TextMuted).MaxLines(2)
					}
					if strings.TrimSpace(call.Result) != "" {
						ui.Text(c, call.Result).FontSize(11).TextColor(t.TextMuted).MaxLines(3)
					}
				})
			})
		}
	})
}

func toolCallName(toolID string) string {
	if index := strings.LastIndex(toolID, "/"); index >= 0 && index+1 < len(toolID) {
		return toolID[index+1:]
	}
	return toolID
}

func toolCallStatus(status string) string {
	switch status {
	case "running":
		return "执行中"
	case "ok":
		return "已执行"
	case "failed":
		return "执行失败"
	case "rejected":
		return "已拒绝"
	default:
		return status
	}
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
