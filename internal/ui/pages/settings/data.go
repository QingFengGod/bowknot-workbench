package settings

import (
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/format"
	"bowknot-workbench/internal/ui/theme"
)

// usageItem 是数据目录里一项占用。
type usageItem struct {
	Label string
	Path  string
	Size  int64
}

// loadDataInfo 在进入设置页时刷新日志级别与目录占用。
func (p *Page) loadDataInfo() {
	p.usage = nil
	p.usageErr = ""
	if p.models != nil {
		p.logLevel = p.models.LogLevel()
	}
	if p.dataRoot == "" {
		return
	}

	items := []usageItem{
		{Label: "数据库", Path: p.databasePath},
		{Label: "日志", Path: p.logsPath},
		{Label: "应用", Path: filepath.Join(p.dataRoot, "apps")},
		{Label: "插件", Path: filepath.Join(p.dataRoot, "plugins")},
		{Label: "产物", Path: filepath.Join(p.dataRoot, "artifacts")},
		{Label: "临时文件", Path: filepath.Join(p.dataRoot, "tmp")},
	}
	for i := range items {
		if items[i].Path == "" {
			continue
		}
		size, err := pathSize(items[i].Path)
		if err != nil {
			p.usageErr = "读取目录占用失败：" + err.Error()
		}
		items[i].Size = size
	}
	p.usage = items
}

// pathSize 统计文件或目录占用的字节数；条目不可读时累加已知部分。
func pathSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}

	var total int64
	err = filepath.WalkDir(path, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if item, err := entry.Info(); err == nil {
			total += item.Size()
		}
		return nil
	})
	return total, err
}

func (p *Page) dataSection(c *ui.Context) {
	t := c.Theme()

	components.Card(c, components.CardProps{Gap: 8}, func() {
		components.SectionTitle(c, "数据目录", "")
		p.infoLine(c, "数据根目录", p.dataRoot)
		p.infoLine(c, "数据库", p.databasePath)
		p.infoLine(c, "配置文件", p.configPath)
		p.infoLine(c, "日志目录", p.logsPath)

		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			components.Button(c, components.ButtonProps{
				Label:    "打开数据目录",
				Variant:  components.ButtonSecondary,
				Disabled: true,
			})
			components.Button(c, components.ButtonProps{
				Label:    "导出备份",
				Variant:  components.ButtonSecondary,
				Disabled: true,
			})
			ui.Text(c, "备份与清理功能开发中").FontSize(11).TextColor(t.TextMuted)
		})
	})

	components.Card(c, components.CardProps{Gap: 8}, func() {
		components.SectionTitle(c, "目录占用", "")
		if p.usageErr != "" {
			ui.Text(c, p.usageErr).FontSize(12).TextColor(t.Danger)
		}
		if len(p.usage) == 0 {
			ui.Text(c, "暂无数据").FontSize(12).TextColor(t.TextMuted)
			return
		}
		for _, item := range p.usage {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Column(c).Gap(1).Children(func() {
					ui.Text(c, item.Label).FontSize(12)
					ui.Text(c, item.Path).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
				})
				ui.Text(c, format.Size(item.Size)).FontSize(12).Bold()
			})
		}
	})

	components.Card(c, components.CardProps{Gap: 8}, func() {
		components.SectionTitle(c, "日志", "重启生效")
		p.infoLine(c, "日志级别", p.logLevel)
		ui.Text(c, "日志按 10MB 轮转并保留 5 份；级别在 config.json 中配置，修改后需要重启应用。").
			FontSize(11).
			TextColor(t.TextMuted)
	})
}

func (p *Page) aboutSection(c *ui.Context) {
	t := c.Theme()

	info := contract.AppInfo{}
	if p.app != nil {
		info = p.app.Info()
	}
	name := info.Name
	if name == "" {
		name = "Bowknot Workbench"
	}
	version := info.Version
	if version == "" {
		version = "未知"
	}

	components.Card(c, components.CardProps{Gap: 10}, func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).
				Width(44).
				Height(44).
				Radius(theme.Radius).
				Background(t.Accent.Alpha(0.12)).
				AlignItems(ui.Center).
				Justify(ui.Center).
				Children(func() {
					components.Icon(c, components.IconLogo, 28).TextColor(t.Accent)
				})
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, name).FontSize(15).Bold()
				ui.Text(c, "版本 "+version).FontSize(12).TextColor(t.TextMuted)
			})
		})

		p.infoLine(c, "数据目录", p.dataRoot)
		p.infoLine(c, "配置文件", p.configPath)

		ui.Text(c, "本地优先的 AI 助理与多 Agent 协作工作台，数据存放在本机。").
			FontSize(11).
			TextColor(t.TextMuted)
	})
}

// infoLine 是名称与值的只读信息行。
func (p *Page) infoLine(c *ui.Context, label, value string) {
	t := c.Theme()

	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).Width(96).Shrink(0)
		if value == "" {
			value = "未配置"
		}
		ui.Text(c, value).FontSize(12).MaxLines(2)
	})
}
