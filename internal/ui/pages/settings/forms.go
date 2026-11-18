package settings

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/theme"
)

// fillSettingDrafts 把设置快照填充到表单字段。
func (p *Page) fillSettingDrafts() {
	p.promptGlobal = p.draft.Prompt.Global
	p.taskConcurrent = intText(p.draft.Task.MaxConcurrent)
	p.taskTimeout = intText(p.draft.Task.TimeoutMinutes)
	p.taskAttempts = intText(p.draft.Task.MaxAttempts)
	p.taskRetry = joinInts(p.draft.Task.RetryDelaysSeconds)
	p.groupRounds = intText(p.draft.Group.MaxRounds)
	p.groupToolRounds = intText(p.draft.Group.MaxToolRounds)
	p.memoryItems = intText(p.draft.Memory.InjectItems)
	p.memoryBudget = intText(p.draft.Memory.InjectBudgetRunes)
	p.memoryRecentDays = intText(p.draft.Memory.RecentDays)
	p.memoryDigestHours = intText(p.draft.Memory.DigestIntervalHours)
	p.memoryContextTasks = intText(p.draft.Memory.ContextTaskLimit)
}

// assignSectionFields 只更新指定分类的表单字段，保留其它分类的未保存草稿。
func (p *Page) assignSectionFields(section string) {
	if p.settings == nil {
		return
	}
	p.draft = p.settings.Settings()

	switch section {
	case "prompt":
		p.promptGlobal = p.draft.Prompt.Global
	case "task":
		p.taskConcurrent = intText(p.draft.Task.MaxConcurrent)
		p.taskTimeout = intText(p.draft.Task.TimeoutMinutes)
		p.taskAttempts = intText(p.draft.Task.MaxAttempts)
		p.taskRetry = joinInts(p.draft.Task.RetryDelaysSeconds)
	case "group":
		p.groupRounds = intText(p.draft.Group.MaxRounds)
		p.groupToolRounds = intText(p.draft.Group.MaxToolRounds)
	case "memory":
		p.memoryItems = intText(p.draft.Memory.InjectItems)
		p.memoryBudget = intText(p.draft.Memory.InjectBudgetRunes)
		p.memoryRecentDays = intText(p.draft.Memory.RecentDays)
		p.memoryDigestHours = intText(p.draft.Memory.DigestIntervalHours)
		p.memoryContextTasks = intText(p.draft.Memory.ContextTaskLimit)
	}
}

func (p *Page) saveSection(section string) {
	if p.settings == nil {
		p.setError("设置服务不可用")
		return
	}
	next := p.settings.Settings()

	switch section {
	case "prompt":
		next.Prompt.Global = p.promptGlobal
	case "task":
		concurrent, ok := p.requiredInt("任务并发数", p.taskConcurrent)
		if !ok {
			return
		}
		timeout, ok := p.requiredInt("任务超时", p.taskTimeout)
		if !ok {
			return
		}
		attempts, ok := p.requiredInt("任务尝试次数", p.taskAttempts)
		if !ok {
			return
		}
		delays, err := parseDelays(p.taskRetry)
		if err != nil {
			p.setError(err.Error())
			return
		}
		next.Task = contract.TaskSettings{
			MaxConcurrent:      concurrent,
			TimeoutMinutes:     timeout,
			MaxAttempts:        attempts,
			RetryDelaysSeconds: delays,
		}
	case "group":
		rounds, ok := p.requiredInt("群聊轮次", p.groupRounds)
		if !ok {
			return
		}
		toolRounds, ok := p.requiredInt("工具调用轮次", p.groupToolRounds)
		if !ok {
			return
		}
		next.Group = contract.GroupSettings{MaxRounds: rounds, MaxToolRounds: toolRounds}
	case "memory":
		items, ok := p.requiredInt("记忆注入条数", p.memoryItems)
		if !ok {
			return
		}
		budget, ok := p.requiredInt("记忆字数预算", p.memoryBudget)
		if !ok {
			return
		}
		days, ok := p.requiredInt("记忆时间窗口", p.memoryRecentDays)
		if !ok {
			return
		}
		digest, ok := p.requiredInt("记忆摘要间隔", p.memoryDigestHours)
		if !ok {
			return
		}
		contextTasks, ok := p.requiredInt("直聊任务条数", p.memoryContextTasks)
		if !ok {
			return
		}
		next.Memory = contract.MemorySettings{
			InjectItems:         items,
			InjectBudgetRunes:   budget,
			RecentDays:          days,
			DigestIntervalHours: digest,
			ContextTaskLimit:    contextTasks,
		}
	default:
		p.setError("该分类暂不支持保存")
		return
	}

	if err := p.settings.Save(next); err != nil {
		p.setError(err.Error())
		return
	}
	p.assignSectionFields(section)
	p.setNotice("已保存并生效。")
}

func (p *Page) requiredInt(label, value string) (int, bool) {
	value = strings.TrimSpace(value)
	n, err := strconv.Atoi(value)
	if err != nil {
		p.setError(label + "需填写数字")
		return 0, false
	}
	return n, true
}

func parseDelays(value string) ([]int, error) {
	out := make([]int, 0, 4)
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, errors.New("重试间隔需填写逗号分隔的秒数，例如 5, 15")
		}
		out = append(out, n)
	}
	return out, nil
}

func (p *Page) currentTheme() string {
	if p.settings == nil {
		return contract.ThemeSystem
	}
	return p.settings.Settings().Appearance.Theme
}

func (p *Page) selectTheme(value string) {
	if p.settings == nil {
		p.setError("设置服务不可用")
		return
	}
	next := p.settings.Settings()
	next.Appearance.Theme = value
	if err := p.settings.Save(next); err != nil {
		p.setError(err.Error())
		return
	}
	if p.hooks.ApplyTheme != nil {
		p.hooks.ApplyTheme(value)
	}
	p.setNotice("外观已更新。")
}

// settingCard 是设置表单的通用外壳：内容 + 底部操作栏。
func (p *Page) settingCard(c *ui.Context, section string, body func()) {
	components.Card(c, components.CardProps{Gap: 10}, func() {
		body()
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).AlignItems(ui.Center).Children(func() {
			if components.Button(c, components.ButtonProps{
				Label:   "恢复默认",
				Variant: components.ButtonGhost,
			}).Clicked() {
				p.resetSection = section
				p.resetOpen = true
			}
			if components.Button(c, components.ButtonProps{
				Label:   "保存",
				Variant: components.ButtonPrimary,
			}).Clicked() {
				p.saveSection(section)
			}
		})
	})
}

func (p *Page) numberField(c *ui.Context, label, placeholder string, value *string, maxLength int) {
	t := c.Theme()

	ui.Column(c).Grow(1).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(12).Bold().TextColor(t.TextMuted)
		ui.TextInput(c, value).Placeholder(placeholder).MaxLength(maxLength)
	})
}

func (p *Page) promptSection(c *ui.Context) {
	t := c.Theme()

	p.settingCard(c, "prompt", func() {
		ui.Text(c, "全局提示词").FontSize(13).Bold()
		ui.TextAreaBase(c, &p.promptGlobal).
			Placeholder("例如：回答保持简洁，先给结论再展开。").
			Lines(5, 10).
			FillWidth().
			MaxLength(4000)
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, "作用范围：主助手、员工、群主。").
				FontSize(11).
				TextColor(t.TextMuted)
			ui.Text(c, fmt.Sprintf("%d / 4000 字", len([]rune(p.promptGlobal)))).
				FontSize(11).
				TextColor(t.TextMuted)
		})
		ui.Text(c, "内置工具与插件说明仍由代码生成，不在此处配置。").
			FontSize(11).
			TextColor(t.TextMuted)
	})
}

func (p *Page) taskSection(c *ui.Context) {
	t := c.Theme()

	p.settingCard(c, "task", func() {
		ui.Text(c, "任务调度").FontSize(13).Bold()
		ui.Text(c, "调整后立即生效；并发调小时已排队任务会等待空位。").
			FontSize(11).
			TextColor(t.TextMuted)

		ui.Row(c).FillWidth().Gap(10).Children(func() {
			p.numberField(c, "并发数", "1–16", &p.taskConcurrent, 4)
			p.numberField(c, "超时（分钟）", "1–120", &p.taskTimeout, 4)
		})
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			p.numberField(c, "尝试次数", "1–5，含首次", &p.taskAttempts, 2)
			p.numberField(c, "重试间隔（秒）", "例如：5, 15", &p.taskRetry, 40)
		})
		ui.Text(c, "重试间隔按尝试次数自动截断或补默认值。").
			FontSize(11).
			TextColor(t.TextMuted)
	})
}

func (p *Page) groupSection(c *ui.Context) {
	t := c.Theme()

	p.settingCard(c, "group", func() {
		ui.Text(c, "群聊与工具").FontSize(13).Bold()
		ui.Text(c, "群聊轮次决定单条消息触发多少轮成员讨论；工具轮次限制单次生成内的连续调用。").
			FontSize(11).
			TextColor(t.TextMuted)

		ui.Row(c).FillWidth().Gap(10).Children(func() {
			p.numberField(c, "群聊轮次", "1–10", &p.groupRounds, 2)
			p.numberField(c, "工具调用轮次", "1–20", &p.groupToolRounds, 2)
		})
	})
}

func (p *Page) memorySection(c *ui.Context) {
	t := c.Theme()

	p.settingCard(c, "memory", func() {
		ui.Text(c, "记忆").FontSize(13).Bold()
		ui.Text(c, "控制注入条数、字符预算、时间窗口与后台摘要频率；摘要间隔在下一轮后生效。").
			FontSize(11).
			TextColor(t.TextMuted)

		ui.Row(c).FillWidth().Gap(10).Children(func() {
			p.numberField(c, "注入条数", "1–50", &p.memoryItems, 2)
			p.numberField(c, "字数预算", "500–8000", &p.memoryBudget, 5)
		})
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			p.numberField(c, "时间窗口（天）", "1–365", &p.memoryRecentDays, 3)
			p.numberField(c, "摘要间隔（小时）", "1–168", &p.memoryDigestHours, 3)
		})
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			p.numberField(c, "直聊任务条数", "0–30，0 表示不注入", &p.memoryContextTasks, 2)
		})
	})
}

func (p *Page) appearanceSection(c *ui.Context) {
	t := c.Theme()

	options := []struct {
		Value string
		Label string
		Desc  string
	}{
		{contract.ThemeSystem, "跟随系统", "随系统外观自动切换"},
		{contract.ThemeDark, "暗黑模式", "始终使用深色背景"},
		{contract.ThemeLight, "明亮模式", "始终使用浅色背景"},
	}

	components.Card(c, components.CardProps{Gap: 8}, func() {
		for _, option := range options {
			selected := p.currentTheme() == option.Value
			row := ui.Row(c).
				FillWidth().
				Padding(10, 12).
				Radius(theme.RadiusSM).
				BorderWidth(1).
				BorderColor(t.Border).
				Gap(10).
				AlignItems(ui.Center)
			if selected {
				row.BorderColor(t.Accent)
			} else if row.Hovered() {
				row.Background(t.SurfaceHover)
			}
			row.Children(func() {
				ui.Column(c).Gap(2).Grow(1).Children(func() {
					ui.Text(c, option.Label).FontSize(13).Bold()
					ui.Text(c, option.Desc).FontSize(11).TextColor(t.TextMuted)
				})
				if selected {
					components.Chip(c, "当前", components.ToneBrand)
				}
			})
			if row.Clicked() && !selected {
				p.selectTheme(option.Value)
			}
		}
	})
}

func (p *Page) resetModal(c *ui.Context) {
	if !p.resetOpen {
		return
	}

	section := p.resetSection
	if ui.AlertDialog(c, &p.resetOpen,
		"恢复默认",
		"将把「"+sectionLabel(section)+"」恢复为默认值，其它分类保持不变。",
		"取消", "恢复默认") == 1 {
		if p.settings == nil {
			p.setError("设置服务不可用")
		} else if err := p.settings.Reset(section); err != nil {
			p.setError(err.Error())
		} else {
			p.assignSectionFields(section)
			p.setNotice("已恢复默认值。")
		}
	}
	if !p.resetOpen {
		p.resetSection = ""
	}
}

func sectionLabel(id string) string {
	for _, item := range sections {
		if item.ID == id {
			return item.Label
		}
	}
	return id
}

func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ", ")
}
