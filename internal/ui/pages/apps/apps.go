package apps

import (
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/format"
	"bowknot-workbench/internal/ui/theme"
)

var sortOptions = []string{"最近使用", "名称", "安装时间"}

// Hooks 是页面依赖的外部动作，由壳层注入。
type Hooks struct {
	// Open 在新窗口打开应用。
	Open func(app contract.AppItem)
	// Close 关闭应用窗口。
	Close func(id string)
	// SelectPackage 打开系统文件选择器，返回选中的应用包路径；取消时返回空字符串。
	SelectPackage func() (string, error)
}

// Page 是 Apps 页的原生实现。
type Page struct {
	svc   contract.AppCatalog
	hooks Hooks

	apps      []contract.AppItem
	query     string
	sortLabel string
	grid      ui.GridState

	detailOpen bool
	detailID   string

	uninstallOpen bool
	uninstallID   string
	keepData      bool

	clearOpen bool
	clearID   string

	installOpen  bool
	installType  int
	installValue string
	installErr   string
	preview      *contract.AppPreview
}

// New 创建 Apps 页。
func New(svc contract.AppCatalog, hooks Hooks) *Page {
	p := &Page{
		svc:       svc,
		hooks:     hooks,
		sortLabel: sortOptions[0],
	}
	p.reload()
	return p
}

func (p *Page) View(c *ui.Context) {
	t := c.Theme()
	appCount := len(p.apps)
	ui.Column(c).Fill().Background(t.Background).Gap(t.Space(3)).Children(func() {
		header := ui.Row(c).
			FillWidth().
			Background(t.Surface).
			Radius(theme.Radius).
			Padding(t.Space(3)).
			AlignItems(ui.Center).
			Justify(ui.SpaceBetween).
			Gap(t.Space(3))
		theme.NormalShadow.Apply(c, header)

		header.Children(func() {
			ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Apps").FontSize(22).Bold()
				if appCount >= 0 {
					components.Chip(c, fmt.Sprint(appCount), components.ToneNeutral)
				}
				ui.Text(c, "安装应用，能力自动接入助理").FontSize(12).TextColor(t.TextMuted)
			})

			ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
				ui.SearchField(c, &p.query).Width(220).Placeholder("搜索应用")
				ui.Select(c, &p.sortLabel, sortOptions).Width(116)
				if components.Button(c, components.ButtonProps{
					Label:   "安装应用",
					Variant: components.ButtonPrimary,
				}).Clicked() {
					p.openInstall()
				}
			})
		})

		list := p.visible()
		switch {
		case len(p.apps) == 0:
			components.EmptyState(c, components.EmptyProps{
				Icon:  components.IconApps,
				Title: "还没有应用",
				Hint:  "安装一个应用，它的能力会自动接入助理。",
				Action: func() {
					if components.Button(c, components.ButtonProps{
						Label:   "安装第一个应用",
						Variant: components.ButtonPrimary,
					}).Clicked() {
						p.openInstall()
					}
				},
			})
		case len(list) == 0:
			components.EmptyState(c, components.EmptyProps{
				Icon:  components.IconSearch,
				Title: "没有找到匹配的应用",
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

		p.detailDrawer(c)
		p.uninstallModal(c)
		p.clearModal(c)
		p.installModal(c)
	})

}

func (p *Page) gridView(c *ui.Context, list []contract.AppItem) {

	p.grid.Key = func(i int) any { return list[i].ID }
	ui.GridView(c, &p.grid, len(list), 260, 100, func(i int) {
		p.appCard(c, list[i])
	}).Grow(1).Margin(-4, 0, 0, 0)
}

func (p *Page) appCard(c *ui.Context, app contract.AppItem) {
	t := c.Theme()

	card := ui.Row(c).
		Fill().
		Padding(t.Space(3)).
		Gap(t.Space(3)).
		Radius(theme.Radius).
		Background(t.Surface).
		Cursor(ui.Cursor(ui.CursorPointer)).
		AlignItems(ui.Center)

	theme.CardShadow.Apply(c, card)

	handled := false
	card.Children(func() {
		components.Avatar(c, app.IconText, 56)

		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
				ui.Text(c, app.DisplayName).FontSize(14).Bold().MaxLines(1)

				detail := ui.ButtonBase(c).
					Height(20).
					PaddingX(6).
					Radius(theme.RadiusXS).
					AlignItems(ui.Center).
					Justify(ui.Center).
					TextColor(t.Accent).
					Cursor(ui.CursorPointer)

				if detail.Hovered() {
					detail.Background(t.Accent.Alpha(0.12))
				}
				detail.Children(func() {
					ui.Text(c, "详情").FontSize(12).TextColor(t.Accent)
				})
				if detail.Clicked() {
					p.openDetail(app.ID)
					handled = true
				}
			})

			ui.Text(c, orDefault(app.Description, "暂无描述")).
				FontSize(12).
				TextColor(t.TextMuted).
				MaxLines(1)

			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, fmt.Sprintf("v%s   ·   %s", app.Version, lastUsedText(app))).
					FontSize(11).
					TextColor(t.TextMuted).
					MaxLines(1)
			})
		})
	})

	if card.Clicked() && !handled {
		p.launch(app.ID)
	}
}

// openDetail 打开应用详情抽屉；detailID 为空时 detailDrawer 会跳过绘制。
func (p *Page) openDetail(id string) {
	if p.appByID(id) == nil {
		return
	}
	p.detailID = id
	p.detailOpen = true
}

func (p *Page) detailDrawer(c *ui.Context) {
	app := p.appByID(p.detailID)
	t := c.Theme()

	var a contract.AppItem
	if app != nil {
		a = *app
	}

	// 即使本次没有选中应用也要构建抽屉：关闭状态下它是动画的锚点，
	// 提前建立动画通道，首次打开才有从 0 到 1 的滑入过渡。
	props := components.DrawerProps{
		Open:  &p.detailOpen,
		Title: "应用详情",
		Width: 480,
	}
	if app != nil {
		props.Footer = func() {
			if components.Button(c, components.ButtonProps{
				Label:   "卸载",
				Variant: components.ButtonDanger,
			}).Clicked() {
				p.detailOpen = false
				p.uninstallID = a.ID
				p.uninstallOpen = true
			}

			if components.Button(c, components.ButtonProps{
				Label:   "打开应用",
				Variant: components.ButtonPrimary,
			}).Clicked() {
				p.detailOpen = false
				p.launch(a.ID)
			}
		}
	}

	components.Drawer(c, props, func() {
		if app == nil {
			return
		}

		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			components.Avatar(c, a.IconText, 48)
			ui.Column(c).Gap(2).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.DisplayName).FontSize(18).Bold().MaxLines(1)
					components.Chip(c, "v"+a.Version, components.ToneNeutral)
				})
				ui.Text(c, a.Name).FontSize(11).TextColor(t.TextMuted)
			})
		})

		ui.Text(c, orDefault(a.Description, "暂无描述")).FontSize(13).TextColor(t.TextMuted)

		components.InfoRow(c, "来源", sourceLabel(a.SourceType))
		components.InfoRow(c, "安装时间", format.Time(a.InstalledAt))
		components.InfoRow(c, "最近使用", lastUsedText(a))
		components.InfoRow(c, "数据占用", format.Size(a.DataSize))

		ui.Divider(c)
		enabledCount := 0
		for _, tool := range a.Tools {
			if tool.Enabled {
				enabledCount++
			}
		}
		components.SectionTitle(c, "开放给助理的能力", fmt.Sprintf("%d/%d 已启用", enabledCount, len(a.Tools)))
		if len(a.Tools) == 0 {
			ui.Text(c, "该应用不提供助理能力").FontSize(12).TextColor(t.TextMuted)
		}
		for _, tool := range a.Tools {
			p.toolRow(c, a.ID, tool)
		}

		ui.Divider(c)
		components.SectionTitle(c, "应用数据", "")
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, fmt.Sprintf("占用 %s", format.Size(a.DataSize))).FontSize(13)
				ui.Text(c, "清除后应用保留，数据回到初始状态").FontSize(11).TextColor(t.TextMuted)
			})
			if components.Button(c, components.ButtonProps{
				Label:    "清除数据",
				Variant:  components.ButtonDanger,
				Disabled: a.DataSize == 0,
			}).Clicked() {
				p.clearID = a.ID
				p.clearOpen = true
			}
		})
	})
}

func (p *Page) toolRow(c *ui.Context, appID string, tool contract.AppTool) {
	t := c.Theme()

	ui.Box(c).
		FillWidth().
		Padding(10).
		Radius(8).
		BorderWidth(1).
		BorderColor(t.Border).
		Children(func() {
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, tool.Name).FontSize(12).Bold().MaxLines(1)
						if tool.Dangerous {
							components.Chip(c, "执行前确认", components.ToneDanger)
						}
					})
					ui.Text(c, tool.Description).FontSize(11).TextColor(t.TextMuted)
				})

				enabled := tool.Enabled
				sw := ui.Switch(c, &enabled)
				if sw.Changed() {
					p.setToolEnabled(appID, tool.Name, enabled)
				}
			})
		})
}

func (p *Page) uninstallModal(c *ui.Context) {
	app := p.appByID(p.uninstallID)
	if app == nil {
		return
	}

	switch ui.AlertDialog(c, &p.uninstallOpen, "卸载应用",
		fmt.Sprintf("确定卸载「%s」吗？保留数据卸载会留下应用设置，直接卸载会连应用数据一起删除且无法恢复。", app.DisplayName),
		"取消", "保留数据卸载", "直接卸载") {
	case 1:
		p.keepData = true
		p.uninstall()
	case 2:
		p.keepData = false
		p.uninstall()
	}
}

func (p *Page) clearModal(c *ui.Context) {
	app := p.appByID(p.clearID)
	if app == nil {
		return
	}

	switch ui.AlertDialog(c, &p.clearOpen, "清除应用数据",
		fmt.Sprintf("确定清除「%s」的应用数据吗？清除后无法恢复，应用本身会保留。", app.DisplayName), "取消", "清除") {
	case 0:
		p.clearData()
	}
}

func (p *Page) installModal(c *ui.Context) {
	t := c.Theme()

	placeholder := "例如：D:\\bowknot-apps\\pomodoro"
	if p.installType == 1 {
		placeholder = "例如：https://apps.example.com/pomodoro.tar"
	}

	components.Modal(c, components.ModalProps{
		Open:  &p.installOpen,
		Title: "安装应用",
		Width: 560,
		Footer: func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.installOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:    "安装",
				Variant:  components.ButtonPrimary,
				Disabled: p.preview == nil,
			}).Clicked() {
				p.install()
			}
		},
	}, func() {
		tabs := ui.Tabs(c, &p.installType, "本地应用包", "远程地址")
		if tabs.Changed() {
			p.installValue = ""
			p.installErr = ""
			p.preview = nil
		}

		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			// 本地包用系统文件选择器，避免让用户手输绝对路径。
			if p.installType == 0 && p.hooks.SelectPackage != nil {
				value := p.installValue
				color := t.Text
				if value == "" {
					value = "未选择 .tar 应用包"
					color = t.TextMuted
				}
				ui.Box(c).
					Grow(1).
					Height(34).
					PaddingX(10).
					Radius(theme.RadiusSM).
					Background(t.SurfaceHover).
					Children(func() {
						ui.Row(c).Fill().AlignItems(ui.Center).Children(func() {
							ui.Text(c, value).FontSize(12).TextColor(color).MaxLines(1)
						})
					})
				if components.Button(c, components.ButtonProps{Label: "选择文件"}).Clicked() {
					p.pickPackage()
				}
				return
			}

			input := ui.TextInput(c, &p.installValue).Placeholder(placeholder)
			if input.Changed() {
				p.installErr = ""
				p.preview = nil
			}
			if components.Button(c, components.ButtonProps{Label: "解析"}).Clicked() {
				p.parse()
			}
		})

		hint := "先解析应用包，确认能力清单后再安装。"
		if p.installType == 0 && p.hooks.SelectPackage != nil {
			hint = "选择 .tar 应用包后会自动解析，确认能力清单后再安装。"
		}
		ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted)

		if p.installErr != "" {
			ui.Text(c, p.installErr).FontSize(12).TextColor(t.Danger)
		}

		if p.preview != nil {
			p.previewCard(c, *p.preview)
		}
	})
}

func (p *Page) previewCard(c *ui.Context, preview contract.AppPreview) {
	t := c.Theme()
	manifest := preview.Manifest

	ui.Box(c).
		FillWidth().
		Padding(12).
		Radius(10).
		Background(t.SurfaceHover).
		Children(func() {
			ui.Column(c).Gap(8).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					components.Avatar(c, firstRune(manifest.Name), 40)
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, manifest.Name).FontSize(14).Bold().MaxLines(1)
							components.Chip(c, "v"+manifest.Version, components.ToneNeutral)
						})
						ui.Text(c, orDefault(manifest.Description, "暂无描述")).
							FontSize(11).
							TextColor(t.TextMuted).
							MaxLines(1)
					})
				})

				ui.Text(c, previewSource(preview.Source)).FontSize(11).TextColor(t.TextMuted).MaxLines(1)

				components.SectionTitle(c, "将接入助理的能力", fmt.Sprintf("%d 个", len(manifest.Tools)))
				if len(manifest.Tools) == 0 {
					ui.Text(c, "该应用不提供助理能力").FontSize(12).TextColor(t.TextMuted)
				}
				for _, tool := range manifest.Tools {
					ui.Column(c).Gap(2).Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, tool.Name).FontSize(12).Bold()
							if tool.Dangerous {
								components.Chip(c, "执行前确认", components.ToneDanger)
							}
						})
						ui.Text(c, tool.Description).FontSize(11).TextColor(t.TextMuted)
					})
				}

				ui.Text(c, "安装后能力默认全部启用，可在应用详情中逐个关闭。").
					FontSize(11).
					TextColor(t.TextMuted)
			})
		})
}

func (p *Page) launch(id string) {
	if p.svc != nil {
		_ = p.svc.MarkUsed(id)
		p.reload()
	}
	app := p.appByID(id)
	if app != nil && p.hooks.Open != nil {
		p.hooks.Open(*app)
	}
}

func (p *Page) openInstall() {
	p.installOpen = true
	p.installType = 0
	p.installValue = ""
	p.installErr = ""
	p.preview = nil
}

func (p *Page) parse() {
	if p.svc == nil {
		return
	}
	preview, err := p.svc.Preview(p.installSource())
	if err != nil {
		p.installErr = err.Error()
		p.preview = nil
		return
	}
	p.installErr = ""
	p.preview = &preview
}

// pickPackage 通过系统文件选择器拿到本地包路径，选中后直接解析。
func (p *Page) pickPackage() {
	if p.hooks.SelectPackage == nil {
		return
	}
	value, err := p.hooks.SelectPackage()
	if err != nil {
		p.installErr = err.Error()
		return
	}
	if value == "" {
		return
	}
	p.installValue = value
	p.installErr = ""
	p.preview = nil
	p.parse()
}

func (p *Page) install() {
	if p.svc == nil || p.preview == nil {
		return
	}
	if err := p.svc.Install(*p.preview); err != nil {
		p.installErr = err.Error()
		return
	}
	p.installOpen = false
	p.preview = nil
	p.reload()
}

func (p *Page) installSource() contract.InstallSource {
	kind := "local"
	if p.installType == 1 {
		kind = "remote"
	}
	return contract.InstallSource{Type: kind, Value: p.installValue}
}

func (p *Page) uninstall() {
	if p.svc == nil {
		return
	}
	// 先关闭应用窗口，释放页面占用的文件再删除安装目录。
	if p.hooks.Close != nil {
		p.hooks.Close(p.uninstallID)
	}
	if err := p.svc.Uninstall(p.uninstallID, p.keepData); err != nil {
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

func (p *Page) clearData() {
	if p.svc == nil {
		return
	}
	if err := p.svc.ClearData(p.clearID); err != nil {
		return
	}
	p.clearOpen = false
	p.reload()
}

func (p *Page) setToolEnabled(appID, toolName string, enabled bool) {
	if p.svc == nil {
		return
	}
	_ = p.svc.SetToolEnabled(appID, toolName, enabled)
	p.reload()
}

func (p *Page) reload() {
	if p.svc == nil {
		p.apps = nil
		return
	}
	p.apps = p.svc.Apps()
}

func (p *Page) visible() []contract.AppItem {
	text := strings.ToLower(strings.TrimSpace(p.query))
	list := make([]contract.AppItem, 0, len(p.apps))
	for _, app := range p.apps {
		if text == "" ||
			strings.Contains(strings.ToLower(app.DisplayName), text) ||
			strings.Contains(strings.ToLower(app.Name), text) ||
			strings.Contains(strings.ToLower(app.Description), text) {
			list = append(list, app)
		}
	}

	switch p.sortLabel {
	case "名称":
		sort.Slice(list, func(i, j int) bool { return list[i].DisplayName < list[j].DisplayName })
	case "安装时间":
		sort.Slice(list, func(i, j int) bool { return list[i].InstalledAt > list[j].InstalledAt })
	default:
		sort.Slice(list, func(i, j int) bool {
			if list[i].LastUsedAt != list[j].LastUsedAt {
				return list[i].LastUsedAt > list[j].LastUsedAt
			}
			return list[i].InstalledAt > list[j].InstalledAt
		})
	}
	return list
}

func (p *Page) appByID(id string) *contract.AppItem {
	if id == "" {
		return nil
	}
	for i := range p.apps {
		if p.apps[i].ID == id {
			return &p.apps[i]
		}
	}
	return nil
}

func lastUsedText(app contract.AppItem) string {
	if app.LastUsedAt == 0 {
		return "从未使用"
	}
	return format.Relative(app.LastUsedAt)
}

func sourceLabel(sourceType string) string {
	if sourceType == "remote" {
		return "远程地址"
	}
	return "本地应用包"
}

func previewSource(source contract.InstallSource) string {
	return sourceLabel(source.Type) + " · " + source.Value
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return "?"
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
