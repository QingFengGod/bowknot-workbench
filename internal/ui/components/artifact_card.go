package components

import (
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/format"
	"bowknot-workbench/internal/ui/theme"
)

// ArtifactKindText 返回产物类型的中文名称。
func ArtifactKindText(kind contract.ArtifactKind) string {
	switch kind {
	case contract.ArtifactMarkdown:
		return "Markdown"
	case contract.ArtifactText:
		return "文本"
	case contract.ArtifactCode:
		return "代码"
	case contract.ArtifactHTML:
		return "网页"
	case contract.ArtifactImage:
		return "图片"
	case contract.ArtifactLink:
		return "链接"
	default:
		return "文件"
	}
}

// ArtifactIcon 返回产物类型对应的图标。
func ArtifactIcon(kind contract.ArtifactKind) IconName {
	switch kind {
	case contract.ArtifactImage:
		return IconImage
	case contract.ArtifactLink:
		return IconLink
	default:
		return IconFileText
	}
}

// ArtifactCardProps 描述消息与任务卡片里的产物行。
type ArtifactCardProps struct {
	Artifact contract.ArtifactBrief
	// Meta 覆盖默认的类型与大小描述，产物页用它补充来源与时间。
	Meta   string
	OnOpen func()
}

// ArtifactCard 是紧凑的产物条目：图标、标题、类型与大小。
func ArtifactCard(c *ui.Context, p ArtifactCardProps) ui.Element {
	t := c.Theme()

	row := ui.ButtonBase(c).
		FillWidth().
		Padding(8).
		Gap(8).
		Radius(theme.RadiusSM).
		BorderWidth(1).
		BorderColor(t.Border).
		Background(t.Surface).
		AlignItems(ui.Center)
	if row.Hovered() {
		row.Background(t.SurfaceHover)
	}

	row.Children(func() {
		Icon(c, ArtifactIcon(p.Artifact.Kind), 15).TextColor(t.Accent)
		ui.Column(c).Gap(1).Grow(1).Children(func() {
			ui.Text(c, p.Artifact.Title).FontSize(12).Bold().MaxLines(1)
			meta := p.Meta
			if meta == "" {
				meta = ArtifactKindText(p.Artifact.Kind)
				if p.Artifact.Size > 0 {
					meta += " · " + format.Size(p.Artifact.Size)
				}
			}
			ui.Text(c, meta).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		})
		if p.Artifact.Available {
			Chip(c, "预览", ToneNeutral)
		} else {
			Chip(c, "文件已丢失", ToneDanger)
		}
	})

	if row.Clicked() && p.OnOpen != nil {
		p.OnOpen()
	}
	return row
}

// ArtifactList 渲染一组产物条目，没有产物时不构建任何元素。
func ArtifactList(c *ui.Context, artifacts []contract.ArtifactBrief, onOpen func(id string)) {
	if len(artifacts) == 0 {
		return
	}
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		for _, artifact := range artifacts {
			item := artifact
			ArtifactCard(c, ArtifactCardProps{
				Artifact: item,
				OnOpen:   func() { onOpen(item.ID) },
			})
		}
	})
}
