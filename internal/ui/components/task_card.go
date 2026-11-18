package components

import (
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/theme"
)

// TaskStatusText 把任务状态转成中文文案。
func TaskStatusText(status contract.TaskStatus) string {
	switch status {
	case contract.TaskQueued:
		return "排队中"
	case contract.TaskRunning:
		return "运行中"
	case contract.TaskDone:
		return "已完成"
	case contract.TaskFailed:
		return "失败"
	case contract.TaskCanceled:
		return "已取消"
	case contract.TaskTimeout:
		return "已超时"
	default:
		return "未知"
	}
}

// TaskStatusTone 返回任务状态对应的标签颜色。
func TaskStatusTone(status contract.TaskStatus) Tone {
	switch status {
	case contract.TaskQueued, contract.TaskRunning:
		return ToneBrand
	case contract.TaskDone:
		return ToneSuccess
	case contract.TaskFailed, contract.TaskTimeout:
		return ToneDanger
	default:
		return ToneNeutral
	}
}

// TaskStatusChip 渲染任务状态标签。
func TaskStatusChip(c *ui.Context, status contract.TaskStatus) {
	Chip(c, TaskStatusText(status), TaskStatusTone(status))
}

// TaskCardProps 描述任务汇报卡片。
type TaskCardProps struct {
	Title    string
	Assignee string
	Status   contract.TaskStatus
	Content  string
	// OnView 打开任务过程；为 nil 时不显示入口。
	OnView func()
	// OnOpenChat 跳转到该员工的直聊会话；为 nil 时不显示入口。
	OnOpenChat func()
	// Artifacts 是任务产出的交付物；为空时不显示。
	Artifacts []contract.ArtifactBrief
	// OnOpenArtifact 打开产物预览。
	OnOpenArtifact func(id string)
	// OnCancel 取消排队中或运行中的任务。
	OnCancel func()
	// OnRetry 重新执行终态任务。
	OnRetry func()
}

// TaskCard 是助手页里的任务汇报卡片：标题、员工、状态、汇报正文与操作。
func TaskCard(c *ui.Context, p TaskCardProps) ui.Element {
	t := c.Theme()

	card := ui.Column(c).
		FillWidth().
		Padding(14).
		Gap(8).
		Radius(theme.Radius).
		Background(t.Surface).
		BorderWidth(1).
		BorderColor(t.Border)

	card.Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, p.Title).FontSize(13).Bold().MaxLines(1).Grow(1)
			TaskStatusChip(c, p.Status)
		})
		if p.Assignee != "" {
			ui.Text(c, "负责人："+p.Assignee).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		}
		if content := p.Content; content != "" {
			ui.Text(c, content).FontSize(13).TextColor(t.Text).MaxLines(12)
		}
		if p.OnOpenArtifact != nil {
			ArtifactList(c, p.Artifacts, p.OnOpenArtifact)
		}

		ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
			if p.OnView != nil {
				if TextButton(c, "查看过程", false).Clicked() {
					p.OnView()
				}
			}
			if p.OnOpenChat != nil {
				if TextButton(c, "打开对话", false).Clicked() {
					p.OnOpenChat()
				}
			}
			if p.OnCancel != nil && (p.Status == contract.TaskQueued || p.Status == contract.TaskRunning) {
				if TextButton(c, "取消", true).Clicked() {
					p.OnCancel()
				}
			}
			if p.OnRetry != nil && p.Status.Terminal() {
				if TextButton(c, "重试", false).Clicked() {
					p.OnRetry()
				}
			}
		})
	})
	return card
}
