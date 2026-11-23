package components

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/format"
)

// ArtifactViewer 是页面持有的产物预览状态：打开时读取一次文件。
type ArtifactViewer struct {
	Detail contract.ArtifactDetail
	Data   ArtifactPreviewData
}

// Open 加载指定产物并准备预览数据。
func (v *ArtifactViewer) Open(service contract.ArtifactService, id string) {
	if service == nil || id == "" {
		return
	}
	v.Detail = service.Artifact(id)
	v.Data = LoadArtifactPreview(v.Detail)
}

// Reload 重新读取当前产物，用于删除失败或文件状态变化后的刷新。
func (v *ArtifactViewer) Reload(service contract.ArtifactService) {
	if v.Detail.ID == "" {
		return
	}
	v.Open(service, v.Detail.ID)
}

// ArtifactDrawerProps 描述右侧产物预览抽屉。
type ArtifactDrawerProps struct {
	Open   *bool
	Viewer ArtifactViewer
	// OnReveal 跳转到产物来源消息；为 nil 时不显示入口。
	OnReveal func(messageID string)
	// OnDelete 由页面接管确认流程与删除动作。
	OnDelete func()
}

// ArtifactDrawer 渲染文件预览抽屉与操作栏。
func ArtifactDrawer(c *ui.Context, p ArtifactDrawerProps) {
	t := c.Theme()
	detail := p.Viewer.Detail

	Drawer(c, DrawerProps{
		Open:  p.Open,
		Title: "文件预览",
		Width: 660,
		Footer: func() {
			if detail.ID == "" {
				return
			}
			openLabel := "打开"
			if detail.Kind == contract.ArtifactLink {
				openLabel = "在浏览器打开"
			}
			if Button(c, ButtonProps{Label: openLabel, Variant: ButtonSecondary}).Clicked() {
				openArtifact(detail)
			}
			if detail.Kind != contract.ArtifactLink && detail.Path != "" {
				if Button(c, ButtonProps{Label: "在文件夹中显示", Variant: ButtonGhost}).Clicked() {
					mygo.Shell.ShowItemInFolder(detail.Path)
				}
			}
			if Button(c, ButtonProps{Label: "复制路径", Variant: ButtonGhost}).Clicked() {
				target := detail.Path
				if detail.Kind == contract.ArtifactLink {
					target = detail.URI
				}
				if target != "" {
					mygo.Clipboard.WriteText(target)
				}
			}
			if p.OnDelete != nil {
				if Button(c, ButtonProps{Label: "删除", Variant: ButtonDanger}).Clicked() {
					p.OnDelete()
				}
			}
		},
	}, func() {
		if detail.ID == "" {
			EmptyState(c, EmptyProps{
				Icon:  IconArtifact,
				Title: "文件不存在",
				Hint:  "它可能已经被删除或移动。",
			})
			return
		}

		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Text(c, detail.Title).FontSize(16).Bold().MaxLines(3)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				Chip(c, ArtifactKindText(detail.Kind), ToneNeutral)
				if detail.Size > 0 {
					ui.Text(c, format.Size(detail.Size)).FontSize(11).TextColor(t.TextMuted)
				}
				if detail.SourceName != "" {
					ui.Text(c, "来自 "+detail.SourceName).FontSize(11).TextColor(t.TextMuted)
				}
				if detail.CreatedAt > 0 {
					ui.Text(c, format.Time(detail.CreatedAt)).FontSize(11).TextColor(t.TextMuted)
				}
				if detail.MessageID != "" && p.OnReveal != nil {
					if TextButton(c, "回到来源消息", false).Clicked() {
						p.OnReveal(detail.MessageID)
					}
				}
			})
			if detail.Filename != "" {
				ui.Text(c, detail.Filename).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
			}
			if detail.Summary != "" {
				ui.Text(c, detail.Summary).FontSize(12).TextColor(t.TextMuted)
			}
		})

		ArtifactPreview(c, detail, p.Viewer.Data)
	})
}

// ArtifactDeleteDialog 是删除文件的确认框；外部文件只移除登记记录。
func ArtifactDeleteDialog(c *ui.Context, open *bool, title string, external bool, onConfirm func()) {
	if open == nil {
		return
	}
	message := fmt.Sprintf("确定删除「%s」吗？文件会一并删除，无法恢复。", title)
	if external {
		message = fmt.Sprintf("确定从文件库移除「%s」吗？工作区里的原文件会保留。", title)
	}
	if ui.AlertDialog(c, open, "删除文件", message, "删除", "取消") == 0 {
		onConfirm()
	}
}

func openArtifact(detail contract.ArtifactDetail) {
	if detail.Kind == contract.ArtifactLink {
		if err := mygo.Shell.OpenExternal(detail.URI); err != nil {
			mygo.Dialog.Error("打开链接失败", err.Error())
		}
		return
	}
	if detail.Path == "" {
		return
	}
	if err := mygo.Shell.OpenPath(detail.Path); err != nil {
		mygo.Dialog.Error("打开文件失败", err.Error())
	}
}
