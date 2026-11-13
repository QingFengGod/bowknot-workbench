package agents

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/theme"
)

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
	// modelMenuOpen 是模型选择面板的开关，指针每帧指向同一地址。
	modelMenuOpen bool

	activeKey       string // "agent:xxx" 或 "group:yyy"
	sessions        []contract.AgentSession
	activeSessionID string
	messages        []contract.ChatMessage

	draft      string
	sendErr    string
	focusDraft bool

	panelGroupOpen   map[string]*bool
	sessionGroupOpen map[string]*bool
	// itemMenuOpen 保存左侧列表每个条目「更多」菜单的开关，按条目分别持有指针。
	itemMenuOpen      map[string]*bool
	panelScroll       ui.ScrollState
	chatScroll        ui.ScrollState
	sessionScroll     ui.ScrollState
	sessionsCollapsed bool
	// sessionMenuOpen 保存每个会话「更多」菜单的开关，按会话分别持有指针。
	sessionMenuOpen   map[string]*bool
	sessionDeleteOpen bool
	sessionDeleteID   string
	// follow 为 true 时消息列表贴着底部，用户上滚后关闭，回到底部再恢复。
	follow bool
	// lastMaxY 记录上一帧的最大滚动距离，用来判断内容是否变高。
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
		sessionMenuOpen: map[string]*bool{},
		itemMenuOpen:    map[string]*bool{},
	}
	p.reloadAgents()
	p.reloadModels()
	return p
}

func (p *Page) View(c *ui.Context) {
	t := c.Theme()

	p.followEnd()

	// 动画通道挂在常驻根行上，会话列收起后彻底退出布局，
	// 也不会给父容器留下多余的 gap。
	root := ui.Row(c).Gap(t.Space(3)).Fill()
	target := float32(1)
	if p.sessionsCollapsed {
		target = 0
	}
	progress := root.AnimateTo("sessions-panel", target, ui.Animation{
		Duration: 180 * time.Millisecond,
		Ease:     ui.EaseOut,
	})

	root.Children(func() {
		p.panel(c)

		chat := ui.Column(c).Grow(1).Background(t.Surface).Radius(theme.Radius).Clip().FillHeight()
		theme.NormalShadow.Apply(c, chat)
		chat.Children(func() {
			if p.activeKey == "" {
				p.emptyHint(c)
				return
			}
			p.chatHeader(c, progress)
			p.messageList(c)
			p.composer(c)
		})

		if p.activeAgent() != nil && progress > 0.001 {
			p.sessionColumn(c, progress)
		}
	})

	p.renameModal(c)
	p.agentDialog(c)
	p.groupDialog(c)
	p.deleteModal(c)
	p.deleteSessionModal(c)
	p.deleteMessageModal(c)
}

// emptyHint 是未选择任何条目时的占位，与助手页的空状态保持一致。
func (p *Page) emptyHint(c *ui.Context) {
	components.EmptyState(c, components.EmptyProps{
		Icon:  components.IconAgents,
		Title: "选择一个 Agent 或群聊",
		Hint:  "左侧列表里点一个条目，就能直接开始对话。",
	})
}

func (p *Page) chatHeader(c *ui.Context, progress float32) {
	t := c.Theme()

	ui.Row(c).
		FillWidth().
		Height(58).
		Shrink(0).
		PaddingX(t.Space(3)).
		Background(t.Surface).
		BorderWidth(0, 0, 1, 0).
		BorderColor(t.Border).
		AlignItems(ui.Center).
		Justify(ui.SpaceBetween).
		Children(func() {
			ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
				p.entityAvatar(c, 40)
				ui.Column(c).Gap(1).Children(func() {
					ui.Text(c, p.activeName()).FontSize(14).Bold().MaxLines(1).MaxWidth(300)
					ui.Text(c, p.activeDesc()).FontSize(11).TextColor(t.TextMuted)
				})
				if p.streamingActive() {
					components.Chip(c, "正在回复…", components.ToneBrand)
				}
			})

			if p.activeAgent() != nil {
				toggle := ui.ButtonBase(c).
					Width(22).
					Height(22).
					Radius(4).
					AlignItems(ui.Center).
					Justify(ui.Center).
					TextColor(t.TextMuted)
				if toggle.Hovered() {
					toggle.Background(t.SurfacePressed).TextColor(t.Text)
				}
				// 图标随展开进度平滑旋转，收起时转到 180°。
				toggle.Children(func() {
					components.Icon(c, components.IconPanel, 13).Rotate(180 * (1 - progress))
				})
				label := "收起会话列表"
				if p.sessionsCollapsed {
					label = "展开会话列表"
				}
				if toggle.Tooltip(label).Clicked() {
					p.sessionsCollapsed = !p.sessionsCollapsed
				}
			}
		})
}

// entityAvatar 渲染当前条目的头像：单聊取头像文字，群聊为四宫格成员头像。
func (p *Page) entityAvatar(c *ui.Context, size float32) {
	if agent := p.activeAgent(); agent != nil {
		components.Avatar(c, agent.AvatarText, size)
		return
	}
	group := p.activeGroup()
	if group == nil {
		return
	}
	p.groupAvatar(c, size, group.MemberIDs)
}

// groupAvatar 画与单人头像同尺寸的群聊头像：内部四宫格，
// 成员超过四个时最后一格显示剩余数量。
func (p *Page) groupAvatar(c *ui.Context, size float32, ids []string) {
	t := c.Theme()

	if len(ids) == 0 {
		components.Avatar(c, "群", size)
		return
	}

	cells := make([]string, 0, 4)
	for _, id := range ids {
		// 超过四个成员时只保留前三格，第四格让给数量。
		if len(ids) > 4 && len(cells) == 3 {
			break
		}
		cells = append(cells, p.avatarText(id))
		if len(cells) == 4 {
			break
		}
	}
	if len(ids) > 4 {
		cells = append(cells, fmt.Sprintf("+%d", len(ids)-3))
	}

	const gap float32 = 2
	pad := size * 0.08
	cell := (size - pad*2 - gap) / 2

	ui.Box(c).
		Width(size).
		Height(size).
		Shrink(0).
		Radius(size * 0.3).
		Padding(pad).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Background(t.SurfacePressed).
		Children(func() {
			ui.Column(c).Gap(gap).Children(func() {
				for start := 0; start < len(cells); start += 2 {
					batch := cells[start:min(start+2, len(cells))]
					ui.Row(c).Gap(gap).Children(func() {
						for _, text := range batch {
							bg, fg := t.Accent.Alpha(0.12), t.Accent
							if strings.HasPrefix(text, "+") {
								bg, fg = t.SurfacePressed, t.TextMuted
							}
							ui.Box(c).
								Width(cell).
								Height(cell).
								Radius(cell * 0.3).
								Background(bg).
								AlignItems(ui.Center).
								Justify(ui.Center).
								Children(func() {
									ui.Text(c, text).
										FontSize(cell * 0.58).
										Bold().
										TextColor(fg).
										MaxLines(1)
								})
						}
					})
				}
			})
		})
}

// avatarText 返回成员的头像文字，找不到时用问号占位。
func (p *Page) avatarText(id string) string {
	for _, agent := range p.agents {
		if agent.ID == id {
			return agent.AvatarText
		}
	}
	return "?"
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

		var questions []components.QuestionNavItem

		list := ui.Scroll(c).Grow(1).TrackScroll(&p.chatScroll)
		list.Children(func() {
			ui.Column(c).FillWidth().Padding(20, 24).Gap(18).Children(func() {
				for _, message := range p.messages {
					item := p.messageItem(c, message)
					if message.Role == contract.RoleUser {
						questions = append(questions, components.QuestionNavItem{
							Title:  message.Content,
							Target: item,
						})
					}
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

		components.QuestionNav(c, components.QuestionNavProps{
			Items:    questions,
			List:     list,
			AtEnd:    p.chatScroll.MaxY > 0 && p.chatScroll.Y >= p.chatScroll.MaxY-2,
			OnSelect: func() { p.follow = false },
		})
	})
}

func (p *Page) messageItem(c *ui.Context, m contract.ChatMessage) ui.Element {
	avatar := "助"
	if m.SenderAvatar != "" {
		avatar = m.SenderAvatar
	}

	return components.MessageView(c, components.MessageViewProps{
		Message:    m,
		AvatarText: avatar,
		ShowRetry:  m.Role == contract.RoleAssistant && m.ID == p.lastAssistantID() && m.Status != contract.MessageStreaming,
		OnStop:     func() { p.stop(p.conversationID()) },
		OnRetry:    func() { p.regenerate(m.ID) },
		OnDelete: func() {
			p.deleteMessageID = m.ID
			p.deleteMessageOpen = true
		},
	})
}

func (p *Page) composer(c *ui.Context) {
	streaming := p.streamingActive()

	components.Composer(c, components.ComposerProps{
		Draft:          &p.draft,
		FocusRequested: &p.focusDraft,
		Placeholder:    fmt.Sprintf("给 %s 发消息…", p.activeName()),
		Error:          p.sendErr,
		Submit:         p.sendText,
		Cancel: func() {
			if streaming {
				p.stop(p.conversationID())
			}
		},
		OnInput: func() {
			p.sendErr = ""
		},
		Actions: func(c *ui.Context) {
			t := c.Theme()

			p.modelPicker(c)

			if streaming {
				stop := components.CircleButton(c, components.IconStop, t.Danger, t.AccentText, true)
				if stop.Tooltip("停止生成").Clicked() {
					p.stop(p.conversationID())
				}
				return
			}
			enabled := strings.TrimSpace(p.draft) != ""
			send := components.CircleButton(c, components.IconSend, t.Accent, t.AccentText, enabled)
			if send.Tooltip("发送").Clicked() && enabled {
				p.send()
			}
		},
	})
}

// modelPicker 把页面的模型列表适配成选择器组件的选项。
func (p *Page) modelPicker(c *ui.Context) {
	options := make([]components.ModelOption, len(p.models))
	for i, model := range p.models {
		options[i] = components.ModelOption{Name: model.Name, Description: model.Description}
	}

	components.ModelPicker(c, components.ModelPickerProps{
		Options:  options,
		Selected: p.modelLabel,
		Open:     &p.modelMenuOpen,
		OnSelect: func(name string) {
			p.modelLabel = name
		},
	})
}

// panel 是左侧的 Agent / 群聊列表。
func (p *Page) panel(c *ui.Context) {
	t := c.Theme()

	root := ui.Column(c).
		Width(300).
		FillHeight().
		Shrink(0).
		Background(t.Surface).
		Radius(theme.Radius).
		Clip()

	root.Children(func() {
		ui.Row(c).
			FillWidth().
			Height(58).
			PaddingX(t.Space(3)).
			BorderWidth(0, 0, 1, 0).
			BorderColor(t.Border).
			AlignItems(ui.Center).
			Justify(ui.SpaceBetween).
			Children(func() {
				ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Agents").FontSize(13).Bold()
					components.Chip(c, fmt.Sprint(len(p.agents)+len(p.groups)), components.ToneNeutral)
				})

				ui.Text(c, "新建").TextColor(t.Accent).PopoverMenuButton(func(m *ui.Menu) {
					if m.Item("新建 Agent").Chosen() {
						p.openAgentDialog("")
					}
					if m.Item("新建群聊").Chosen() {
						p.openGroupDialog("")
					}
				})
			})

		ui.Scroll(c).Grow(1).TrackScroll(&p.panelScroll).Children(func() {
			ui.Column(c).FillWidth().Padding(t.Space(3)).Gap(t.Space(2)).Children(func() {
				for _, group := range p.panelGroups() {
					p.panelGroup(c, group)
				}
			})
		})
	})

	theme.NormalShadow.Apply(c, root)
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
	open := p.panelGroupOpen[group.key]
	if open == nil {
		open = boolValue(true)
		p.panelGroupOpen[group.key] = open
	}

	components.CollapsibleSection(c, components.CollapsibleSectionProps{
		Title: group.title,
		Open:  open,
	}, func() {
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
		Padding(t.Space(3)).
		Radius(theme.Radius).
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
	// 每个条目持有自己的菜单开关指针，指针跨帧稳定才能保留弹层状态。
	menuOpen := p.itemMenuOpen[item.key]
	if menuOpen == nil {
		menuOpen = boolValue(false)
		p.itemMenuOpen[item.key] = menuOpen
	}
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
		more.Tooltip("更多操作")
		if more.Clicked() {
			handled = true
			*menuOpen = true
		}
		components.Popover(c, more, menuOpen, func() {
			editLabel := "编辑人设"
			if item.kind == contract.ItemKindGroup {
				editLabel = "编辑成员"
			}
			components.PopoverItem(c, components.PopoverItemProps{
				Label: editLabel,
				OnClick: func() {
					handled = true
					*menuOpen = false
					p.editItem(item)
				},
				Content: func(c *ui.Context, _ bool) {
					ui.Text(c, editLabel).FontSize(13).TextColor(c.Theme().Text).MaxLines(1)
				},
			})

			pinLabel := "置顶"
			if item.pinned {
				pinLabel = "取消置顶"
			}
			components.PopoverItem(c, components.PopoverItemProps{
				Label: pinLabel,
				OnClick: func() {
					handled = true
					*menuOpen = false
					p.togglePin(item)
				},
				Content: func(c *ui.Context, _ bool) {
					ui.Text(c, pinLabel).FontSize(13).TextColor(c.Theme().Text).MaxLines(1)
				},
			})

			ui.Box(c).FillWidth().Height(1).Background(t.Border)

			removeLabel := "删除"
			if item.kind == contract.ItemKindGroup {
				removeLabel = "解散群聊"
			}
			components.PopoverItem(c, components.PopoverItemProps{
				Label:  removeLabel,
				Danger: true,
				OnClick: func() {
					handled = true
					*menuOpen = false
					p.deleteKey = item.key
					p.deleteOpen = true
				},
				Content: func(c *ui.Context, _ bool) {
					ui.Text(c, removeLabel).FontSize(13).TextColor(c.Theme().Danger).MaxLines(1)
				},
			})
		})
	})

	if row.Clicked() && !handled {
		p.selectItem(item.key)
	}
}

// itemAvatar 渲染条目头像：单聊取头像文字，群聊为四宫格成员头像。
func (p *Page) itemAvatar(c *ui.Context, item panelItem) {
	if item.kind == contract.ItemKindAgent {
		components.Avatar(c, item.avatar, 40)
		return
	}
	p.groupAvatar(c, 40, item.members)
}

// sessionColumn 是右侧的会话列表，仅单聊 Agent 显示。
// sessionColumn 是右侧会话列表：外层按展开进度收窄并裁剪，内层保持固定宽度，
// 避免过渡中文字重新换行，与助手页的侧栏一致。
func (p *Page) sessionColumn(c *ui.Context, progress float32) {
	t := c.Theme()
	const width float32 = 300

	root := ui.Box(c).Width(width * progress).FillHeight().Background(t.Surface).Radius(theme.Radius).Shrink(0).Clip().Children(func() {
		ui.Column(c).
			Width(width).
			FillHeight().
			Children(func() {
				ui.Row(c).
					FillWidth().
					Height(58).
					PaddingX(t.Space(3)).
					BorderWidth(0, 0, 1, 0).
					BorderColor(t.Border).
					AlignItems(ui.Center).
					Justify(ui.SpaceBetween).
					Children(func() {
						ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "会话").FontSize(13).Bold()
							components.Chip(c, fmt.Sprint(len(p.sessions)), components.ToneNeutral)
						})

						if ui.Text(c, "新建").TextColor(t.Accent).Clicked() {
							p.createSession()
						}
					})

				if len(p.sessions) == 0 {
					components.EmptyState(c, components.EmptyProps{
						Icon:  components.IconMessage,
						Title: "还没有会话",
						Hint:  "新建一个会话开始对话。",
					})
					return
				}

				ui.Scroll(c).Grow(1).TrackScroll(&p.sessionScroll).Children(func() {
					ui.Column(c).FillWidth().Padding(t.Space(3)).Gap(t.Space(2)).Children(func() {
						for _, group := range p.sessionGroups() {
							p.sessionGroup(c, group)
						}
					})
				})
			})
	})

	theme.NormalShadow.Apply(c, root)
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

	components.CollapsibleSection(c, components.CollapsibleSectionProps{
		Title: group.title,
		Open:  open,
	}, func() {
		ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
			for _, session := range group.sessions {
				p.sessionRow(c, session)
			}
		})
	})
}

func (p *Page) sessionRow(c *ui.Context, s contract.AgentSession) {
	menuOpen := p.sessionMenuOpen[s.ID]
	if menuOpen == nil {
		menuOpen = boolValue(false)
		p.sessionMenuOpen[s.ID] = menuOpen
	}

	components.SessionItem(c, components.SessionItemProps{
		Title:          s.Title,
		Summary:        s.Summary,
		UpdatedAt:      s.UpdatedAt,
		Active:         s.ID == p.activeSessionID,
		Pinned:         s.Pinned,
		MenuOpen:       menuOpen,
		DeleteDisabled: len(p.sessions) <= 1,
		OnSelect:       func() { p.selectSession(s.ID) },
		OnRename: func() {
			p.renameID = s.ID
			p.renameValue = s.Title
			p.renameErr = ""
			p.renameOpen = true
		},
		OnTogglePin: func() { p.pinSession(s) },
		OnDelete:    func() { p.openDeleteSession(s) },
	})
}

func (p *Page) openDeleteSession(s contract.AgentSession) {
	p.sessionDeleteID = s.ID
	p.sessionDeleteOpen = true
}

func (p *Page) deleteSessionModal(c *ui.Context) {
	var title string
	for _, s := range p.sessions {
		if s.ID == p.sessionDeleteID {
			title = s.Title
			break
		}
	}
	if title == "" {
		return
	}

	// 最后一个按钮是默认按钮，破坏性操作把「取消」放在最后，避免回车误删。
	switch ui.AlertDialog(c, &p.sessionDeleteOpen, "删除会话",
		fmt.Sprintf("确定删除「%s」吗？删除后消息无法恢复。", title), "删除", "取消") {
	case 0:
		p.deleteSession(p.sessionDeleteID)
		p.sessionDeleteID = ""
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

// followEnd 让列表贴住底部；用户上滚后停止跟随，回到底部再恢复。
// 只在内容变高时改写 Y：每帧都写 MaxFloat32 会被滚动容器判定为应用接管，
// 持续取消滚轮的滑动，表现为贴底后要滚好几下才能离开底部。
func (p *Page) followEnd() {
	if p.chatScroll.MaxY <= 0 {
		p.lastMaxY = 0
		return
	}

	if !p.follow {
		if p.chatScroll.Y >= p.chatScroll.MaxY-16 {
			p.follow = true
			p.chatScroll.Y = math.MaxFloat32
			p.lastMaxY = p.chatScroll.MaxY
		}
		return
	}

	// 内容没有变高却离开了上一帧的底部，说明用户主动上滚；
	// 内容收缩导致 MaxY 变小时不算。
	if p.chatScroll.MaxY >= p.lastMaxY && p.chatScroll.Y < p.lastMaxY-1 {
		p.follow = false
		p.lastMaxY = p.chatScroll.MaxY
		return
	}

	if p.chatScroll.MaxY > p.lastMaxY {
		p.chatScroll.Y = math.MaxFloat32
	}
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
