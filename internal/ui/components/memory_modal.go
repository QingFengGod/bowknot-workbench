package components

import (
	"fmt"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/theme"
)

// MemoryPanelState 缓存记忆面板的数据，避免每帧查询数据库；由页面持有。
type MemoryPanelState struct {
	Loaded        bool
	RefreshedAt   time.Time
	AgentID       string
	GlobalEnabled bool
	AgentEnabled  bool
	UserItems     []contract.MemoryItem
	AgentItems    []contract.MemoryItem
}

// MemoryModalProps 描述长期记忆面板及其清空确认。
// 所有开关与数据缓存由页面持有，组件只读写稳定指针。
// 单条记忆的增删改由模型通过记忆工具完成，面板只负责查看与整体清空。
type MemoryModalProps struct {
	Service contract.MemoryService
	Open    *bool
	// State 是页面的数据缓存，组件在失效时刷新。
	State *MemoryPanelState
	// AgentID 非空时同时展示该 Agent 的专属记忆与开关。
	AgentID   string
	AgentName string

	ClearOpen *bool

	// OnChanged 在记忆或开关变化后触发，页面可据此刷新消息的「已记住」标记。
	OnChanged func()
}

// MemoryModal 是长期记忆管理弹窗：开关、列表与整体清空。
func MemoryModal(c *ui.Context, p MemoryModalProps) {
	t := c.Theme()
	if p.Service == nil || p.Open == nil || p.ClearOpen == nil || p.State == nil {
		return
	}

	p.refreshState()
	userItems := p.State.UserItems
	agentItems := p.State.AgentItems

	Modal(c, ModalProps{Open: p.Open, Title: "长期记忆", Width: 640, Footer: func() {
		clear := TextButton(c, "清空全部记忆", true)
		if clear.Clicked() {
			*p.ClearOpen = true
		}
		ui.Box(c).Grow(1)
		close := Button(c, ButtonProps{Label: "关闭", Variant: ButtonSecondary})
		if close.Clicked() {
			*p.Open = false
		}
	}}, func() {
		if p.AgentID != "" {
			// Agent 上下文只展示该 Agent 的开关与专属记忆；
			// 用户全局记忆属于主助手，不在这里混入。
			name := p.AgentName
			if name == "" {
				name = "当前 Agent"
			}
			p.toggleRow(c, "允许长期记忆（"+name+"）", "只影响该 Agent 的专属记忆", p.State.AgentEnabled, func(enabled bool) {
				_ = p.Service.SetMemoryEnabled(p.AgentID, enabled)
				p.invalidate()
			})
			p.memorySection(c, "「"+name+"」的专属记忆", agentItems, 360,
				"该 Agent 还没有专属记忆，聊到值得记住的事时它会主动记下来。")
			return
		}

		p.toggleRow(c, "允许长期记忆（全局）", "关闭后不再抽取和注入用户级记忆", p.State.GlobalEnabled, func(enabled bool) {
			_ = p.Service.SetMemoryEnabled("", enabled)
			p.invalidate()
		})

		ui.Box(c).FillWidth().Height(1).Background(t.Border)

		if len(userItems) == 0 && len(agentItems) == 0 {
			ui.Column(c).FillWidth().Padding(24).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, "还没有长期记忆").FontSize(13).Bold()
				ui.Text(c, "和助手聊到偏好或重要事实时，它会主动记下来。").FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
			})
			return
		}

		ui.Scroll(c).FillWidth().Height(360).Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				if len(userItems) > 0 {
					SectionTitle(c, "用户全局记忆", fmt.Sprintf("%d 条", len(userItems)))
					p.memoryList(c, userItems)
				}
				if len(agentItems) > 0 {
					SectionTitle(c, "Agent 专属记忆", fmt.Sprintf("%d 条", len(agentItems)))
					p.memoryList(c, agentItems)
				}
			})
		})
	})

	// 清空确认
	Modal(c, ModalProps{Open: p.ClearOpen, Title: "清空全部记忆", Width: 420, Footer: func() {
		cancel := Button(c, ButtonProps{Label: "取消", Variant: ButtonSecondary})
		if cancel.Clicked() {
			*p.ClearOpen = false
		}
		confirm := Button(c, ButtonProps{Label: "确认清空", Variant: ButtonDanger})
		if confirm.Clicked() {
			_ = p.Service.ClearMemories()
			*p.ClearOpen = false
			p.invalidate()
		}
	}}, func() {
		message := "清空后所有长期记忆与摘要都会删除，且无法恢复。"
		if p.AgentID != "" {
			message = "清空会删除所有 Agent 与主助手的长期记忆和摘要，且无法恢复。"
		}
		ui.Text(c, message).FontSize(13)
	})
}

func (p MemoryModalProps) toggleRow(c *ui.Context, label, hint string, enabled bool, onChange func(bool)) {
	t := c.Theme()

	ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13)
			ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted)
		})
		value := enabled
		sw := ui.Switch(c, &value)
		if sw.Changed() {
			onChange(value)
		}
	})
}

// memorySection 渲染一个记忆分区：标题与列表，为空时给出针对性提示。
func (p MemoryModalProps) memorySection(c *ui.Context, title string, items []contract.MemoryItem, height float32, emptyHint string) {
	t := c.Theme()
	SectionTitle(c, title, fmt.Sprintf("%d 条", len(items)))
	if len(items) == 0 {
		ui.Column(c).FillWidth().Padding(16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, emptyHint).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
		})
		return
	}
	ui.Scroll(c).FillWidth().Height(height).Children(func() {
		p.memoryList(c, items)
	})
}

func (p MemoryModalProps) memoryList(c *ui.Context, items []contract.MemoryItem) {
	sorted := append([]contract.MemoryItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Pinned != sorted[j].Pinned {
			return sorted[i].Pinned
		}
		return sorted[i].OccurredAt > sorted[j].OccurredAt
	})

	ui.Column(c).FillWidth().Gap(6).Children(func() {
		for _, item := range sorted {
			p.memoryRow(c, item)
		}
	})
}

func (p MemoryModalProps) memoryRow(c *ui.Context, item contract.MemoryItem) {
	t := c.Theme()

	row := ui.Column(c).
		FillWidth().
		Padding(10).
		Gap(4).
		Radius(theme.RadiusSM).
		BorderWidth(1).
		BorderColor(t.Border)
	row.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, item.DayKey).FontSize(11).TextColor(t.TextMuted)
			Chip(c, memoryKindLabel(item.Kind), ToneNeutral)
			if item.Pinned {
				Chip(c, "置顶", ToneBrand)
			}
		})

		ui.Text(c, item.Content).FontSize(13).MaxLines(3)
	})
}

func (p MemoryModalProps) refreshState() {
	if p.State.AgentID != p.AgentID {
		p.State.Loaded = false
		p.State.AgentID = p.AgentID
	}
	if p.Open == nil || !*p.Open {
		p.State.Loaded = false
		return
	}
	if p.State.Loaded && time.Since(p.State.RefreshedAt) < 2*time.Second {
		return
	}

	// Agent 上下文只加载专属记忆；用户全局记忆只在主助手的面板里出现。
	p.State.UserItems = nil
	p.State.AgentItems = nil
	p.State.AgentEnabled = true
	if p.AgentID != "" {
		p.State.AgentEnabled = p.Service.MemoryEnabled(p.AgentID)
		p.State.AgentItems = p.Service.Memories("agent", p.AgentID)
	} else {
		p.State.GlobalEnabled = p.Service.MemoryEnabled("")
		p.State.UserItems = p.Service.Memories("user", "")
	}
	p.State.Loaded = true
	p.State.RefreshedAt = time.Now()
}

// invalidate 让下一帧重新读取数据，并通知页面刷新列表。
func (p MemoryModalProps) invalidate() {
	if p.State != nil {
		p.State.Loaded = false
	}
	p.notify()
}

func (p MemoryModalProps) notify() {
	if p.OnChanged != nil {
		p.OnChanged()
	}
}

func memoryKindLabel(kind string) string {
	switch kind {
	case "profile":
		return "背景"
	case "preference":
		return "偏好"
	case "fact":
		return "事实"
	case "decision":
		return "决策"
	case "event":
		return "事件"
	default:
		return "记忆"
	}
}
