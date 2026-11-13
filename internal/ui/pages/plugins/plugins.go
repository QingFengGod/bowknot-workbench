package plugins

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/format"
)

// Page 是插件页的原生实现。
type Page struct {
	svc contract.PluginService

	plugins []contract.Plugin
	agents  []contract.PluginAgent

	query string
	grid  ui.GridState

	detailOpen bool
	detailID   string
	// draft 保存详情弹窗里的授权草稿，按 Agent id 记录。
	draft map[string]bool

	installOpen  bool
	installType  int
	installValue string
	installErr   string

	uninstallOpen bool
	uninstallID   string
}

// New 创建插件页。
func New(svc contract.PluginService) *Page {
	p := &Page{svc: svc}
	p.reload()
	return p
}

func (p *Page) View(c *ui.Context) {
	t := c.Theme()
	pluginCount := len(p.plugins)
	ui.Column(c).Fill().Background(t.Background).Gap(t.Space(3)).Children(func() {
		ui.Row(c).FillWidth().Background(t.Surface).Radius(t.Radius).Padding(t.Space(3)).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(t.Space(3)).Children(func() {
			ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "插件").FontSize(22).Bold()
				if pluginCount >= 0 {
					components.Chip(c, fmt.Sprint(pluginCount), components.ToneNeutral)
				}
				ui.Text(c, "安装插件，把工具按需授权给助理或子 Agent").FontSize(12).TextColor(t.TextMuted)
			})

			ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
				ui.SearchField(c, &p.query).Width(220).Placeholder("搜索插件")
				if components.Button(c, components.ButtonProps{
					Label:   "安装插件",
					Variant: components.ButtonPrimary,
				}).Clicked() {
					p.openInstall()
				}
			})
		})

		list := p.filtered()
		switch {
		case len(p.plugins) == 0:
			components.EmptyState(c, components.EmptyProps{
				Icon:  components.IconPlugins,
				Title: "还没有插件",
				Hint:  "安装一个插件，把它的工具按需授权给助理或子 Agent。",
				Action: func() {
					if components.Button(c, components.ButtonProps{
						Label:   "安装第一个插件",
						Variant: components.ButtonPrimary,
					}).Clicked() {
						p.openInstall()
					}
				},
			})
		case len(list) == 0:
			components.EmptyState(c, components.EmptyProps{
				Icon:  components.IconSearch,
				Title: "没有找到匹配的插件",
				Hint:  "换个关键词试试，或清除搜索。",
				Action: func() {
					if components.Button(c, components.ButtonProps{Label: "清除搜索"}).Clicked() {
						p.query = ""
					}
				},
			})
		default:
			p.gridView(c, list)
		}
	})

	p.detailModal(c)
	p.installModal(c)
	p.uninstallModal(c)
}

func (p *Page) gridView(c *ui.Context, list []contract.Plugin) {
	p.grid.Key = func(i int) any { return list[i].ID }
	ui.GridView(c, &p.grid, len(list), 260, 116, func(i int) {
		p.pluginCard(c, list[i])
	}).Grow(1)
}

func (p *Page) pluginCard(c *ui.Context, plugin contract.Plugin) {
	t := c.Theme()

	card := ui.Row(c).
		Fill().
		Padding(14).
		Gap(12).
		Radius(12).
		Background(t.Surface).
		BorderWidth(1).
		AlignItems(ui.Center)

	if card.Hovered() {
		card.BorderColor(t.Accent)
	} else {
		card.BorderColor(t.Border)
	}

	card.Children(func() {
		components.Avatar(c, plugin.IconText, 42)

		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Text(c, plugin.Name).FontSize(14).Bold().MaxLines(1)
			ui.Text(c, orDefault(plugin.Description, "暂无描述")).
				FontSize(12).
				TextColor(t.TextMuted).
				MaxLines(1)

			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, fmt.Sprintf("v%s · %d 个工具", plugin.Version, len(plugin.Tools))).
					FontSize(11).
					TextColor(t.TextMuted).
					MaxLines(1)
				ui.Box(c).Grow(1)
				if n := len(plugin.AgentIDs); n > 0 {
					components.Chip(c, fmt.Sprintf("已授权 %d", n), components.ToneBrand)
				} else {
					components.Chip(c, "未授权", components.ToneNeutral)
				}
			})
		})
	})

	if card.Clicked() {
		p.openDetail(plugin.ID)
	}
}

func (p *Page) detailModal(c *ui.Context) {
	plugin := p.pluginByID(p.detailID)
	if plugin == nil {
		return
	}
	t := c.Theme()
	pl := *plugin

	components.Drawer(c, components.DrawerProps{
		Open:  &p.detailOpen,
		Title: "插件详情",
		Width: 480,
		Footer: func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
				if components.Button(c, components.ButtonProps{
					Label:   "卸载",
					Variant: components.ButtonDanger,
				}).Clicked() {
					p.detailOpen = false
					p.uninstallID = pl.ID
					p.uninstallOpen = true
				}
				if components.Button(c, components.ButtonProps{
					Label:   "关闭",
					Variant: components.ButtonSecondary,
				}).Clicked() {
					p.detailOpen = false
				}
			})
		},
	}, func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			components.Avatar(c, pl.IconText, 48)
			ui.Column(c).Gap(2).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, pl.Name).FontSize(18).Bold()
					components.Chip(c, "v"+pl.Version, components.ToneNeutral)
				})
				ui.Text(c, pl.ID).FontSize(11).TextColor(t.TextMuted)
			})
		})

		ui.Text(c, orDefault(pl.Description, "暂无描述")).FontSize(13).TextColor(t.TextMuted)

		components.InfoRow(c, "来源", sourceLabel(pl.SourceType))
		components.InfoRow(c, "安装时间", format.Time(pl.InstalledAt))
		components.InfoRow(c, "工具数量", fmt.Sprintf("%d 个", len(pl.Tools)))

		ui.Divider(c)
		components.SectionTitle(c, "工具清单", fmt.Sprintf("%d 个", len(pl.Tools)))
		if len(pl.Tools) == 0 {
			ui.Text(c, "该插件不提供工具").FontSize(12).TextColor(t.TextMuted)
		}
		for _, tool := range pl.Tools {
			ui.Column(c).Gap(2).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, tool.Name).FontSize(12).Bold()
					if tool.Dangerous {
						components.Chip(c, "执行前确认", components.ToneDanger)
					}
				})
				ui.Text(c, tool.Description).FontSize(12).TextColor(t.TextMuted)
			})
		}

		ui.Divider(c)
		components.SectionTitle(c, "可用 Agent", fmt.Sprintf("已授权 %d/%d", len(pl.AgentIDs), len(p.agents)))
		ui.Text(c, "插件按整体授权，勾选后其包含的全部工具都对该 Agent 可用。").
			FontSize(12).
			TextColor(t.TextMuted)
		for _, agent := range p.agents {
			p.agentRow(c, pl.ID, agent)
		}
		if len(p.agents) <= 1 {
			ui.Text(c, "还没有子 Agent，创建后可在这里授权。").FontSize(12).TextColor(t.TextMuted)
		}
	})
}

func (p *Page) agentRow(c *ui.Context, pluginID string, agent contract.PluginAgent) {
	t := c.Theme()

	label := agent.Name
	if agent.IsMain {
		label += "（主 Agent）"
	}

	checked := p.draft[agent.ID]
	box := ui.Checkbox(c, &checked, label)
	if box.Changed() {
		p.setAuthorized(pluginID, agent.ID, checked)
	}

	ui.Text(c, agent.Description).FontSize(11).TextColor(t.TextMuted).PaddingStart(26)
}

func (p *Page) installModal(c *ui.Context) {
	t := c.Theme()

	placeholder := "例如 D:\\bowknot-plugins\\my-plugin"
	if p.installType == 1 {
		placeholder = "例如 https://plugins.bowknot.dev/my-plugin"
	}

	components.Modal(c, components.ModalProps{
		Open:  &p.installOpen,
		Title: "安装插件",
		Width: 520,
		Footer: func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.installOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:   "安装",
				Variant: components.ButtonPrimary,
			}).Clicked() {
				p.install()
			}
		},
	}, func() {
		ui.Text(c, "支持本地插件包目录或远程插件包地址，安装后可在这里授权给 Agent。").
			FontSize(12).
			TextColor(t.TextMuted)

		ui.Tabs(c, &p.installType, "本地插件包", "远程地址")
		ui.TextInput(c, &p.installValue).Placeholder(placeholder)
		if p.installErr != "" {
			ui.Text(c, p.installErr).FontSize(12).TextColor(t.Danger)
		}
	})
}

func (p *Page) uninstallModal(c *ui.Context) {
	plugin := p.pluginByID(p.uninstallID)
	t := c.Theme()

	var name string
	if plugin != nil {
		name = plugin.Name
	}

	// 关闭状态下也要构建，动画锚点才能停在 0，首次打开才有过渡。
	props := components.ModalProps{
		Open:  &p.uninstallOpen,
		Title: "卸载插件",
		Width: 420,
	}
	if plugin != nil {
		props.Footer = func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.uninstallOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:   "确认卸载",
				Variant: components.ButtonDanger,
			}).Clicked() {
				p.uninstall()
			}
		}
	}

	components.Modal(c, props, func() {
		if plugin == nil {
			return
		}
		ui.Text(c, fmt.Sprintf("确定卸载「%s」吗？卸载后所有 Agent 将无法再调用该插件的工具。", name)).
			FontSize(13).
			TextColor(t.TextMuted)
	})
}

func (p *Page) openDetail(id string) {
	plugin := p.pluginByID(id)
	if plugin == nil {
		return
	}
	p.detailID = id
	p.draft = make(map[string]bool, len(plugin.AgentIDs))
	for _, agentID := range plugin.AgentIDs {
		p.draft[agentID] = true
	}
	p.detailOpen = true
}

func (p *Page) openInstall() {
	p.installValue = ""
	p.installErr = ""
	p.installOpen = true
}

func (p *Page) install() {
	if p.svc == nil {
		return
	}
	kind := "local"
	if p.installType == 1 {
		kind = "remote"
	}
	if err := p.svc.Install(contract.InstallSource{Type: kind, Value: p.installValue}); err != nil {
		p.installErr = err.Error()
		return
	}
	p.installErr = ""
	p.installOpen = false
	p.reload()
}

func (p *Page) uninstall() {
	if p.svc == nil {
		return
	}
	if err := p.svc.Uninstall(p.uninstallID); err != nil {
		return
	}
	if p.detailID == p.uninstallID {
		p.detailOpen = false
		p.detailID = ""
	}
	p.uninstallOpen = false
	p.uninstallID = ""
	p.reload()
}

func (p *Page) setAuthorized(pluginID, agentID string, on bool) {
	if on {
		p.draft[agentID] = true
	} else {
		delete(p.draft, agentID)
	}
	if p.svc == nil {
		return
	}
	ids := make([]string, 0, len(p.draft))
	for _, agent := range p.agents {
		if p.draft[agent.ID] {
			ids = append(ids, agent.ID)
		}
	}
	_ = p.svc.SetAuthorizedAgents(pluginID, ids)
	p.reload()
}

func (p *Page) reload() {
	if p.svc == nil {
		p.plugins = nil
		p.agents = nil
		return
	}
	p.plugins = p.svc.Plugins()
	p.agents = p.svc.Agents()
}

func (p *Page) filtered() []contract.Plugin {
	text := strings.ToLower(strings.TrimSpace(p.query))
	if text == "" {
		return p.plugins
	}
	out := make([]contract.Plugin, 0, len(p.plugins))
	for _, plugin := range p.plugins {
		if strings.Contains(strings.ToLower(plugin.Name), text) ||
			strings.Contains(strings.ToLower(plugin.Description), text) {
			out = append(out, plugin)
		}
	}
	return out
}

func (p *Page) pluginByID(id string) *contract.Plugin {
	if id == "" {
		return nil
	}
	for i := range p.plugins {
		if p.plugins[i].ID == id {
			return &p.plugins[i]
		}
	}
	return nil
}

func sourceLabel(sourceType string) string {
	if sourceType == "remote" {
		return "远程地址"
	}
	return "本地插件包"
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
