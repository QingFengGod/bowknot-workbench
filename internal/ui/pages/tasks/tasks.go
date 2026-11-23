package tasks

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
	// OpenAgentChat 跳转到员工的直聊会话。
	OpenAgentChat func(agentID string)
}

// statusOption 是任务状态筛选。
type statusOption struct {
	Label  string
	Status contract.TaskStatus
}

var statusOptions = []statusOption{
	{Label: "全部"},
	{Label: "排队中", Status: contract.TaskQueued},
	{Label: "运行中", Status: contract.TaskRunning},
	{Label: "已完成", Status: contract.TaskDone},
	{Label: "失败", Status: contract.TaskFailed},
	{Label: "已取消", Status: contract.TaskCanceled},
	{Label: "已超时", Status: contract.TaskTimeout},
}

// rangeOption 是统计与列表的时间范围。
type rangeOption struct {
	Label string
	Days  int
}

var rangeOptions = []rangeOption{
	{Label: "全部时间"},
	{Label: "近 7 天", Days: 7},
	{Label: "近 30 天", Days: 30},
}

// Page 是任务审计页：统计、筛选、任务列表与过程详情。
type Page struct {
	svc       contract.TaskService
	agents    contract.AgentService
	artifacts contract.ArtifactService
	hooks     Hooks

	audit       contract.TaskAudit
	items       []contract.TaskDetail
	statusIndex int
	rangeIndex  int
	ownerID     string
	ownerLabel  string
	owners      []contract.AgentInfo
	ownerOpen   bool
	search      string

	lastRevision uint64
	errText      string

	detailOpen      bool
	detail          contract.TaskDetail
	detailMessages  []contract.ChatMessage
	detailArtifacts []contract.ArtifactDetail

	// 文件预览：抽屉状态与删除确认统一由 ArtifactHost 管理。
	artifactHost *components.ArtifactHost
}

func New(svc contract.TaskService, agents contract.AgentService, artifacts contract.ArtifactService, hooks Hooks) *Page {
	return &Page{
		svc: svc, agents: agents, artifacts: artifacts, hooks: hooks,
		ownerLabel: "全部员工", artifactHost: components.NewArtifactHost(artifacts),
	}
}

// OnShow 在切换到本页时刷新员工列表与任务统计。
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
	revision := p.svc.TaskRevision()
	if !force && revision == p.lastRevision {
		return
	}
	p.lastRevision = revision
	p.audit = p.svc.TaskAudit(rangeOptions[p.rangeIndex].Days)
	p.applyFilter()
	// 任务状态变化时同步刷新已打开的详情，避免抽屉里的状态过期。
	p.reloadDetail()
}

// applyFilter 只做本地筛选：统计接口已按时间范围裁剪过最近任务。
func (p *Page) applyFilter() {
	option := statusOptions[p.statusIndex]
	query := strings.TrimSpace(p.search)

	items := make([]contract.TaskDetail, 0, len(p.audit.Recent))
	for _, task := range p.audit.Recent {
		if option.Status != "" && task.Status != option.Status {
			continue
		}
		if p.ownerID != "" && task.AssigneeID != p.ownerID {
			continue
		}
		if query != "" &&
			!strings.Contains(task.Title, query) &&
			!strings.Contains(task.Brief, query) {
			continue
		}
		items = append(items, task)
	}
	p.items = items
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
		p.summary(c)
		p.filters(c)
		ui.Row(c).FillWidth().Grow(1).Gap(t.Space(3)).Children(func() {
			p.taskList(c)
			p.agentPanel(c)
		})
	})

	p.detailDrawer(c)
	p.artifactDrawer(c)
}

func (p *Page) header(c *ui.Context) {
	t := c.Theme()
	stats := p.audit.Overall

	ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, "任务").FontSize(18).Bold()
			ui.Text(c, fmt.Sprintf("共 %d 个任务 · 成功率 %s · 平均耗时 %s",
				stats.Total, p.rateText(stats), format.Duration(stats.AvgDurationMs))).
				FontSize(11).
				TextColor(t.TextMuted)
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			search := ui.TextInput(c, &p.search).
				Placeholder("搜索任务标题或描述").
				Width(220).
				MaxLength(60)
			if search.Changed() {
				p.applyFilter()
			}
			p.ownerFilter(c)
		})
	})
}

// summary 用一张卡片展示时间范围内的关键指标。
func (p *Page) summary(c *ui.Context) {
	t := c.Theme()
	stats := p.audit.Overall

	components.Card(c, components.CardProps{}, func() {
		ui.Row(c).FillWidth().Gap(t.Space(3)).Children(func() {
			p.summaryItem(c, "总任务", fmt.Sprintf("%d", stats.Total), p.rangeLabel(), t.Text)
			p.summaryItem(c, "成功率", p.rateText(stats),
				fmt.Sprintf("完成 %d · 失败 %d", stats.Done, stats.Failed+stats.Timeout),
				p.rateColor(c, stats))
			p.summaryItem(c, "平均耗时", format.Duration(stats.AvgDurationMs), "仅统计已结束任务", t.Text)
			p.summaryItem(c, "进行中", fmt.Sprintf("%d", stats.Queued+stats.Running),
				fmt.Sprintf("排队 %d · 运行 %d", stats.Queued, stats.Running), t.Accent)
		})
	})
}

func (p *Page) summaryItem(c *ui.Context, label, value, hint string, color ui.Color) {
	t := c.Theme()

	ui.Column(c).Grow(1).Gap(3).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, value).FontSize(20).Bold().TextColor(color)
		ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
	})
}

func (p *Page) filters(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			for index := range statusOptions {
				option := statusOptions[index]
				selected := index == p.statusIndex
				p.pill(c, option.Label, selected, func() {
					p.statusIndex = index
					p.applyFilter()
				})
			}
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			for index := range rangeOptions {
				option := rangeOptions[index]
				selected := index == p.rangeIndex
				p.pill(c, option.Label, selected, func() {
					p.rangeIndex = index
					p.reload(true)
				})
			}
		})
	})
}

// pill 是一枚筛选胶囊，选中时用品牌色浅底。
func (p *Page) pill(c *ui.Context, label string, selected bool, onClick func()) {
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
	btn.Children(func() {
		ui.Text(c, label).FontSize(12)
	})
	if btn.Clicked() && !selected {
		onClick()
	}
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
		p.ownerOption(c, "", "全部员工")
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
			p.applyFilter()
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

func (p *Page) taskList(c *ui.Context) {
	if len(p.items) == 0 {
		ui.Box(c).Grow(1).FillHeight().Children(func() {
			components.EmptyState(c, components.EmptyProps{
				Icon:  components.IconTasks,
				Title: "没有符合条件的任务",
				Hint:  "调整状态、员工或时间范围后再试试。",
			})
		})
		return
	}

	// Row 默认按交叉轴居中排版，这里显式撑满高度，让列表从顶部开始排列。
	ui.Scroll(c).Grow(1).FillHeight().Children(func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			for _, detail := range p.items {
				p.taskRow(c, detail)
			}
		})
	})
}

func (p *Page) taskRow(c *ui.Context, task contract.TaskDetail) {
	t := c.Theme()

	components.Card(c, components.CardProps{}, func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, task.Title).FontSize(13).Bold().MaxLines(1).Grow(1)
			components.TaskStatusChip(c, task.Status)
		})

		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if task.AssigneeName != "" {
				ui.Text(c, "负责人："+task.AssigneeName).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
			}
			if task.DurationMs > 0 {
				ui.Text(c, "耗时 "+format.Duration(task.DurationMs)).FontSize(11).TextColor(t.TextMuted)
			}
			if task.Attempt > 1 {
				ui.Text(c, fmt.Sprintf("第 %d 次尝试", task.Attempt)).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Text(c, format.Time(task.CreatedAt)).FontSize(11).TextColor(t.TextMuted)
		})

		content := task.ResultSummary
		if content == "" {
			content = task.Brief
		}
		if content != "" {
			ui.Text(c, content).FontSize(12).TextColor(t.Text).MaxLines(2)
		}
		if task.Error != "" && (task.Status == contract.TaskFailed || task.Status == contract.TaskTimeout) {
			ui.Text(c, "原因："+task.Error).FontSize(11).TextColor(t.Danger).MaxLines(2)
		}

		ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
			if components.TextButton(c, "查看过程", false).Clicked() {
				p.openDetail(task.ID)
			}
			if p.hooks.OpenAgentChat != nil && task.AssigneeID != "" {
				if components.TextButton(c, "打开对话", false).Clicked() {
					p.hooks.OpenAgentChat(task.AssigneeID)
				}
			}
			if task.Status == contract.TaskQueued || task.Status == contract.TaskRunning {
				if components.TextButton(c, "取消", true).Clicked() {
					p.cancelTask(task.ID)
				}
			}
			if task.Status.Terminal() {
				if components.TextButton(c, "重试", false).Clicked() {
					p.retryTask(task.ID)
				}
			}
		})
	})
}

// agentPanel 是右侧的员工表现列表。
func (p *Page) agentPanel(c *ui.Context) {
	t := c.Theme()

	panel := ui.Column(c).
		Width(280).
		Shrink(0).
		FillHeight().
		Padding(14).
		Gap(8).
		Radius(theme.Radius).
		Background(t.Surface).
		BorderWidth(1).
		BorderColor(t.Border)

	panel.Children(func() {
		components.SectionTitle(c, "员工表现", fmt.Sprintf("%d 位", len(p.audit.Agents)))
		if len(p.audit.Agents) == 0 {
			ui.Text(c, "当前范围内还没有任务记录").FontSize(11).TextColor(t.TextMuted)
			return
		}
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(12).Children(func() {
				for _, item := range p.audit.Agents {
					p.agentStatRow(c, item)
				}
			})
		})
	})
}

func (p *Page) agentStatRow(c *ui.Context, item contract.AgentTaskStats) {
	t := c.Theme()
	name := item.AgentName
	if name == "" {
		name = "未知员工"
	}

	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, name).FontSize(12).Bold().MaxLines(1).Grow(1)
			ui.Text(c, p.rateText(item.Stats)).FontSize(12).Bold().TextColor(p.rateColor(c, item.Stats))
		})
		hint := fmt.Sprintf("%d 个任务 · 平均 %s", item.Stats.Total, format.Duration(item.Stats.AvgDurationMs))
		if active := item.Stats.Queued + item.Stats.Running; active > 0 {
			hint += fmt.Sprintf(" · 进行中 %d", active)
		}
		ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
	})
}

func (p *Page) rateText(stats contract.TaskStats) string {
	if stats.Done+stats.Failed+stats.Canceled+stats.Timeout == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", stats.SuccessRate*100)
}

func (p *Page) rateColor(c *ui.Context, stats contract.TaskStats) ui.Color {
	t := c.Theme()
	switch {
	case stats.Done+stats.Failed+stats.Canceled+stats.Timeout == 0:
		return t.Text
	case stats.SuccessRate >= 0.8:
		return t.Success
	case stats.SuccessRate >= 0.5:
		return t.Warning
	default:
		return t.Danger
	}
}

func (p *Page) rangeLabel() string {
	return rangeOptions[p.rangeIndex].Label
}

// ---------- 任务详情 ----------

func (p *Page) openDetail(taskID string) {
	if p.svc == nil || taskID == "" {
		return
	}
	p.detail = p.svc.Task(taskID)
	p.detailMessages = p.svc.TaskMessages(taskID)
	p.detailArtifacts = nil
	if p.artifacts != nil {
		p.detailArtifacts = p.artifacts.Artifacts(contract.ArtifactFilter{TaskID: taskID, Limit: 50})
	}
	p.detailOpen = true
}

func (p *Page) reloadDetail() {
	if !p.detailOpen || p.detail.ID == "" {
		return
	}
	p.openDetail(p.detail.ID)
}

func (p *Page) detailDrawer(c *ui.Context) {
	task := p.detail

	components.Drawer(c, components.DrawerProps{
		Open:  &p.detailOpen,
		Title: "任务详情",
		Width: 620,
		Footer: func() {
			if task.ID == "" {
				return
			}
			if task.Status.Terminal() {
				if components.Button(c, components.ButtonProps{Label: "重试", Variant: components.ButtonSecondary}).Clicked() {
					p.retryTask(task.ID)
				}
			}
			if task.Status == contract.TaskQueued || task.Status == contract.TaskRunning {
				if components.Button(c, components.ButtonProps{Label: "取消任务", Variant: components.ButtonDanger}).Clicked() {
					p.cancelTask(task.ID)
				}
			}
			if p.hooks.OpenAgentChat != nil && task.AssigneeID != "" {
				if components.Button(c, components.ButtonProps{Label: "打开员工对话", Variant: components.ButtonGhost}).Clicked() {
					p.detailOpen = false
					p.hooks.OpenAgentChat(task.AssigneeID)
				}
			}
		},
	}, func() {
		if task.ID == "" {
			return
		}
		p.detailBody(c, task)
	})
}

func (p *Page) detailBody(c *ui.Context, task contract.TaskDetail) {
	t := c.Theme()

	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Text(c, task.Title).FontSize(16).Bold().MaxLines(2)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			components.TaskStatusChip(c, task.Status)
			if task.AssigneeName != "" {
				ui.Text(c, task.AssigneeName).FontSize(11).TextColor(t.TextMuted)
			}
			if task.Attempt > 1 {
				ui.Text(c, fmt.Sprintf("第 %d 次尝试", task.Attempt)).FontSize(11).TextColor(t.TextMuted)
			}
		})
	})

	components.InfoRow(c, "创建时间", format.Time(task.CreatedAt))
	components.InfoRow(c, "开始时间", format.Time(task.StartedAt))
	components.InfoRow(c, "结束时间", format.Time(task.FinishedAt))
	components.InfoRow(c, "执行耗时", format.Duration(task.DurationMs))
	if task.OriginTitle != "" {
		components.InfoRow(c, "来源会话", task.OriginTitle)
	}

	if task.Brief != "" {
		components.SectionTitle(c, "任务说明", "")
		ui.Text(c, task.Brief).FontSize(12).TextColor(t.Text)
	}
	if task.ResultSummary != "" {
		components.SectionTitle(c, "执行结果", "")
		ui.Text(c, task.ResultSummary).FontSize(12).TextColor(t.Text)
	}
	if task.Error != "" {
		components.SectionTitle(c, "错误信息", "")
		ui.Text(c, task.Error).FontSize(12).TextColor(t.Danger)
	}

	if len(p.detailArtifacts) > 0 {
		components.SectionTitle(c, "产出文件", fmt.Sprintf("%d 个", len(p.detailArtifacts)))
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			for _, artifact := range p.detailArtifacts {
				item := artifact
				components.ArtifactCard(c, components.ArtifactCardProps{
					Artifact: item.ArtifactBrief,
					OnOpen:   func() { p.openArtifact(item.ID) },
				})
			}
		})
	}

	components.SectionTitle(c, "执行记录", "")
	if len(p.detailMessages) == 0 {
		components.EmptyState(c, components.EmptyProps{
			Icon:  components.IconMessage,
			Title: "还没有执行记录",
			Hint:  "任务开始执行后，这里会显示员工的完整工作过程。",
		})
		return
	}
	avatar := firstRune(task.AssigneeName, "员")
	for _, message := range p.detailMessages {
		p.processMessage(c, message, avatar)
	}
}

// processMessage 渲染任务线程的一条记录；派发消息按内部说明展示，不套用户气泡。
func (p *Page) processMessage(c *ui.Context, m contract.ChatMessage, avatar string) {
	if m.MessageType == "task_dispatch" {
		t := c.Theme()
		ui.Column(c).
			FillWidth().
			Padding(10).
			Gap(4).
			Radius(theme.RadiusSM).
			Background(t.SurfaceHover).
			Children(func() {
				ui.Text(c, "主助手派发任务").FontSize(11).Bold().TextColor(t.TextMuted)
				ui.Text(c, m.Content).FontSize(12).TextColor(t.Text)
			})
		return
	}

	components.MessageView(c, components.MessageViewProps{
		Message:        m,
		AvatarText:     avatar,
		OnOpenArtifact: p.openArtifact,
	})
}

// firstRune 取名称首字作为头像文字，空名称回退到默认字。
func firstRune(name, fallback string) string {
	for _, r := range strings.TrimSpace(name) {
		return string(r)
	}
	return fallback
}

// ---------- 任务操作 ----------

func (p *Page) cancelTask(taskID string) {
	if p.svc == nil {
		return
	}
	// 取消运行中的任务要等线程收尾，放到后台避免卡住当前帧。
	go func() {
		err := p.svc.CancelTask(taskID)
		p.update(func() {
			if err != nil {
				p.errText = err.Error()
				return
			}
			p.errText = ""
			p.reload(true)
		})
	}()
}

func (p *Page) retryTask(taskID string) {
	if p.svc == nil {
		return
	}
	go func() {
		err := p.svc.RetryTask(taskID)
		p.update(func() {
			if err != nil {
				p.errText = err.Error()
				return
			}
			p.errText = ""
			p.reload(true)
		})
	}()
}

func (p *Page) update(fn func()) {
	if p.hooks.Update != nil {
		p.hooks.Update(fn)
		return
	}
	fn()
}

// ---------- 产物预览 ----------

func (p *Page) openArtifact(id string) {
	if p.artifacts == nil || id == "" {
		return
	}
	p.artifactHost.Open(id)
}

func (p *Page) artifactDrawer(c *ui.Context) {
	p.artifactHost.Render(c, components.ArtifactHostProps{OnDelete: p.deleteArtifact})
}

func (p *Page) deleteArtifact(id string) {
	if p.artifacts == nil || id == "" {
		return
	}
	go func() {
		err := p.artifacts.DeleteArtifact(id)
		p.update(func() {
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
