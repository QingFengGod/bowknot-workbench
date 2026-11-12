package aigc

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/components"
)

// navItem 是 AIGC 侧边栏的一个菜单项，path 同时是桌面端内部的选中标识。
type navItem struct {
	Path  string
	Label string
	Icon  components.IconName
}

type navGroup struct {
	Label string
	Items []navItem
}

// 菜单结构：生成中心 / 素材管理 / 角色管理 / 剧本创作 / 无限画布 + 资源库。
var navGroups = []navGroup{
	{
		Items: []navItem{
			{Path: "/aigc/generate", Label: "生成中心", Icon: components.IconSparkles},
			{Path: "/aigc/assets", Label: "素材管理", Icon: components.IconImage},
			{Path: "/aigc/characters", Label: "角色管理", Icon: components.IconUser},
			{Path: "/aigc/scripts", Label: "剧本创作", Icon: components.IconFileText},
			{Path: "/aigc/canvas", Label: "无限画布", Icon: components.IconDashboard},
		},
	},
	{
		Label: "资源库",
		Items: []navItem{
			{Path: "/aigc/prompts", Label: "提示词库", Icon: components.IconSparkles},
			{Path: "/aigc/poses", Label: "动作库", Icon: components.IconPerson},
		},
	},
}

// Page 是 AIGC 页的原生实现：本期只做左侧菜单，右侧以占位呈现。
// 子页面切换在页内维护，不经过壳层路由。
type Page struct {
	active string
	// activeTaskCount 对齐 Web 端的任务角标，任务服务接入前恒为 0。
	activeTaskCount int
}

// New 创建 AIGC 页，默认进入生成中心。
func New() *Page {
	return &Page{active: "/aigc/generate"}
}

func (p *Page) View(c *ui.Context) {
	ui.Row(c).Fill().Gap(12).Children(func() {
		p.sidebar(c)
		p.placeholder(c)
	})
}

func (p *Page) sidebar(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).
		Width(200).
		FillHeight().
		Shrink(0).
		Background(t.Surface).
		Radius(10).
		BorderWidth(1).
		BorderColor(t.Border).
		Children(func() {
			// 头部：品牌图标 + 标题，与 Web 端一致。
			ui.Row(c).
				FillWidth().
				Shrink(0).
				Padding(10, 12).
				Gap(8).
				AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).
				BorderColor(t.Border).
				Children(func() {
					ui.Box(c).
						Width(32).
						Height(32).
						Radius(10).
						Background(t.Accent.Alpha(0.1)).
						TextColor(t.Accent).
						AlignItems(ui.Center).
						Justify(ui.Center).
						Children(func() {
							components.Icon(c, components.IconSparkles, 17)
						})
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, "AIGC").FontSize(14).Bold()
						ui.Text(c, "创作工作台").FontSize(11).TextColor(t.TextMuted)
					})
				})

			ui.Column(c).Grow(1).FillWidth().Padding(6).Gap(2).Children(func() {
				for _, group := range navGroups {
					if group.Label != "" {
						ui.Text(c, group.Label).FontSize(11).TextColor(t.TextMuted).Padding(8, 10, 4, 10)
					}
					for _, item := range group.Items {
						p.navRow(c, item)
					}
				}
			})

			// 底部任务入口，角标在任务服务接入前不显示。
			ui.Row(c).
				FillWidth().
				Shrink(0).
				Padding(6).
				BorderWidth(1, 0, 0, 0).
				BorderColor(t.Border).
				Children(func() {
					p.navRow(c, navItem{Path: "tasks", Label: "任务", Icon: components.IconTasks})
				})
		})
}

func (p *Page) navRow(c *ui.Context, item navItem) {
	t := c.Theme()
	active := item.Path == p.active

	btn := ui.ButtonBase(c).
		FillWidth().
		Height(32).
		PaddingX(10).
		Radius(8).
		Gap(8).
		AlignItems(ui.Center).
		FontSize(13)

	switch {
	case active:
		btn.Background(t.Accent.Alpha(0.1)).TextColor(t.Accent)
	case btn.Hovered():
		btn.Background(t.SurfaceHover).TextColor(t.Text)
	default:
		btn.TextColor(t.TextMuted)
	}

	btn.Children(func() {
		components.Icon(c, item.Icon, 16)
		label := ui.Text(c, item.Label).FontSize(13).MaxLines(1).Grow(1)
		if active {
			label.Bold()
		}

		// 任务角标：仅在有进行中任务时显示。
		if item.Path == "tasks" && p.activeTaskCount > 0 {
			ui.Box(c).
				MinWidth(18).
				Height(18).
				Radius(9).
				Background(t.Accent).
				PaddingX(5).
				AlignItems(ui.Center).
				Justify(ui.Center).
				Children(func() {
					ui.Text(c, fmt.Sprint(p.activeTaskCount)).FontSize(10).TextColor(t.AccentText)
				})
		}
	})

	if btn.Clicked() {
		p.active = item.Path
	}
}

// placeholder 是右侧内容占位，与 Web 端 ModulePlaceholder 视觉一致。
func (p *Page) placeholder(c *ui.Context) {
	t := c.Theme()

	title, icon := p.currentMeta()
	ui.Column(c).Grow(1).FillHeight().AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
		ui.Box(c).
			Width(80).
			Height(80).
			Radius(40).
			BorderWidth(1).
			BorderColor(t.Accent).
			TextColor(t.Accent).
			AlignItems(ui.Center).
			Justify(ui.Center).
			Children(func() {
				components.Icon(c, icon, 36)
			})
		ui.Text(c, title).FontSize(18).Bold()
		ui.Text(c, "该模块正在逐步实现，敬请期待。").
			FontSize(12).
			TextColor(t.TextMuted).
			MaxWidth(420).
			TextAlign(ui.Center)
	})
}

// currentMeta 返回选中菜单的标题与图标，任务入口复用任务图标。
func (p *Page) currentMeta() (string, components.IconName) {
	for _, group := range navGroups {
		for _, item := range group.Items {
			if item.Path == p.active {
				return item.Label, item.Icon
			}
		}
	}
	return "任务", components.IconTasks
}
