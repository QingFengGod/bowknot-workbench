package artifacts

import (
	"fmt"
	"strings"

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

	viewer             components.ArtifactViewer
	viewerOpen         bool
	artifactDeleteOpen bool
	errText            string
}

func New(svc contract.ArtifactService, agents contract.AgentService, hooks Hooks) *Page {
	return &Page{svc: svc, agents: agents, hooks: hooks, ownerLabel: "全部来源"}
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
			ui.Text(c, "产物").FontSize(18).Bold()
			ui.Text(c, fmt.Sprintf("共 %d 个交付物", len(p.items))).FontSize(11).TextColor(t.TextMuted)
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			search := ui.TextInput(c, &p.search).
				Placeholder("搜索产物标题").
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
			Title: "还没有产物",
			Hint:  "员工产出文档、图片或链接后，会自动出现在这里。",
		})
		return
	}

	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			for _, detail := range p.items {
				item := detail
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
					OnOpen:   func() { p.openArtifact(item.ID) },
				})
			}
		})
	})
}

func (p *Page) openArtifact(id string) {
	if p.svc == nil || id == "" {
		return
	}
	p.viewer.Open(p.svc, id)
	p.viewerOpen = true
}

func (p *Page) artifactDrawer(c *ui.Context) {
	components.ArtifactDrawer(c, components.ArtifactDrawerProps{
		Open:   &p.viewerOpen,
		Viewer: p.viewer,
		OnDelete: func() {
			p.artifactDeleteOpen = true
		},
	})
	components.ArtifactDeleteDialog(c, &p.artifactDeleteOpen, p.viewer.Detail.Title, func() {
		p.deleteArtifact(p.viewer.Detail.ID)
	})
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
			p.viewerOpen = false
			p.viewer = components.ArtifactViewer{}
			p.reload(true)
		})
	}()
}
