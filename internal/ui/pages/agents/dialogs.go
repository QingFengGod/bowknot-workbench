package agents

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
)

func (p *Page) renameModal(c *ui.Context) {
	t := c.Theme()

	canSave := strings.TrimSpace(p.renameValue) != ""
	save := func() {
		if p.svc != nil {
			if err := p.svc.RenameSession(p.renameID, p.renameValue); err != nil {
				p.renameErr = err.Error()
				return
			}
			p.reloadSessions()
		}
		p.renameOpen = false
	}

	components.Modal(c, components.ModalProps{
		Open:  &p.renameOpen,
		Title: "重命名会话",
		Width: 420,
		Footer: func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.renameOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:    "保存",
				Variant:  components.ButtonPrimary,
				Disabled: !canSave,
			}).Clicked() {
				save()
			}
		},
	}, func() {
		input := ui.TextInput(c, &p.renameValue).Placeholder("会话名称").MaxLength(30)
		if input.Changed() {
			p.renameErr = ""
		}
		if p.renameErr != "" {
			ui.Text(c, p.renameErr).FontSize(12).TextColor(t.Danger)
		}
		if input.Submitted() && canSave {
			save()
		}
	})
}

// agentDialog 是新建 / 编辑 Agent 的表单。
func (p *Page) agentDialog(c *ui.Context) {
	t := c.Theme()

	title := "新建 Agent"
	if p.editAgentID != "" {
		title = "编辑 Agent"
	}
	canSave := strings.TrimSpace(p.agentName) != ""

	components.Modal(c, components.ModalProps{
		Open:  &p.agentDialogOpen,
		Title: title,
		Width: 480,
		Footer: func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.agentDialogOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:    "保存",
				Variant:  components.ButtonPrimary,
				Disabled: !canSave,
			}).Clicked() {
				p.submitAgent()
			}
		},
	}, func() {
		ui.Text(c, "名称").FontSize(12).Bold().TextColor(t.TextMuted)
		nameInput := ui.TextInput(c, &p.agentName).Placeholder("例如：写作助手").MaxLength(20)
		if nameInput.Changed() {
			p.agentErr = ""
		}

		ui.Text(c, "描述").FontSize(12).Bold().TextColor(t.TextMuted)
		ui.TextInput(c, &p.agentDesc).Placeholder("一句话说明它负责什么").MaxLength(40)

		ui.Text(c, "人设").FontSize(12).Bold().TextColor(t.TextMuted)
		ui.TextAreaBase(c, &p.agentPersona).
			Placeholder("描述它的身份、语气与行为边界").
			Lines(4, 8).
			FillWidth().
			MaxLength(300)

		if p.agentErr != "" {
			ui.Text(c, p.agentErr).FontSize(12).TextColor(t.Danger)
		}

		if nameInput.Submitted() && canSave {
			p.submitAgent()
		}
	})
}

func (p *Page) openAgentDialog(id string) {
	p.editAgentID = id
	p.agentErr = ""
	if agent := p.agentByID(id); agent != nil {
		p.agentName = agent.Name
		p.agentDesc = agent.Description
		p.agentPersona = agent.Persona
	} else {
		p.agentName = ""
		p.agentDesc = ""
		p.agentPersona = ""
	}
	p.agentDialogOpen = true
}

func (p *Page) submitAgent() {
	if p.svc == nil {
		return
	}
	if p.editAgentID != "" {
		if err := p.svc.UpdateAgent(p.editAgentID, p.agentName, p.agentDesc, p.agentPersona); err != nil {
			p.agentErr = err.Error()
			return
		}
	} else {
		agent := p.svc.CreateAgent(p.agentName, p.agentDesc, p.agentPersona)
		p.reloadAgents()
		p.selectItem("agent:" + agent.ID)
	}
	p.reloadAgents()
	p.agentDialogOpen = false
}

// groupDialog 是新建 / 编辑群聊的表单。
func (p *Page) groupDialog(c *ui.Context) {
	t := c.Theme()

	title := "新建群聊"
	if p.editGroupID != "" {
		title = "编辑群聊成员"
	}
	canSave := strings.TrimSpace(p.groupName) != "" && p.checkedMembers() != nil

	components.Modal(c, components.ModalProps{
		Open:  &p.groupDialogOpen,
		Title: title,
		Width: 480,
		Footer: func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.groupDialogOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:    "保存",
				Variant:  components.ButtonPrimary,
				Disabled: !canSave,
			}).Clicked() {
				p.submitGroup()
			}
		},
	}, func() {
		ui.Text(c, "群聊名称").FontSize(12).Bold().TextColor(t.TextMuted)
		nameInput := ui.TextInput(c, &p.groupName).Placeholder("例如：周报小组").MaxLength(20)
		if nameInput.Changed() {
			p.groupErr = ""
		}

		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "选择成员").FontSize(12).Bold().TextColor(t.TextMuted)
			ui.Text(c, "主 Agent 默认在群内").FontSize(12).TextColor(t.TextMuted)
		})

		ui.Scroll(c).FillWidth().Height(240).Children(func() {
			ui.Column(c).FillWidth().Padding(4).Gap(2).Children(func() {
				if len(p.agents) == 0 {
					ui.Text(c, "还没有可选的 Agent。").FontSize(12).TextColor(t.TextMuted).Padding(12)
					return
				}
				for _, agent := range p.agents {
					p.memberRow(c, agent)
				}
			})
		})

		if p.groupErr != "" {
			ui.Text(c, p.groupErr).FontSize(12).TextColor(t.Danger)
		}

		if nameInput.Submitted() && canSave {
			p.submitGroup()
		}
	})
}

func (p *Page) memberRow(c *ui.Context, agent contract.AgentInfo) {
	t := c.Theme()
	checked := p.groupMembers[agent.ID]
	if checked == nil {
		checked = boolValue(false)
		p.groupMembers[agent.ID] = checked
	}

	row := ui.Row(c).
		FillWidth().
		Padding(8, 10).
		Radius(8).
		Gap(10).
		AlignItems(ui.Center)
	if row.Hovered() {
		row.Background(t.SurfaceHover)
	}

	row.Children(func() {
		ui.Checkbox(c, checked, "")
		components.Avatar(c, agent.AvatarText, 26)
		ui.Column(c).Gap(1).Children(func() {
			ui.Text(c, agent.Name).FontSize(13).Bold()
			ui.Text(c, agent.Description).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		})
	})
}

func (p *Page) checkedMembers() []string {
	var ids []string
	for _, agent := range p.agents {
		if checked := p.groupMembers[agent.ID]; checked != nil && *checked {
			ids = append(ids, agent.ID)
		}
	}
	return ids
}

func (p *Page) openGroupDialog(id string) {
	p.editGroupID = id
	p.groupErr = ""

	var members []string
	if group := p.groupByID(id); group != nil {
		p.groupName = group.Name
		members = group.MemberIDs
	} else {
		p.groupName = ""
		members = nil
	}

	p.groupMembers = map[string]*bool{}
	for _, agent := range p.agents {
		checked := false
		for _, member := range members {
			if member == agent.ID {
				checked = true
				break
			}
		}
		p.groupMembers[agent.ID] = &checked
	}
	p.groupDialogOpen = true
}

func (p *Page) submitGroup() {
	if p.svc == nil {
		return
	}
	members := p.checkedMembers()
	if p.editGroupID != "" {
		if err := p.svc.UpdateGroup(p.editGroupID, p.groupName, members); err != nil {
			p.groupErr = err.Error()
			return
		}
	} else {
		group := p.svc.CreateGroup(p.groupName, members)
		p.reloadAgents()
		p.selectItem("group:" + group.ID)
	}
	p.reloadAgents()
	p.groupDialogOpen = false
}

// deleteModal 确认删除 Agent 或解散群聊。
func (p *Page) deleteModal(c *ui.Context) {
	t := c.Theme()

	item := p.panelItemByKey(p.deleteKey)
	if item == nil {
		p.deleteOpen = false
	}

	title, content, okLabel := "删除 Agent", "", "删除"
	if item != nil {
		content = fmt.Sprintf("确定删除「%s」吗？该 Agent 的会话记录将一并移除。", item.name)
		if item.kind == contract.ItemKindGroup {
			title, content, okLabel = "解散群聊",
				fmt.Sprintf("确定解散「%s」吗？解散后群聊消息将无法恢复。", item.name), "解散"
		}
	}

	props := components.ModalProps{
		Open:  &p.deleteOpen,
		Title: title,
		Width: 420,
	}
	if item != nil {
		props.Footer = func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.deleteOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:   okLabel,
				Variant: components.ButtonDanger,
			}).Clicked() {
				p.removeItem(*item)
				p.deleteOpen = false
			}
		}
	}

	components.Modal(c, props, func() {
		if item == nil {
			return
		}
		ui.Text(c, content).FontSize(13).TextColor(t.TextMuted)
	})
}

func (p *Page) removeItem(item panelItem) {
	if p.svc == nil {
		return
	}
	var err error
	if item.kind == contract.ItemKindAgent {
		err = p.svc.RemoveAgent(item.id)
	} else {
		err = p.svc.RemoveGroup(item.id)
	}
	if err != nil {
		p.sendErr = err.Error()
		return
	}

	delete(p.streams, item.key)
	if p.activeKey == item.key {
		p.activeKey = ""
		p.messages = nil
		p.sessions = nil
		p.activeSessionID = ""
	}
	p.reloadAgents()
}

// deleteMessageModal 确认删除单条消息。
func (p *Page) deleteMessageModal(c *ui.Context) {
	t := c.Theme()

	components.Modal(c, components.ModalProps{
		Open:  &p.deleteMessageOpen,
		Title: "删除消息",
		Width: 420,
		Footer: func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.deleteMessageOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:   "删除",
				Variant: components.ButtonDanger,
			}).Clicked() {
				conv := p.conversationID()
				if p.svc != nil && conv != "" {
					if p.streams[conv] == p.deleteMessageID {
						p.stop(conv)
					}
					if err := p.svc.DeleteMessage(conv, p.deleteMessageID); err != nil {
						p.sendErr = err.Error()
					}
				}
				p.deleteMessageOpen = false
				p.deleteMessageID = ""
				p.reloadMessages()
			}
		},
	}, func() {
		ui.Text(c, "删除后无法恢复，确定删除这条消息吗？").
			FontSize(13).
			TextColor(t.TextMuted)
	})
}

// ---------- 查找 ----------

func (p *Page) agentByID(id string) *contract.AgentInfo {
	for i := range p.agents {
		if p.agents[i].ID == id {
			return &p.agents[i]
		}
	}
	return nil
}

func (p *Page) groupByID(id string) *contract.AgentGroup {
	for i := range p.groups {
		if p.groups[i].ID == id {
			return &p.groups[i]
		}
	}
	return nil
}

// panelItemByKey 按 key 重建列表条目，找不到时返回 nil。
func (p *Page) panelItemByKey(key string) *panelItem {
	for _, agent := range p.agents {
		if "agent:"+agent.ID == key {
			item := p.agentItem(agent)
			return &item
		}
	}
	for _, group := range p.groups {
		if "group:"+group.ID == key {
			item := p.groupItem(group)
			return &item
		}
	}
	return nil
}
