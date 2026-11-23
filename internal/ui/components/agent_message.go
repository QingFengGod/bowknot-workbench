package components

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/theme"
)

// AgentMessageProps 描述一条 Agent 回复：思考与工具调用按时间线内联在正文之上。
type AgentMessageProps struct {
	Message    contract.ChatMessage
	AvatarText string
	// Steps 是本次会话内实时累积的思考与工具步骤；为空时回退到消息里的工具调用快照。
	Steps []AgentStep
	// StepOpen 返回第 index 个步骤的折叠状态指针，由页面保证地址稳定。
	StepOpen func(index int) *bool
	// ToolOpen 返回历史消息里第 index 个工具调用的折叠状态指针。
	ToolOpen func(index int) *bool
	// ThinkingOpen 是历史消息思维链块的折叠状态。
	ThinkingOpen *bool
	// ToolsOpen 是历史消息工具调用分组的折叠状态。
	ToolsOpen *bool
	// OnOpenArtifact 打开正文文件引用指向的产物预览。
	OnOpenArtifact func(id string)
	ShowRetry      bool
	OnStop         func()
	OnRetry        func()
	OnDelete       func()
	OnMemory       func()
}

// AgentMessage 是左侧对齐的 Agent 消息：头像、时间线、正文、元信息与操作。
func AgentMessage(c *ui.Context, p AgentMessageProps) ui.Element {
	m := p.Message

	return MessageItem(c, MessageItemProps{
		IsUser: false,
		Avatar: func(c *ui.Context) {
			Avatar(c, p.AvatarText, 32)
		},
		Sender: m.SenderName,
		Body: func(c *ui.Context) {
			agentBody(c, p)
		},
		Meta: func(c *ui.Context) {
			messageMeta(c, m, false, p.OnMemory)
		},
		Actions: func(c *ui.Context) {
			messageActions(c, m, p.ShowRetry, p.OnRetry, p.OnDelete)
		},
	})
}

func agentBody(c *ui.Context, p AgentMessageProps) {
	t := c.Theme()
	m := p.Message

	thinking := strings.TrimSpace(m.Reasoning)
	switch {
	case len(p.Steps) > 0:
		ui.Column(c).FillWidth().Gap(6).Margin(0, 0, 8, 0).Children(func() {
			for index, step := range p.Steps {
				switch step.Kind {
				case AgentStepThinking:
					thinkingStep(c, step.Text, p.stepOpen(index))
				case AgentStepTool:
					toolStep(c, step.Tool, p.stepOpen(index))
				}
			}
		})
	case thinking != "" || len(m.ToolCalls) > 0:
		ui.Column(c).FillWidth().Gap(6).Margin(0, 0, 8, 0).Children(func() {
			if thinking != "" {
				thinkingStep(c, thinking, p.ThinkingOpen)
			}
			if len(m.ToolCalls) > 0 {
				toolGroup(c, m.ToolCalls, p.ToolsOpen, p.ToolOpen)
			}
		})
	}

	if strings.TrimSpace(m.Content) != "" {
		options := messageMarkdown(c, m, p.OnOpenArtifact)
		if m.Status == contract.MessageStreaming {
			markdown.Stream(c, m.Content, options)
		} else {
			markdown.View(c, m.Content, options)
		}
	}

	// 文件条放在气泡内部，避免产物像挂在消息外面的附件清单。
	if m.Status == contract.MessageStreaming && len(m.Artifacts) == 0 && artifactGenerating(p.Steps) {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
			ui.Box(c).Width(6).Height(6).Radius(3).Background(t.Accent)
			ui.Text(c, "正在生成文件…").FontSize(11).TextColor(t.TextMuted)
		})
	}
	if len(m.Artifacts) > 0 && p.OnOpenArtifact != nil {
		ui.Column(c).FillWidth().Margin(8, 0, 0, 0).Children(func() {
			ArtifactList(c, m.Artifacts, p.OnOpenArtifact)
		})
	}

	switch m.Status {
	case contract.MessageStreaming:
		ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
			ui.Box(c).Width(6).Height(6).Radius(3).Background(t.Accent)
			ui.Text(c, "正在生成").FontSize(11).TextColor(t.TextMuted)
			if p.OnStop != nil && TextButton(c, "停止生成", false).Clicked() {
				p.OnStop()
			}
		})
	case contract.MessageFailed:
		errText := m.Error
		if errText == "" {
			errText = "生成失败，请稍后重试"
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
			ui.Text(c, errText).FontSize(11).TextColor(t.Danger).MaxLines(2)
			if p.OnRetry != nil && TextButton(c, "重试", false).Clicked() {
				p.OnRetry()
			}
		})
	case contract.MessageStopped:
		ui.Text(c, "已停止生成").FontSize(11).TextColor(t.TextMuted).Margin(8, 0, 0, 0)
	}
}

func (p AgentMessageProps) stepOpen(index int) *bool {
	if p.StepOpen == nil {
		return nil
	}
	return p.StepOpen(index)
}

// artifactGenerating 判断当前是否有正在写文件的工具调用；
// 这里用工具 ID 常量而不是 import 基础设施层，保持 UI 只依赖 contract。
func artifactGenerating(steps []AgentStep) bool {
	for _, step := range steps {
		if step.Kind != AgentStepTool || step.Tool.Status != "running" {
			continue
		}
		switch lastSegment(step.Tool.ToolID) {
		case "builtin_save_artifact", "builtin_write_file", "builtin_edit_file":
			return true
		}
	}
	return false
}

// lastSegment 取工具 ID 的最后一段，兼容插件工具的 plugin:<id>/<name> 形式。
func lastSegment(id string) string {
	if index := strings.LastIndex(id, "/"); index >= 0 {
		return id[index+1:]
	}
	return id
}

// thinkingStep 是可折叠的思维链块：左侧品牌色细线，默认展开。
func thinkingStep(c *ui.Context, text string, open *bool) {
	if strings.TrimSpace(text) == "" {
		return
	}
	t := c.Theme()

	ui.Column(c).FillWidth().Gap(4).Children(func() {
		header := ui.ButtonBase(c).
			FillWidth().
			Height(22).
			PaddingX(2).
			Radius(theme.RadiusXS).
			AlignItems(ui.Center).
			Justify(ui.Start).
			Gap(4)
		if header.Hovered() {
			header.Background(t.SurfaceHover)
		}
		header.Children(func() {
			Icon(c, IconSparkles, 12).TextColor(t.TextMuted)
			ui.Text(c, "思考").FontSize(11).TextColor(t.TextMuted)
			if open != nil {
				Icon(c, IconChevron, 10).Rotate(90).TextColor(t.TextMuted)
			}
		})
		if open != nil && header.Clicked() {
			*open = !*open
		}

		if open == nil || *open {
			ui.Box(c).
				FillWidth().
				Padding(8, 10).
				Radius(theme.RadiusSM).
				Background(t.SurfaceHover).
				BorderWidth(0, 0, 0, 2).
				BorderColor(t.Accent.Alpha(0.35)).
				Children(func() {
					ui.Text(c, text).FontSize(12).TextColor(t.TextMuted)
				})
		}
	})
}

// toolGroup 是历史消息的工具调用分组，默认收起。
func toolGroup(c *ui.Context, calls []contract.ChatToolCall, open *bool, callOpen func(index int) *bool) {
	t := c.Theme()

	ui.Column(c).FillWidth().Gap(6).Children(func() {
		header := ui.ButtonBase(c).
			FillWidth().
			Height(22).
			PaddingX(2).
			Radius(theme.RadiusXS).
			AlignItems(ui.Center).
			Justify(ui.Start).
			Gap(4)
		if header.Hovered() {
			header.Background(t.SurfaceHover)
		}
		header.Children(func() {
			Icon(c, IconWrench, 12).TextColor(t.TextMuted)
			ui.Text(c, fmt.Sprintf("工具调用 %d 次", len(calls))).FontSize(11).TextColor(t.TextMuted)
			if open != nil {
				Icon(c, IconChevron, 10).Rotate(90).TextColor(t.TextMuted)
			}
		})
		if open != nil && header.Clicked() {
			*open = !*open
		}

		if open == nil || *open {
			for index, call := range calls {
				var detail *bool
				if callOpen != nil {
					detail = callOpen(index)
				}
				toolStep(c, call, detail)
			}
		}
	})
}

// toolStep 是可折叠的工具调用：默认只显示工具名与状态，展开后显示参数与结果。
func toolStep(c *ui.Context, call contract.ChatToolCall, open *bool) {
	t := c.Theme()

	color := t.TextMuted
	switch call.Status {
	case "running":
		color = t.Accent
	case "ok":
		color = t.Success
	case "failed":
		color = t.Danger
	case "rejected":
		color = t.Warning
	}

	arguments := strings.TrimSpace(call.Arguments)
	result := strings.TrimSpace(call.Result)
	errText := strings.TrimSpace(call.Error)
	hasDetail := arguments != "" || result != "" || errText != ""

	ui.Column(c).FillWidth().Gap(4).Padding(2, 0).Children(func() {
		header := ui.ButtonBase(c).
			FillWidth().
			Height(22).
			PaddingX(2).
			Radius(theme.RadiusXS).
			AlignItems(ui.Center).
			Justify(ui.Start).
			Gap(4)
		if header.Hovered() {
			header.Background(t.SurfaceHover)
		}
		header.Children(func() {
			Icon(c, IconWrench, 12).TextColor(color)
			ui.Text(c, toolCallName(call.ToolID)).FontSize(12).Bold().MaxLines(1)
			ui.Text(c, toolCallStatus(call.Status)).FontSize(11).TextColor(color)
			if open != nil && hasDetail {
				angle := float32(0)
				if *open {
					angle = 90
				}
				Icon(c, IconChevron, 10).Rotate(angle).TextColor(t.TextMuted)
			}
		})
		if open != nil && hasDetail && header.Clicked() {
			*open = !*open
		}

		if !hasDetail || (open != nil && !*open) {
			return
		}
		ui.Box(c).
			FillWidth().
			Padding(8, 10).
			Radius(theme.RadiusSM).
			Background(t.SurfaceHover).
			BorderWidth(0, 0, 0, 2).
			BorderColor(t.Accent.Alpha(0.35)).
			Children(func() {
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					if arguments != "" {
						ui.Text(c, arguments).FontSize(11).TextColor(t.TextMuted).MaxLines(2)
					}
					if result != "" {
						ui.Text(c, result).FontSize(11).TextColor(t.TextMuted).MaxLines(3)
					} else if errText != "" {
						ui.Text(c, errText).FontSize(11).TextColor(t.Danger).MaxLines(2)
					}
				})
			})
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
