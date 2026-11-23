package components

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
)

// SessionArtifacts 汇总会话内消息的产物，新的在前并按 ID 去重。
func SessionArtifacts(messages []contract.ChatMessage) []contract.ArtifactBrief {
	seen := make(map[string]bool)
	items := make([]contract.ArtifactBrief, 0)
	for index := len(messages) - 1; index >= 0; index-- {
		for _, artifact := range messages[index].Artifacts {
			if artifact.ID == "" || seen[artifact.ID] {
				continue
			}
			seen[artifact.ID] = true
			items = append(items, artifact)
		}
	}
	return items
}

// SessionFilesButton 是标题栏里的会话文件入口，返回是否点击。
func SessionFilesButton(c *ui.Context, count int) bool {
	t := c.Theme()

	button := ui.ButtonBase(c).
		Width(22).
		Height(22).
		Radius(4).
		AlignItems(ui.Center).
		Justify(ui.Center).
		TextColor(t.TextMuted)
	if button.Hovered() {
		button.Background(t.SurfacePressed).TextColor(t.Text)
	}
	button.Children(func() {
		Icon(c, IconArtifact, 14)
	})
	label := "会话文件"
	if count > 0 {
		label = fmt.Sprintf("会话文件（%d）", count)
	}
	return button.Tooltip(label).Clicked()
}

// SessionFilesDrawer 把当前会话的文件收在一处，点击后交给文件预览。
func SessionFilesDrawer(c *ui.Context, open *bool, artifacts []contract.ArtifactBrief, onOpen func(id string)) {
	if open == nil {
		return
	}

	Drawer(c, DrawerProps{
		Open:  open,
		Title: "会话文件",
		Width: 440,
	}, func() {
		if len(artifacts) == 0 {
			EmptyState(c, EmptyProps{
				Icon:  IconArtifact,
				Title: "还没有文件",
				Hint:  "Agent 写入文件后，会在这里汇总。",
			})
			return
		}
		for _, artifact := range artifacts {
			item := artifact
			ArtifactCard(c, ArtifactCardProps{
				Artifact: item,
				OnOpen: func() {
					*open = false
					if onOpen != nil {
						onOpen(item.ID)
					}
				},
			})
		}
	})
}
