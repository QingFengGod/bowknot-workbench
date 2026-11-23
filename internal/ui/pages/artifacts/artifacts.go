package artifacts

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/format"
	"bowknot-workbench/internal/ui/theme"
)

// Hooks 是页面依赖的外部动作，由壳层注入。
type Hooks struct {
	// Update 在主线程修改页面状态并触发重绘。
	Update func(fn func())
}

type kindOption struct {
	Label string
	Kinds []contract.ArtifactKind
}

var kindOptions = []kindOption{
	{Label: "全部"},
	{Label: "文档", Kinds: []contract.ArtifactKind{
		contract.ArtifactMarkdown, contract.ArtifactText, contract.ArtifactCode, contract.ArtifactHTML,
	}},
	{Label: "图片", Kinds: []contract.ArtifactKind{contract.ArtifactImage}},
	{Label: "链接", Kinds: []contract.ArtifactKind{contract.ArtifactLink}},
	{Label: "其它", Kinds: []contract.ArtifactKind{contract.ArtifactFile}},
}

// Page 是产物一级页面：筛选、列表与预览。
type Page struct {
	svc    contract.ArtifactService
	agents contract.AgentService
	hooks  Hooks

	items      []contract.ArtifactDetail
	owners     []contract.AgentInfo
	kindIndex  int
	ownerID    string
	ownerLabel string
	search     string

	lastRevision uint64
	ownerOpen    bool

	// 文件预览：抽屉状态与删除确认统一由 ArtifactHost 管理。
	artifactHost *components.ArtifactHost
	// grid 是文件卡片的网格布局状态，宽屏时自动排成多列。
	grid    ui.GridState
	errText string
}

func New(svc contract.ArtifactService, agents contract.AgentService, hooks Hooks) *Page {
	return &Page{
		svc: svc, agents: agents, hooks: hooks, ownerLabel: "全部来源",
		artifactHost: components.NewArtifactHost(svc),
	}
}

// OnShow 在切换到本页时刷新来源列表与产物。
func (p *Page) OnShow() {
	p.reloadOwners()
	p.reload(true)
}

func (p *Page) reloadOwners() {
	if p.agents == nil {
		p.owners = nil
		return
	}
	p.owners = p.agents.Agents()
}

func (p *Page) reload(force bool) {
	if p.svc == nil {
		p.items = nil
		return
	}
	revision := p.svc.ArtifactRevision()
	if !force && revision == p.lastRevision {
		return
	}
	p.lastRevision = revision

	option := kindOptions[p.kindIndex]
	p.items = p.svc.Artifacts(contract.ArtifactFilter{
		Kinds:   option.Kinds,
		OwnerID: p.ownerID,
		Search:  strings.TrimSpace(p.search),
		Limit:   200,
	})
}

func (p *Page) View(c *ui.Context) {
	t := c.Theme()
	p.reload(false)

	root := ui.Column(c).
		FillWidth().
		FillHeight().
		Padding(t.Space(3)).
		Gap(t.Space(3)).
		Background(t.Surface).
		Radius(theme.Radius).
		Clip()
	theme.NormalShadow.Apply(c, root)

	root.Children(func() {
		p.header(c)
		if p.errText != "" {
			ui.Text(c, p.errText).FontSize(12).TextColor(t.Danger)
		}
		p.filters(c)
		p.list(c)
	})

	p.artifactDrawer(c)
}

func (p *Page) header(c *ui.Context) {
	t := c.Theme()

	ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, "文件库").FontSize(18).Bold()
			ui.Text(c, fmt.Sprintf("共 %d 个文件", len(p.items))).FontSize(11).TextColor(t.TextMuted)
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			search := ui.TextInput(c, &p.search).
				Placeholder("搜索文件名").
				Width(220).
				MaxLength(60)
			if search.Changed() {
				p.reload(true)
			}
			p.ownerFilter(c)
		})
	})
}

func (p *Page) filters(c *ui.Context) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		for index, option := range kindOptions {
			selected := index == p.kindIndex
			t := c.Theme()
			btn := ui.ButtonBase(c).
				Height(26).
				PaddingX(10).
				Radius(999).
				AlignItems(ui.Center).
				Justify(ui.Center)
			if selected {
				btn.Background(t.Accent.Alpha(0.12)).TextColor(t.Accent)
			} else {
				if btn.Hovered() {
					btn.Background(t.SurfaceHover)
				}
				btn.TextColor(t.TextMuted)
			}
			label := option.Label
			btn.Children(func() {
				ui.Text(c, label).FontSize(12)
			})
			if btn.Clicked() && !selected {
				p.kindIndex = index
				p.reload(true)
			}
		}
	})
}

func (p *Page) ownerFilter(c *ui.Context) {
	t := c.Theme()

	trigger := ui.ButtonBase(c).
		Height(30).
		PaddingX(10).
		Radius(theme.RadiusSM).
		BorderWidth(1).
		BorderColor(t.Border).
		Background(t.Surface).
		AlignItems(ui.Center).
		Gap(6)
	if trigger.Hovered() {
		trigger.Background(t.SurfaceHover)
	}
	trigger.Children(func() {
		ui.Text(c, p.ownerLabel).FontSize(12).TextColor(t.Text).MaxLines(1)
		components.Icon(c, components.IconChevron, 11).TextColor(t.TextMuted)
	})
	if trigger.Clicked() {
		p.ownerOpen = true
	}

	components.Popover(c, trigger, &p.ownerOpen, func() {
		p.ownerOption(c, "", "全部来源")
		p.ownerOption(c, "main", "主助手")
		for _, agent := range p.owners {
			p.ownerOption(c, agent.ID, agent.Name)
		}
	})
}

func (p *Page) ownerOption(c *ui.Context, id, label string) {
	components.PopoverItem(c, components.PopoverItemProps{
		Label:    label,
		Selected: p.ownerID == id,
		OnClick: func() {
			p.ownerID = id
			p.ownerLabel = label
			p.ownerOpen = false
			p.reload(true)
		},
		Content: func(c *ui.Context, selected bool) {
			color := c.Theme().Text
			if selected {
				color = c.Theme().Accent
			}
			ui.Text(c, label).FontSize(13).TextColor(color).MaxLines(1)
		},
	})
}

func (p *Page) list(c *ui.Context) {
	if len(p.items) == 0 {
		components.EmptyState(c, components.EmptyProps{
			Icon:  components.IconArtifact,
			Title: "文件库还是空的",
			Hint:  "Agent 写入工作区的文件会自动出现在这里。",
		})
		return
	}

	// 宽屏时排成多列，避免一行只有一个文件显得空旷。
	p.grid.Key = func(i int) any { return p.items[i].ID }
	ui.GridView(c, &p.grid, len(p.items), 320, 56, func(i int) {
		item := p.items[i]
		meta := components.ArtifactKindText(item.Kind)
		if item.Size > 0 {
			meta += " · " + format.Size(item.Size)
		}
		if item.SourceName != "" {
			meta += " · " + item.SourceName
		}
		if item.CreatedAt > 0 {
			meta += " · " + format.Time(item.CreatedAt)
		}
		components.ArtifactCard(c, components.ArtifactCardProps{
			Artifact: item.ArtifactBrief,
			Meta:     meta,
			Stretch:  true,
			OnOpen:   func() { p.openArtifact(item.ID) },
			Actions: func(c *ui.Context) bool {
				if !components.TextButton(c, "复制路径", false).Clicked() {
					return false
				}
				target := item.Path
				if item.Kind == contract.ArtifactLink {
					target = item.URI
				}
				mygo.Clipboard.WriteText(target)
				return true
			},
		})
	}).Grow(1)
}

func (p *Page) openArtifact(id string) {
	if p.svc == nil || id == "" {
		return
	}
	p.artifactHost.Open(id)
}

func (p *Page) artifactDrawer(c *ui.Context) {
	p.artifactHost.Render(c, components.ArtifactHostProps{OnDelete: p.deleteArtifact})
}

func (p *Page) deleteArtifact(id string) {
	if p.svc == nil || id == "" {
		return
	}
	go func() {
		err := p.svc.DeleteArtifact(id)
		update := func(fn func()) {
			if p.hooks.Update != nil {
				p.hooks.Update(fn)
				return
			}
			fn()
		}
		update(func() {
			if err != nil {
				p.errText = err.Error()
				return
			}
			p.errText = ""
			p.artifactHost.Close()
			p.reload(true)
		})
	}()
}
