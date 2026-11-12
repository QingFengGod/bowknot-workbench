package agents

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/format"
)

// markdownOptions 沿用助手页的正文行高。
var markdownOptions = &markdown.Options{
	Theme: &markdown.Theme{LineHeight: 1.7},
}

// Hooks 是页面依赖的外部动作，由壳层注入。
type Hooks struct {
	// Update 在主线程修改页面状态并触发重绘，供后台流式事件使用。
	Update func(fn func())
}

// Page 是 agents 页的原生实现：左侧 Agent / 群聊列表，中间对话区，
// 右侧为选中 Agent 的会话列表（群聊只有一个线程，不显示会话列）。
type Page struct {
	svc   contract.AgentService
	hooks Hooks

	agents     []contract.AgentInfo
	groups     []contract.AgentGroup
	models     []contract.ChatModel
	modelLabel string

	activeKey       string // "agent:xxx" 或 "group:yyy"
	sessions        []contract.AgentSession
	activeSessionID string
	messages        []contract.ChatMessage

	draft      string
	sendErr    string
	focusDraft bool

	panelGroupOpen    map[string]*bool
	sessionGroupOpen  map[string]*bool
	panelScroll       ui.ScrollState
	chatScroll        ui.ScrollState
	sessionScroll     ui.ScrollState
	sessionsCollapsed bool
	// follow 为 true 时消息列表贴着底部，用户上滚后关闭，回到底部再恢复。
	follow   bool
	lastMaxY float32

	// streams 记录正在生成的会话与消息。
	streams map[string]string

	renameOpen  bool
	renameID    string
	renameValue string
	renameErr   string

	agentDialogOpen bool
	editAgentID     string
	agentName       string
	agentDesc       string
	agentPersona    string
	agentErr        string

	groupDialogOpen bool
	editGroupID     string
	groupName       string
	groupMembers    map[string]*bool
	groupErr        string

	deleteOpen bool
	deleteKey  string

	deleteMessageOpen bool
	deleteMessageID   string
}

// New 创建 agents 页。
func New(svc contract.AgentService, hooks Hooks) *Page {
	p := &Page{
		svc:     svc,
		hooks:   hooks,
		streams: map[string]string{},
		panelGroupOpen: map[string]*bool{
			"pinned": boolValue(true),
			"agent":  boolValue(true),
			"group":  boolValue(true),
		},
		sessionGroupOpen: map[string]*bool{
			"pinned":    boolValue(true),
			"today":     boolValue(true),
			"yesterday": boolValue(false),
			"week":      boolValue(false),
			"earlier":   boolValue(false),
		},
	}
	p.reloadAgents()
	p.reloadModels()
	return p
}

func (p *Page) View(c *ui.Context) {
	t := c.Theme()

	p.followEnd()

	ui.Row(c).Fill().Background(t.Background).Children(func() {
		p.panel(c)

		ui.Column(c).Grow(1).FillHeight().Children(func() {
			if p.activeKey == "" {
				p.emptyHint(c)
				return
			}
			p.chatHeader(c)
			p.messageList(c)
			p.composer(c)
		})

		if p.activeAgent() != nil && !p.sessionsCollapsed {
			p.sessionColumn(c)
		}
	})

	p.renameModal(c)
	p.agentDialog(c)
	p.groupDialog(c)
	p.deleteModal(c)
	p.deleteMessageModal(c)
}

// emptyHint 是未选择任何条目时的占位，与 Web 端一致。
func (p *Page) emptyHint(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
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
				components.Icon(c, components.IconAgents, 40)
			})
		ui.Text(c, "选择一个 Agent 或群聊").FontSize(16).Bold()
		ui.Text(c, "左侧列表里点一个条目，就能直接开始对话。").FontSize(12).TextColor(t.TextMuted)
	})
}

func (p *Page) chatHeader(c *ui.Context) {
	t := c.Theme()

	ui.Row(c).
		FillWidth().
		Height(58).
		Shrink(0).
		Padding(0, 16).
		Background(t.Surface).
		BorderWidth(0, 0, 1, 0).
		BorderColor(t.Border).
		AlignItems(ui.Center).
		Justify(ui.SpaceBetween).
		Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				p.entityAvatar(c, 34)
				ui.Column(c).Gap(1).Children(func() {
					ui.Text(c, p.activeName()).FontSize(14).Bold().MaxLines(1).MaxWidth(300)
					ui.Text(c, p.activeDesc()).FontSize(11).TextColor(t.TextMuted)
				})
				if p.streamingActive() {
					components.Chip(c, "正在回复…", components.ToneBrand)
				}
			})

			if p.activeAgent() != nil {
				toggle := iconButton(c, components.IconPanel)
				label := "展开会话列表"
				if !p.sessionsCollapsed {
					label = "收起会话列表"
				}
				if toggle.Tooltip(label).Clicked() {
					p.sessionsCollapsed = !p.sessionsCollapsed
				}
			}
		})
}

// entityAvatar 渲染当前条目的头像：单聊取头像文字，群聊叠放成员头像。
func (p *Page) entityAvatar(c *ui.Context, size float32) {
	if agent := p.activeAgent(); agent != nil {
		components.Avatar(c, agent.AvatarText, size)
		return
	}
	group := p.activeGroup()
	if group == nil {
		return
	}

	members := group.MemberIDs
	if len(members) > 3 {
		members = members[:3]
	}
	ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
		for _, id := range members {
			text := "?"
			for _, agent := range p.agents {
				if agent.ID == id {
					text = agent.AvatarText
					break
				}
			}
			components.Avatar(c, text, size*0.62)
		}
		components.Avatar(c, "助", size*0.62)
	})
}

func (p *Page) messageList(c *ui.Context) {
	ui.Column(c).Grow(1).Children(func() {
		if len(p.messages) == 0 {
			group := p.activeGroup()
			components.EmptyState(c, components.EmptyProps{
				Icon:  components.IconMessage,
				Title: fmt.Sprintf("和%s打个招呼吧", p.activeName()),
				Hint: map[bool]string{
					true:  "成员会分别发言，主 Agent 负责汇总结果。",
					false: "回复会以流式方式逐步出现，可以随时停止或重答。",
				}[group != nil],
			})
			return
		}

		ui.Scroll(c).Grow(1).TrackScroll(&p.chatScroll).Children(func() {
			ui.Column(c).FillWidth().Padding(20, 24).Gap(18).Children(func() {
				for _, message := range p.messages {
					p.messageItem(c, message)
				}
			})
		})

		if !p.follow && p.chatScroll.MaxY > 0 {
			back := components.Button(c, components.ButtonProps{
				Label:   "回到底部",
				Variant: components.ButtonSecondary,
			})
			back.Absolute().Right(20).Bottom(14)
			if back.Clicked() {
				p.stickToBottom()
			}
		}
	})
}

func (p *Page) messageItem(c *ui.Context, m contract.ChatMessage) {
	isUser := m.Role == contract.RoleUser

	item := ui.Column(c).FillWidth().Gap(3)
	if isUser {
		item.AlignItems(ui.End)
	}
	hovered := item.Hovered()

	item.Children(func() {
		row := ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Start)
		if isUser {
			row.Justify(ui.End)
		}
		row.Children(func() {
			if !isUser {
				if m.SenderAvatar != "" {
					components.Avatar(c, m.SenderAvatar, 32)
				} else {
					components.Avatar(c, "助", 32)
				}
			}

			column := ui.Column(c).Gap(3)
			if isUser {
				column.MaxWidth(620).AlignItems(ui.End)
			} else {
				column.Grow(1).MaxWidth(760)
			}

			column.Children(func() {
				if m.SenderName != "" {
					ui.Text(c, m.SenderName).FontSize(11).Bold().TextColor(c.Theme().TextMuted)
				}
				p.bubble(c, m, isUser)
				p.messageMeta(c, m, isUser)
				p.messageActions(c, m, hovered)
			})
		})
	})
}

func (p *Page) bubble(c *ui.Context, m contract.ChatMessage, isUser bool) {
	t := c.Theme()

	bubble := ui.Column(c).Radius(12).Gap(0)
	if isUser {
		bubble.Padding(10, 14).Background(t.Accent.Alpha(0.1))
	} else {
		bubble.FillWidth()
	}

	bubble.Children(func() {
		if m.Status == contract.MessageStreaming {
			markdown.Stream(c, m.Content, markdownOptions)
		} else {
			markdown.View(c, m.Content, markdownOptions)
		}

		switch m.Status {
		case contract.MessageStreaming:
			ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
				ui.Box(c).Width(6).Height(6).Radius(3).Background(t.Accent)
				ui.Text(c, "正在生成").FontSize(11).TextColor(t.TextMuted)
				if textButton(c, "停止生成", false).Clicked() {
					p.stop(p.conversationID())
				}
			})
		case contract.MessageFailed:
			errText := m.Error
			if errText == "" {
				errText = "生成失败，请稍后重试"
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
				ui.Text(c, errText).FontSize(11).TextColor(t.Danger).MaxLines(2)
				if textButton(c, "重试", false).Clicked() {
					p.regenerate(m.ID)
				}
			})
		case contract.MessageStopped:
			ui.Text(c, "已停止生成").FontSize(11).TextColor(t.TextMuted).Margin(8, 0, 0, 0)
		}
	})
}

func (p *Page) messageMeta(c *ui.Context, m contract.ChatMessage, isUser bool) {
	t := c.Theme()

	row := ui.Row(c).Gap(8).AlignItems(ui.Center)
	if isUser {
		row.Justify(ui.End)
	}
	row.Children(func() {
		ui.Text(c, format.Clock(m.CreatedAt)).FontSize(11).TextColor(t.TextMuted)
		if m.MemoryWritten {
			ui.Text(c, "已记住").FontSize(11).TextColor(t.Accent)
		}
	})
}

func (p *Page) messageActions(c *ui.Context, m contract.ChatMessage, hovered bool) {
	lastAssistant := m.Role == contract.RoleAssistant && m.ID == p.lastAssistantID()

	row := ui.Row(c).Gap(2).AlignItems(ui.Center)
	if m.Role == contract.RoleUser {
		row.Justify(ui.End)
	}
	if !hovered {
		row.Opacity(0).IgnoreInput(true)
	}

	row.Children(func() {
		if textButton(c, "复制", false).Clicked() && strings.TrimSpace(m.Content) != "" {
			c.Services().WriteClipboard(m.Content)
			c.Toast("已复制")
		}
		if lastAssistant && m.Status != contract.MessageStreaming {
			if textButton(c, "重答", false).Clicked() {
				p.regenerate(m.ID)
			}
		}
		if textButton(c, "删除", true).Clicked() {
			p.deleteMessageID = m.ID
			p.deleteMessageOpen = true
		}
	})
}

func (p *Page) composer(c *ui.Context) {
	t := c.Theme()
	streaming := p.streamingActive()

	ui.Column(c).
		FillWidth().
		Shrink(0).
		Padding(10, 16).
		Gap(8).
		Background(t.Surface).
		BorderWidth(1, 0, 0, 0).
		BorderColor(t.Border).
		Children(func() {
			if p.sendErr != "" {
				ui.Text(c, p.sendErr).FontSize(12).TextColor(t.Danger)
			}

			box := ui.Column(c).
				FillWidth().
				Padding(8, 10).
				Radius(12).
				Background(t.Background).
				BorderWidth(1).
				BorderColor(t.Border).
				Gap(6)
			if box.FocusWithin() {
				box.BorderColor(t.Accent)
			}

			box.Children(func() {
				input := ui.TextAreaBase(c, &p.draft).
					Placeholder(fmt.Sprintf("给 %s 发消息…", p.activeName())).
					Lines(1, 8).
					FillWidth().
					FontSize(14)
				input.OnTextKey(func(k ui.TextKey) bool {
					if k.Key == ui.KeyEnter && k.Mods == 0 {
						if p.sendText(k.Text()) {
							k.SetText("")
						}
						return true
					}
					if k.Key == ui.KeyEscape && streaming {
						p.stop(p.conversationID())
						return true
					}
					return false
				})
				if input.Changed() {
					p.sendErr = ""
				}
				if p.focusDraft {
					input.Focus()
					p.focusDraft = false
				}

				ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					modelMenu := ui.PopoverMenuButton(c, p.modelLabel, func(m *ui.Menu) {
						m.MaxWidth(280)
						for _, model := range p.models {
							item := m.Item(model.Name).
								Subtitle(model.Description).
								Radio(model.Name == p.modelLabel)
							if item.Chosen() {
								p.modelLabel = model.Name
							}
						}
					})
					modelMenu.BorderWidth(0).
						Background(ui.Transparent).
						Height(26).
						PaddingX(8).
						Radius(6).
						FontSize(12).
						TextColor(t.TextMuted)
					if modelMenu.Hovered() {
						modelMenu.Background(t.SurfaceHover).TextColor(t.Text)
					}

					if streaming {
						stop := circleButton(c, components.IconStop, t.Danger, t.AccentText, true)
						if stop.Tooltip("停止生成").Clicked() {
							p.stop(p.conversationID())
						}
					} else {
						enabled := strings.TrimSpace(p.draft) != ""
						send := circleButton(c, components.IconSend, t.Accent, t.AccentText, enabled)
						if send.Tooltip("发送").Clicked() && enabled {
							p.send()
						}
					}
				})
			})
		})
}

// panel 是左侧的 Agent / 群聊列表。
func (p *Page) panel(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).
		Width(300).
		FillHeight().
		Shrink(0).
		Background(t.Surface).
		BorderWidth(0, 1, 0, 0).
		BorderColor(t.Border).
		Children(func() {
			ui.Row(c).
				FillWidth().
				Height(58).
				Padding(0, 12).
				AlignItems(ui.Center).
				Justify(ui.SpaceBetween).
				Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Agents").FontSize(13).Bold()
						components.Chip(c, fmt.Sprint(len(p.agents)+len(p.groups)), components.ToneNeutral)
					})

					add := ui.ButtonBase(c).
						Width(26).
						Height(26).
						Radius(6).
						AlignItems(ui.Center).
						Justify(ui.Center).
						TextColor(t.TextMuted)
					if add.Hovered() {
						add.Background(t.SurfaceHover).TextColor(t.Accent)
					}
					add.Children(func() {
						components.Icon(c, components.IconPlus, 14)
					})
					add.Tooltip("新建")
					add.PopoverMenuButton(func(m *ui.Menu) {
						if m.Item("新建 Agent").Chosen() {
							p.openAgentDialog("")
						}
						if m.Item("新建群聊").Chosen() {
							p.openGroupDialog("")
						}
					})
				})

			ui.Scroll(c).Grow(1).TrackScroll(&p.panelScroll).Children(func() {
				ui.Column(c).FillWidth().Padding(8).Gap(8).Children(func() {
					for _, group := range p.panelGroups() {
						p.panelGroup(c, group)
					}
				})
			})
		})
}

type panelGroup struct {
	key   string
	title string
	items []panelItem
}

// panelItem 是左侧列表的统一条目。
type panelItem struct {
	key      string
	kind     string // contract.ItemKindAgent / ItemKindGroup
	id       string
	name     string
	desc     string
	avatar   string
	members  []string
	avatarOf func(id string) string
	pinned   bool
}

func (p *Page) panelGroups() []panelGroup {
	titles := map[string]string{
		"pinned": "置顶",
		"agent":  "Agents",
		"group":  "群聊",
	}

	groups := make([]panelGroup, 0, 3)
	for _, key := range []string{"pinned", "agent", "group"} {
		group := panelGroup{key: key, title: titles[key]}
		for _, agent := range p.agents {
			if key == "pinned" && agent.Pinned || key == "agent" && !agent.Pinned {
				group.items = append(group.items, p.agentItem(agent))
			}
		}
		for _, grp := range p.groups {
			if key == "pinned" && grp.Pinned || key == "group" && !grp.Pinned {
				group.items = append(group.items, p.groupItem(grp))
			}
		}
		if len(group.items) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func (p *Page) agentItem(agent contract.AgentInfo) panelItem {
	return panelItem{
		key:    "agent:" + agent.ID,
		kind:   contract.ItemKindAgent,
		id:     agent.ID,
		name:   agent.Name,
		desc:   agent.Description,
		avatar: agent.AvatarText,
		pinned: agent.Pinned,
	}
}

func (p *Page) groupItem(group contract.AgentGroup) panelItem {
	return panelItem{
		key:     "group:" + group.ID,
		kind:    contract.ItemKindGroup,
		id:      group.ID,
		name:    group.Name,
		desc:    fmt.Sprintf("%d 个 Agent · 主 Agent 主导", len(group.MemberIDs)+1),
		members: group.MemberIDs,
		pinned:  group.Pinned,
	}
}

func (p *Page) panelGroup(c *ui.Context, group panelGroup) {
	t := c.Theme()
	open := p.panelGroupOpen[group.key]
	if open == nil {
		open = boolValue(true)
		p.panelGroupOpen[group.key] = open
	}

	parts := ui.CollapsibleBase(c, open)

	trigger := parts.Trigger.
		FillWidth().
		Height(24).
		Padding(0, 4).
		Radius(4).
		AlignItems(ui.Center).
		Justify(ui.SpaceBetween).
		TextColor(t.TextMuted)
	if trigger.Hovered() {
		trigger.Background(t.SurfaceHover)
	}
	trigger.Children(func() {
		ui.Text(c, group.title).FontSize(11).Bold().TextColor(t.TextMuted)
		components.Icon(c, components.IconChevron, 11).Rotate(90 * parts.Progress())
	})

	parts.Panel(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			for _, item := range group.items {
				p.panelRow(c, item)
			}
		})
	})
}

func (p *Page) panelRow(c *ui.Context, item panelItem) {
	t := c.Theme()
	active := item.key == p.activeKey

	row := ui.Row(c).
		FillWidth().
		Padding(6, 8).
		Radius(8).
		Gap(8).
		AlignItems(ui.Center).
		BorderWidth(0, 0, 0, 3).
		BorderColor(ui.Transparent)
	switch {
	case active:
		row.Background(t.Accent.Alpha(0.1)).BorderColor(t.Accent)
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}

	handled := false
	row.Children(func() {
		p.itemAvatar(c, item)

		ui.Column(c).Grow(1).Gap(1).Children(func() {
			nameText := ui.Text(c, item.name).FontSize(13).Bold().MaxLines(1)
			if active {
				nameText.TextColor(t.Accent)
			}
			ui.Text(c, item.desc).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		})

		more := ui.ButtonBase(c).
			Width(20).
			Height(20).
			Radius(4).
			AlignItems(ui.Center).
			Justify(ui.Center).
			TextColor(t.TextMuted)
		if more.Hovered() {
			more.Background(t.SurfacePressed).TextColor(t.Text)
		}
		more.Children(func() {
			components.Icon(c, components.IconMore, 13)
		})
		more.PopoverMenuButton(func(m *ui.Menu) {
			editLabel := "编辑人设"
			if item.kind == contract.ItemKindGroup {
				editLabel = "编辑成员"
			}
			if m.Item(editLabel).Chosen() {
				p.editItem(item)
				handled = true
			}
			pinLabel := "置顶"
			if item.pinned {
				pinLabel = "取消置顶"
			}
			if m.Item(pinLabel).Chosen() {
				p.togglePin(item)
				handled = true
			}
			m.Separator()
			removeLabel := "删除"
			if item.kind == contract.ItemKindGroup {
				removeLabel = "解散群聊"
			}
			if m.Item(removeLabel).Danger().Chosen() {
				p.deleteKey = item.key
				p.deleteOpen = true
				handled = true
			}
		})
		more.Tooltip("更多操作")
		if more.Clicked() {
			handled = true
		}
	})

	if row.Clicked() && !handled {
		p.selectItem(item.key)
	}
}

// itemAvatar 渲染条目头像：单聊取头像文字，群聊显示成员头像组。
func (p *Page) itemAvatar(c *ui.Context, item panelItem) {
	if item.kind == contract.ItemKindAgent {
		components.Avatar(c, item.avatar, 34)
		return
	}

	members := item.members
	if len(members) > 3 {
		members = members[:3]
	}
	ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
		for _, id := range members {
			text := "?"
			for _, agent := range p.agents {
				if agent.ID == id {
					text = agent.AvatarText
					break
				}
			}
			components.Avatar(c, text, 21)
		}
		components.Avatar(c, "助", 21)
	})
}

// sessionColumn 是右侧的会话列表，仅单聊 Agent 显示。
func (p *Page) sessionColumn(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).
		Width(300).
		FillHeight().
		Shrink(0).
		Background(t.Surface).
		BorderWidth(0, 0, 0, 1).
		BorderColor(t.Border).
		Children(func() {
			ui.Row(c).
				FillWidth().
				Height(58).
				Padding(0, 12).
				BorderWidth(0, 0, 1, 0).
				BorderColor(t.Border).
				AlignItems(ui.Center).
				Justify(ui.SpaceBetween).
				Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "会话").FontSize(13).Bold()
						components.Chip(c, fmt.Sprint(len(p.sessions)), components.ToneNeutral)
					})

					ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
						if components.Button(c, components.ButtonProps{
							Label:   "新建",
							Variant: components.ButtonGhost,
							Width:   56,
						}).Clicked() {
							p.createSession()
						}
						if iconButton(c, components.IconPanel).Tooltip("收起会话列表").Clicked() {
							p.sessionsCollapsed = true
						}
					})
				})

			if len(p.sessions) == 0 {
				ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "还没有会话").FontSize(13).Bold()
					ui.Text(c, "新建一个会话开始对话。").FontSize(11).TextColor(t.TextMuted)
				})
				return
			}

			ui.Scroll(c).Grow(1).TrackScroll(&p.sessionScroll).Children(func() {
				ui.Column(c).FillWidth().Padding(8).Gap(8).Children(func() {
					for _, group := range p.sessionGroups() {
						p.sessionGroup(c, group)
					}
				})
			})
		})
}

type sessionGroup struct {
	key      string
	title    string
	sessions []contract.AgentSession
}

// sessionGroups 按置顶和更新时间分组，顺序与助手页一致。
func (p *Page) sessionGroups() []sessionGroup {
	sorted := append([]contract.AgentSession(nil), p.sessions...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].UpdatedAt > sorted[j].UpdatedAt
	})

	titles := map[string]string{
		"today":     "今天",
		"yesterday": "昨天",
		"week":      "近 7 天",
		"earlier":   "更早",
	}

	groups := make([]sessionGroup, 0, 5)
	pinned := sessionGroup{key: "pinned", title: "置顶"}
	buckets := map[string]*sessionGroup{}
	for _, session := range sorted {
		if session.Pinned {
			pinned.sessions = append(pinned.sessions, session)
			continue
		}
		key := dayBucket(session.UpdatedAt)
		group := buckets[key]
		if group == nil {
			group = &sessionGroup{key: key, title: titles[key]}
			buckets[key] = group
		}
		group.sessions = append(group.sessions, session)
	}

	if len(pinned.sessions) > 0 {
		groups = append(groups, pinned)
	}
	for _, key := range []string{"today", "yesterday", "week", "earlier"} {
		if group := buckets[key]; group != nil && len(group.sessions) > 0 {
			groups = append(groups, *group)
		}
	}
	return groups
}

func dayBucket(ms int64) string {
	if ms == 0 {
		return "earlier"
	}
	startOfDay := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	}
	now := time.Now()
	days := int(math.Round(startOfDay(now).Sub(startOfDay(time.UnixMilli(ms))).Hours() / 24))
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 7:
		return "week"
	default:
		return "earlier"
	}
}

func (p *Page) sessionGroup(c *ui.Context, group sessionGroup) {
	t := c.Theme()
	open := p.sessionGroupOpen[group.key]
	if open == nil {
		open = boolValue(group.key == "pinned" || group.key == "today")
		p.sessionGroupOpen[group.key] = open
	}

	parts := ui.CollapsibleBase(c, open)

	trigger := parts.Trigger.
		FillWidth().
		Height(24).
		Padding(0, 4).
		Radius(4).
		AlignItems(ui.Center).
		Justify(ui.SpaceBetween).
		TextColor(t.TextMuted)
	if trigger.Hovered() {
		trigger.Background(t.SurfaceHover)
	}
	trigger.Children(func() {
		ui.Text(c, group.title).FontSize(11).Bold().TextColor(t.TextMuted)
		components.Icon(c, components.IconChevron, 11).Rotate(90 * parts.Progress())
	})

	parts.Panel(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			for _, session := range group.sessions {
				p.sessionRow(c, session)
			}
		})
	})
}

func (p *Page) sessionRow(c *ui.Context, s contract.AgentSession) {
	t := c.Theme()
	active := s.ID == p.activeSessionID

	row := ui.Column(c).
		FillWidth().
		Padding(7, 8).
		Radius(8).
		Gap(1).
		BorderWidth(0, 0, 0, 3).
		BorderColor(ui.Transparent)
	switch {
	case active:
		row.Background(t.Accent.Alpha(0.1)).BorderColor(t.Accent)
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}

	handled := false
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, s.Title).FontSize(13).Bold().MaxLines(1).Grow(1)
			ui.Text(c, format.Relative(s.UpdatedAt)).FontSize(11).TextColor(t.TextMuted)

			more := ui.ButtonBase(c).
				Width(20).
				Height(20).
				Radius(4).
				AlignItems(ui.Center).
				Justify(ui.Center).
				TextColor(t.TextMuted)
			if more.Hovered() {
				more.Background(t.SurfacePressed).TextColor(t.Text)
			}
			more.Children(func() {
				components.Icon(c, components.IconMore, 13)
			})
			more.PopoverMenuButton(func(m *ui.Menu) {
				if m.Item("重命名").Chosen() {
					p.renameID = s.ID
					p.renameValue = s.Title
					p.renameErr = ""
					p.renameOpen = true
					handled = true
				}
				pinLabel := "置顶"
				if s.Pinned {
					pinLabel = "取消置顶"
				}
				if m.Item(pinLabel).Chosen() {
					p.pinSession(s)
					handled = true
				}
				m.Separator()
				if m.Item("删除").Danger().Chosen() {
					p.deleteSession(s.ID)
					handled = true
				}
			})
			more.Tooltip("更多操作")
			if more.Clicked() {
				handled = true
			}
		})

		summary := strings.TrimSpace(s.Summary)
		if summary == "" {
			summary = "还没有消息"
		}
		ui.Text(c, summary).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
	})

	if row.Clicked() && !handled {
		p.selectSession(s.ID)
	}
}

// ---------- 状态操作 ----------

func (p *Page) selectItem(key string) {
	if key == p.activeKey {
		return
	}
	p.activeKey = key
	p.sendErr = ""
	p.follow = true
	p.lastMaxY = 0
	p.sessionsCollapsed = false

	if agent := p.activeAgent(); agent != nil {
		p.ensureAgentSession(agent.ID)
	} else {
		p.activeSessionID = ""
		p.sessions = nil
		p.reloadMessages()
	}
	p.focusDraft = true
}

// ensureAgentSession 进入 Agent 时恢复上次会话，没有会话则自动建一个。
func (p *Page) ensureAgentSession(agentID string) {
	if p.svc == nil {
		return
	}
	sessions := p.svc.Sessions(agentID)
	if len(sessions) == 0 {
		p.svc.CreateSession(agentID)
		p.reloadSessions()
		p.selectSession(p.latestSessionID())
		return
	}
	preferred := sessions[0]
	for _, session := range sessions {
		if session.ID == p.activeSessionID {
			preferred = session
			break
		}
		if preferred.ID != p.activeSessionID && session.UpdatedAt > preferred.UpdatedAt {
			preferred = session
		}
	}
	p.activeSessionID = preferred.ID
	p.reloadMessages()
}

func (p *Page) latestSessionID() string {
	latest := ""
	var maxAt int64
	for _, session := range p.sessions {
		if session.UpdatedAt > maxAt {
			maxAt = session.UpdatedAt
			latest = session.ID
		}
	}
	return latest
}

func (p *Page) selectSession(id string) {
	if id == "" || id == p.activeSessionID {
		return
	}
	p.activeSessionID = id
	p.sendErr = ""
	p.reloadMessages()
	p.stickToBottom()
	p.focusDraft = true
}

func (p *Page) createSession() {
	if p.svc == nil || p.activeAgent() == nil {
		return
	}
	p.svc.CreateSession(p.activeAgent().ID)
	p.reloadSessions()
	p.selectSession(p.latestSessionID())
}

func (p *Page) pinSession(s contract.AgentSession) {
	if p.svc == nil {
		return
	}
	if err := p.svc.PinSession(s.ID, !s.Pinned); err != nil {
		p.sendErr = err.Error()
		return
	}
	p.reloadSessions()
}

func (p *Page) deleteSession(id string) {
	if p.svc == nil {
		return
	}
	if err := p.svc.DeleteSession(id); err != nil {
		p.sendErr = err.Error()
		return
	}
	delete(p.streams, id)
	p.reloadSessions()
	if p.activeSessionID == id {
		p.activeSessionID = ""
		p.messages = nil
		p.selectSession(p.latestSessionID())
	}
}

func (p *Page) editItem(item panelItem) {
	if item.kind == contract.ItemKindAgent {
		p.openAgentDialog(item.id)
		return
	}
	p.openGroupDialog(item.id)
}

func (p *Page) togglePin(item panelItem) {
	if p.svc == nil {
		return
	}
	if err := p.svc.Pin(item.kind, item.id, !item.pinned); err != nil {
		p.sendErr = err.Error()
		return
	}
	p.reloadAgents()
}

// ---------- 发送与流式 ----------

func (p *Page) send() {
	if p.sendText(p.draft) {
		p.draft = ""
	}
}

func (p *Page) sendText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || p.svc == nil || p.streamingActive() {
		return false
	}

	conversationID := p.conversationID()
	if conversationID == "" {
		return false
	}
	messageID, events, err := p.svc.Send(conversationID, text, p.modelID())
	if err != nil {
		p.sendErr = err.Error()
		return false
	}

	p.draft = ""
	p.sendErr = ""
	p.focusDraft = true
	p.streams[conversationID] = messageID
	p.reloadMessages()
	p.reloadSessions()
	p.reloadAgents()
	p.stickToBottom()
	p.consume(conversationID, events)
	return true
}

func (p *Page) regenerate(messageID string) {
	if p.svc == nil {
		return
	}
	if p.streamingActive() {
		p.sendErr = "请先停止当前生成"
		return
	}
	conversationID := p.conversationID()
	if conversationID == "" {
		return
	}
	events, err := p.svc.Regenerate(conversationID, messageID)
	if err != nil {
		p.sendErr = err.Error()
		return
	}
	p.sendErr = ""
	p.streams[conversationID] = messageID
	p.reloadMessages()
	p.stickToBottom()
	p.consume(conversationID, events)
}

func (p *Page) stop(conversationID string) {
	if p.svc != nil {
		p.svc.Stop(conversationID)
	}
}

func (p *Page) consume(conversationID string, events <-chan contract.ChatEvent) {
	go func() {
		for event := range events {
			ev := event
			p.update(func() {
				p.applyEvent(conversationID, ev)
			})
		}
	}()
}

// applyEvent 用后端快照覆盖消息，避免重复拼接。
func (p *Page) applyEvent(conversationID string, ev contract.ChatEvent) {
	if p.conversationID() == conversationID {
		found := false
		for i := range p.messages {
			if p.messages[i].ID != ev.MessageID {
				continue
			}
			found = true
			if len(ev.Content) >= len(p.messages[i].Content) {
				p.messages[i].Content = ev.Content
				p.messages[i].Status = ev.Status
				p.messages[i].Error = ev.Error
			}
			break
		}
		if !found {
			p.reloadMessages()
		}
	}

	if ev.Done {
		delete(p.streams, conversationID)
		p.reloadSessions()
		p.reloadAgents()
	}
}

// ---------- 数据加载 ----------

func (p *Page) reloadAgents() {
	if p.svc == nil {
		p.agents = nil
		p.groups = nil
		return
	}
	p.agents = p.svc.Agents()
	p.groups = p.svc.Groups()
}

func (p *Page) reloadModels() {
	if p.svc == nil {
		p.models = nil
		p.modelLabel = ""
		return
	}
	p.models = p.svc.Models()
	if len(p.models) > 0 {
		p.modelLabel = p.models[0].Name
	}
}

func (p *Page) reloadSessions() {
	agent := p.activeAgent()
	if p.svc == nil || agent == nil {
		p.sessions = nil
		return
	}
	p.sessions = p.svc.Sessions(agent.ID)
}

func (p *Page) reloadMessages() {
	conv := p.conversationID()
	if p.svc == nil || conv == "" {
		p.messages = nil
		return
	}
	p.messages = p.svc.Messages(conv)
}

// conversationID 返回当前对话的消息存储键：单聊为会话 ID，群聊为 "group:" 前缀的 key。
func (p *Page) conversationID() string {
	if p.activeAgent() != nil {
		return p.activeSessionID
	}
	return p.activeKey
}

// ---------- 派生状态 ----------

// followEnd 让列表贴底；内容变高不算用户上滚，只有滚动位置明显后退才停止跟随。
func (p *Page) followEnd() {
	if p.chatScroll.MaxY <= 0 {
		p.lastMaxY = 0
		if p.follow {
			p.chatScroll.Y = math.MaxFloat32
		}
		return
	}

	if !p.follow {
		if p.chatScroll.Y >= p.chatScroll.MaxY-16 {
			p.follow = true
			p.lastMaxY = p.chatScroll.MaxY
		}
		return
	}

	if p.lastMaxY > 0 && p.chatScroll.Y < p.lastMaxY-24 {
		p.follow = false
		p.lastMaxY = p.chatScroll.MaxY
		return
	}

	p.chatScroll.Y = math.MaxFloat32
	p.lastMaxY = p.chatScroll.MaxY
}

func (p *Page) stickToBottom() {
	p.follow = true
	p.lastMaxY = 0
	p.chatScroll.Y = math.MaxFloat32
}

func (p *Page) update(fn func()) {
	if p.hooks.Update == nil {
		fn()
		return
	}
	p.hooks.Update(fn)
}

func (p *Page) streamingActive() bool {
	conv := p.conversationID()
	return conv != "" && p.streams[conv] != ""
}

func (p *Page) activeAgent() *contract.AgentInfo {
	if !strings.HasPrefix(p.activeKey, "agent:") {
		return nil
	}
	id := strings.TrimPrefix(p.activeKey, "agent:")
	for i := range p.agents {
		if p.agents[i].ID == id {
			return &p.agents[i]
		}
	}
	return nil
}

func (p *Page) activeGroup() *contract.AgentGroup {
	if !strings.HasPrefix(p.activeKey, "group:") {
		return nil
	}
	id := strings.TrimPrefix(p.activeKey, "group:")
	for i := range p.groups {
		if p.groups[i].ID == id {
			return &p.groups[i]
		}
	}
	return nil
}

func (p *Page) activeName() string {
	if agent := p.activeAgent(); agent != nil {
		return agent.Name
	}
	if group := p.activeGroup(); group != nil {
		return group.Name
	}
	return ""
}

func (p *Page) activeDesc() string {
	if agent := p.activeAgent(); agent != nil {
		if agent.ModelName != "" {
			return agent.ModelName
		}
		return "默认模型"
	}
	if group := p.activeGroup(); group != nil {
		return fmt.Sprintf("%d 个 Agent", len(group.MemberIDs)+1)
	}
	return ""
}

func (p *Page) lastAssistantID() string {
	for i := len(p.messages) - 1; i >= 0; i-- {
		if p.messages[i].Role == contract.RoleAssistant {
			return p.messages[i].ID
		}
	}
	return ""
}

func (p *Page) modelID() string {
	for _, model := range p.models {
		if model.Name == p.modelLabel {
			return model.ID
		}
	}
	if len(p.models) > 0 {
		return p.models[0].ID
	}
	return ""
}

// ---------- 轻量控件 ----------

func boolValue(v bool) *bool {
	return &v
}

func iconButton(c *ui.Context, name components.IconName) ui.Element {
	t := c.Theme()

	b := ui.ButtonBase(c).
		Width(22).
		Height(22).
		Radius(4).
		AlignItems(ui.Center).
		Justify(ui.Center).
		TextColor(t.TextMuted)
	if b.Hovered() {
		b.Background(t.SurfacePressed).TextColor(t.Text)
	}
	b.Children(func() {
		components.Icon(c, name, 13)
	})
	return b
}

func textButton(c *ui.Context, label string, danger bool) ui.Element {
	t := c.Theme()

	fg := t.TextMuted
	bg := ui.Transparent
	b := ui.ButtonBase(c).
		Height(20).
		PaddingX(6).
		Radius(4).
		AlignItems(ui.Center).
		Justify(ui.Center)
	if b.Hovered() {
		if danger {
			fg = t.Danger
			bg = t.Danger.Alpha(0.1)
		} else {
			fg = t.Accent
			bg = t.Accent.Alpha(0.1)
		}
	}
	b.Background(bg).TextColor(fg)
	b.Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(fg)
	})
	return b
}

func circleButton(c *ui.Context, name components.IconName, bg, fg ui.Color, enabled bool) ui.Element {
	b := ui.ButtonBase(c).
		Width(32).
		Height(32).
		Radius(16).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Background(bg).
		TextColor(fg)
	if !enabled {
		b.Opacity(0.45).Disabled(true)
	}
	b.Children(func() {
		components.Icon(c, name, 15)
	})
	return b
}
