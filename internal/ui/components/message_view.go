package components

import (
	"path"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/format"
)

// artifactLinkScheme 是正文里指向产物的内部链接协议。
const artifactLinkScheme = "artifact://"

// messageMarkdown 为一条消息构建正文渲染配置：
// 内联代码里的产物文件名变成可点击链接，点击后打开产物预览。
// 只在消息自带产物里匹配，避免模型写出不存在的路径。
func messageMarkdown(c *ui.Context, m contract.ChatMessage, onOpenArtifact func(id string)) *markdown.Options {
	options := &markdown.Options{Theme: &markdown.Theme{LineHeight: 1.7}}
	if len(m.Artifacts) == 0 || onOpenArtifact == nil {
		return options
	}

	byName := make(map[string]contract.ArtifactBrief, len(m.Artifacts)*2)
	byID := make(map[string]contract.ArtifactBrief, len(m.Artifacts))
	for _, artifact := range m.Artifacts {
		byID[artifact.ID] = artifact
		for _, name := range []string{artifact.Title, artifact.Filename} {
			key := artifactNameKey(name)
			if key == "" {
				continue
			}
			if _, exists := byName[key]; !exists {
				byName[key] = artifact
			}
		}
	}

	options.CodeLink = func(code string) (string, bool) {
		artifact, ok := byName[artifactNameKey(code)]
		if !ok {
			return "", false
		}
		return artifactLinkScheme + artifact.ID, true
	}
	options.OnLink = func(url string) {
		if id, ok := strings.CutPrefix(url, artifactLinkScheme); ok {
			onOpenArtifact(id)
			return
		}
		// 只把明确的站外链接交给系统，相对路径与标题锚点保持原样不处理。
		if isExternalLink(url) {
			c.OpenURL(url)
		}
	}
	options.Preview = func(ctx *ui.Context, dest string) func(tip ui.Element) {
		id, ok := strings.CutPrefix(dest, artifactLinkScheme)
		if !ok {
			return nil
		}
		label := "预览文件"
		if artifact, exists := byID[id]; exists && artifact.Title != "" {
			label = "预览：" + artifact.Title
		}
		return func(tip ui.Element) {
			tip.Padding(8).Children(func() {
				ui.Text(ctx, label).FontSize(11)
			})
		}
	}
	return options
}

// isExternalLink 判断链接是否该交给系统打开。
func isExternalLink(url string) bool {
	lower := strings.ToLower(url)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "file://")
}

// artifactNameKey 归一化正文里的文件名，兼容路径、引号与 :行号 定位。
func artifactNameKey(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "`\"'")
	name = strings.ReplaceAll(name, "\\", "/")
	if index := strings.LastIndex(name, ":"); index > 0 {
		if _, err := strconv.Atoi(name[index+1:]); err == nil {
			name = name[:index]
		}
	}
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return strings.ToLower(path.Base(name))
}

// MessageViewProps 描述一条对话消息的渲染。
type MessageViewProps struct {
	Message contract.ChatMessage
	// AvatarText 是助手头像里的文字，用户消息不显示。
	AvatarText string
	// OnOpenArtifact 打开正文文件引用指向的产物预览。
	OnOpenArtifact func(id string)
	// Images 是用户消息里的图片附件缩略图，由页面按需解码后传入。
	Images []MessageImage
	// ShowRetry 控制「重答」入口，通常只给最后一条已完成的助手回复。
	ShowRetry bool
	// Steps 是实时累积的思考与工具调用，仅当前会话内有效。
	Steps []AgentStep
	// StepOpen 返回时间线步骤的折叠状态指针，由页面保证地址稳定。
	StepOpen func(index int) *bool
	// ToolOpen 返回历史消息里各工具调用的折叠状态指针。
	ToolOpen func(index int) *bool
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
			Images:   p.Images,
			OnOpen:   p.OnOpenArtifact,
			OnDelete: p.OnDelete,
		})
	}

	return AgentMessage(c, AgentMessageProps{
		Message:        p.Message,
		AvatarText:     p.AvatarText,
		OnOpenArtifact: p.OnOpenArtifact,
		Steps:          p.Steps,
		StepOpen:       p.StepOpen,
		ToolOpen:       p.ToolOpen,
		ThinkingOpen:   p.ThinkingOpen,
		ToolsOpen:      p.ToolsOpen,
		ShowRetry:      p.ShowRetry,
		OnStop:         p.OnStop,
		OnRetry:        p.OnRetry,
		OnDelete:       p.OnDelete,
		OnMemory:       p.OnMemory,
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
