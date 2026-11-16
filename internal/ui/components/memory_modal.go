package components

import (
	"fmt"
	"sort"
	"strings"
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

// MemoryModalProps 描述长期记忆面板及其子弹窗。
// 所有开关与编辑状态由页面持有，组件只读写稳定指针。
type MemoryModalProps struct {
	Service contract.MemoryService
	Open    *bool
	// State 是页面的数据缓存，组件在失效时刷新。
	State *MemoryPanelState
	// AgentID 非空时同时展示该 Agent 的专属记忆与开关。
	AgentID   string
	AgentName string

	EditOpen  *bool
	EditID    *string
	EditValue *string
	EditErr   *string
	ClearOpen *bool

	// OnChanged 在记忆或开关变化后触发，页面可据此刷新消息的「已记住」标记。
	OnChanged func()
}

// MemoryModal 是长期记忆管理弹窗：开关、列表、编辑、删除、置顶与清空。
func MemoryModal(c *ui.Context, p MemoryModalProps) {
	t := c.Theme()
	if p.Service == nil || p.Open == nil || p.EditOpen == nil || p.ClearOpen == nil || p.State == nil {
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
		p.toggleRow(c, "允许长期记忆（全局）", "关闭后不再抽取和注入用户级记忆", p.State.GlobalEnabled, func(enabled bool) {
			_ = p.Service.SetMemoryEnabled("", enabled)
			p.invalidate()
		})
		if p.AgentID != "" {
			name := p.AgentName
			if name == "" {
				name = "当前 Agent"
			}
			p.toggleRow(c, "允许长期记忆（"+name+"）", "只影响该 Agent 的专属记忆", p.State.AgentEnabled, func(enabled bool) {
				_ = p.Service.SetMemoryEnabled(p.AgentID, enabled)
				p.invalidate()
			})
		}

		ui.Box(c).FillWidth().Height(1).Background(t.Border)

		if len(userItems) == 0 && len(agentItems) == 0 {
			ui.Column(c).FillWidth().Padding(24).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, "还没有长期记忆").FontSize(13).Bold()
				ui.Text(c, "在对话里表达偏好或重要事实，这里会自动出现记忆。").FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
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

	// 编辑弹窗
	Modal(c, ModalProps{Open: p.EditOpen, Title: "编辑记忆", Width: 480, Footer: func() {
		cancel := Button(c, ButtonProps{Label: "取消", Variant: ButtonSecondary})
		if cancel.Clicked() {
			*p.EditOpen = false
		}
		save := Button(c, ButtonProps{Label: "保存", Variant: ButtonPrimary})
		if save.Clicked() {
			p.saveEdit()
		}
	}}, func() {
		if p.EditValue == nil || p.EditID == nil {
			return
		}
		ui.Text(c, "记忆内容").FontSize(12).Bold().TextColor(t.TextMuted)
		input := ui.TextInput(c, p.EditValue).Placeholder("用一句话描述需要长期保留的事实").MaxLength(200)
		if input.Changed() && p.EditErr != nil {
			*p.EditErr = ""
		}
		if p.EditErr != nil && *p.EditErr != "" {
			ui.Text(c, *p.EditErr).FontSize(12).TextColor(t.Danger)
		}
		if input.Submitted() {
			p.saveEdit()
		}
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
		ui.Text(c, "清空后所有长期记忆与摘要都会删除，且无法恢复。").FontSize(13)
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
		ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, item.DayKey).FontSize(11).TextColor(t.TextMuted)
				Chip(c, memoryKindLabel(item.Kind), ToneNeutral)
				if item.Pinned {
					Chip(c, "置顶", ToneBrand)
				}
			})

			ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
				pinLabel := "置顶"
				if item.Pinned {
					pinLabel = "取消置顶"
				}
				if TextButton(c, pinLabel, false).Clicked() {
					_ = p.Service.SetPinned(item.ID, !item.Pinned)
					p.invalidate()
				}
				if TextButton(c, "编辑", false).Clicked() {
					if p.EditID != nil && p.EditValue != nil {
						*p.EditID = item.ID
						*p.EditValue = item.Content
						if p.EditErr != nil {
							*p.EditErr = ""
						}
						*p.EditOpen = true
					}
				}
				if TextButton(c, "删除", true).Clicked() {
					_ = p.Service.DeleteMemory(item.ID)
					p.invalidate()
				}
			})
		})

		ui.Text(c, item.Content).FontSize(13).MaxLines(3)
	})
}

func (p MemoryModalProps) saveEdit() {
	if p.EditID == nil || p.EditValue == nil || p.EditOpen == nil {
		return
	}
	content := strings.TrimSpace(*p.EditValue)
	if content == "" {
		if p.EditErr != nil {
			*p.EditErr = "记忆内容不能为空"
		}
		return
	}
	if err := p.Service.UpdateMemory(*p.EditID, content); err != nil {
		if p.EditErr != nil {
			*p.EditErr = err.Error()
		}
		return
	}
	*p.EditOpen = false
	p.invalidate()
}

// refreshState 在缓存失效时重新读取记忆与开关；
// 面板打开期间每 2 秒刷新一次，确保异步抽取的新记忆能及时出现。
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

	p.State.GlobalEnabled = p.Service.MemoryEnabled("")
	p.State.UserItems = p.Service.Memories("user", "")
	p.State.AgentItems = nil
	p.State.AgentEnabled = true
	if p.AgentID != "" {
		p.State.AgentEnabled = p.Service.MemoryEnabled(p.AgentID)
		p.State.AgentItems = p.Service.Memories("agent", p.AgentID)
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
