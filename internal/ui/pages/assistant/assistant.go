package assistant

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
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

// Page 是助手页的原生实现。
type Page struct {
	svc    contract.AssistantService
	memory contract.MemoryService
	hooks  Hooks

	sessions   []contract.ChatSession
	models     []contract.ChatModel
	modelLabel string
	// modelMenuOpen 是模型选择面板的开关，指针每帧指向同一地址。
	modelMenuOpen bool

	activeID string
	messages []contract.ChatMessage
	// lastReportRevision 记录已处理的后台汇报版本，避免每帧重复加载。
	lastReportRevision uint64
	// 消息分页：oldestSeq 是已加载消息的最小 seq，hasMore 表示还有更早的消息。
	oldestSeq int64
	hasMore   bool
	// scrollShift 标记头部刚插入历史消息，下一帧需要按高度差补偿滚动位置。
	scrollShift  bool
	previousMaxY float32
	previousY    float32

	draft   string
	sendErr string
	// focusDraft 在切换或新建会话后的下一帧聚焦输入框。
	focusDraft bool
	// shown 标记页面首次显示，自动演示流在首次进入时才播放。
	shown bool

	panelCollapsed bool
	panelScroll    ui.ScrollState
	chatScroll     ui.ScrollState
	// groupOpen 保存会话分组的展开状态，指针由 CollapsibleBase 直接读写。
	groupOpen map[string]*bool
	// menuOpen 保存每个会话「更多」菜单的开关，按会话分别持有指针。
	menuOpen map[string]*bool
	// follow 为 true 时消息列表贴着底部，用户上滚后关闭，回到底部再恢复。
	follow bool
	// lastMaxY 记录上一帧的最大滚动距离，用来判断内容是否变高。
	lastMaxY float32

	// streams 记录正在生成的会话与消息，支持不同会话各自流式输出。
	streams map[string]string

	renameOpen  bool
	renameID    string
	renameValue string
	renameErr   string

	deleteOpen bool
	deleteID   string

	deleteMessageOpen    bool
	deleteMessageID      string
	deleteMessageSession string

	// 危险工具审批状态，由流式审批事件驱动。
	approvalOpen   bool
	approvalRunID  string
	approvalCallID string
	approvalTool   string
	approvalArgs   string

	// 长期记忆面板及其编辑、清空状态。
	memoryOpen      bool
	memoryEditOpen  bool
	memoryEditID    string
	memoryEditValue string
	memoryEditErr   string
	memoryClearOpen bool
	memoryState     components.MemoryPanelState
	// memoryRefreshPending 避免面板打开时重复启动刷新定时器。
	memoryRefreshPending bool

	// 主助手人设：可从标题栏随时查看与编辑。
	personaOpen  bool
	personaDraft contract.AssistantPersona
	personaErr   string
}

// New 创建助手页。
func New(svc contract.AssistantService, memory contract.MemoryService, hooks Hooks) *Page {
	p := &Page{
		svc:      svc,
		memory:   memory,
		hooks:    hooks,
		streams:  map[string]string{},
		follow:   true,
		menuOpen: map[string]*bool{},
		groupOpen: map[string]*bool{
			"pinned":    boolValue(true),
			"today":     boolValue(true),
			"yesterday": boolValue(false),
			"week":      boolValue(false),
			"earlier":   boolValue(false),
		},
	}
	p.reloadSessions()
	p.reloadModels()
	if len(p.sessions) > 0 {
		p.activeID = p.sessions[0].ID
	}
	p.reloadMessages()
	return p
}

func (p *Page) View(c *ui.Context) {
	p.refreshBackgroundReports()
	if !p.shown {
		p.shown = true
		p.greetOnLaunch()
	}
	p.scheduleMemoryRefresh()

	p.reconcileScrollShift()
	p.followEnd()
	t := c.Theme()

	// 动画通道挂在常驻的根行上：侧栏收起后可以彻底退出布局，
	// 既不会给父容器留下多余的 gap，也能保证再次展开时从 0 平滑过渡。
	root := ui.Row(c).Gap(t.Space(3)).Fill()
	target := float32(1)
	if p.panelCollapsed {
		target = 0
	}
	panelProgress := root.AnimateTo("side-panel", target, ui.Animation{
		Duration: 180 * time.Millisecond,
		Ease:     ui.EaseOut,
	})

	root.Children(func() {
		leftPanel := ui.Column(c).Grow(1).Background(t.Surface).Radius(theme.Radius).Clip().FillHeight().Children(func() {
			p.header(c, panelProgress)
			p.messageList(c)
			p.composer(c)
		})
		theme.NormalShadow.Apply(c, leftPanel)

		if panelProgress > 0.001 {
			p.panel(c, panelProgress)
		}
	})

	p.renameModal(c)
	p.deleteModal(c)
	p.deleteMessageModal(c)
	p.approvalModal(c)
	p.personaModal(c)
	components.MemoryModal(c, components.MemoryModalProps{
		Service:   p.memory,
		Open:      &p.memoryOpen,
		State:     &p.memoryState,
		EditOpen:  &p.memoryEditOpen,
		EditID:    &p.memoryEditID,
		EditValue: &p.memoryEditValue,
		EditErr:   &p.memoryEditErr,
		ClearOpen: &p.memoryClearOpen,
		OnChanged: p.reloadMessages,
	})
}

func (p *Page) header(c *ui.Context, panelProgress float32) {
	t := c.Theme()
	title := p.activeTitle()

	ui.Row(c).
		FillWidth().
		Shrink(0).
		PaddingX(t.Space(3)).
		Height(58).
		Background(t.Surface).
		BorderWidth(0, 0, 1, 0).
		BorderColor(t.Border).
		AlignItems(ui.Center).
		Justify(ui.SpaceBetween).
		Children(func() {
			ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
				components.Avatar(c, "助手", 32)
				ui.Column(c).Gap(1).Children(func() {
					ui.Text(c, title).FontSize(14).Bold().MaxLines(1).MaxWidth(360)
					ui.Text(c, "主 Agent · 在线").FontSize(11).TextColor(t.TextMuted)
				})
				if p.streaming(p.activeID) {
					components.Chip(c, "正在回复…", components.ToneBrand)
				}
			})

			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				personaBtn := ui.ButtonBase(c).
					Width(22).
					Height(22).
					Radius(4).
					AlignItems(ui.Center).
					Justify(ui.Center).
					TextColor(t.TextMuted)
				if personaBtn.Hovered() {
					personaBtn.Background(t.SurfacePressed).TextColor(t.Text)
				}
				personaBtn.Children(func() {
					components.Icon(c, components.IconUser, 14)
				})
				if personaBtn.Tooltip("助手人设").Clicked() {
					p.openPersona()
				}

				memBtn := ui.ButtonBase(c).
					Width(22).
					Height(22).
					Radius(4).
					AlignItems(ui.Center).
					Justify(ui.Center).
					TextColor(t.TextMuted)
				if memBtn.Hovered() {
					memBtn.Background(t.SurfacePressed).TextColor(t.Text)
				}
				memBtn.Children(func() {
					components.Icon(c, components.IconMemory, 14)
				})
				if memBtn.Tooltip("长期记忆").Clicked() {
					p.memoryOpen = true
				}

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
					components.Icon(c, components.IconPanel, 13).Rotate(180 * (1 - panelProgress))
				})
				label := "收起会话列表"
				if p.panelCollapsed {
					label = "展开会话列表"
				}
				if toggle.Tooltip(label).Clicked() {
					p.panelCollapsed = !p.panelCollapsed
				}
			})
		})
}

func (p *Page) messageList(c *ui.Context) {
	t := c.Theme()

	// 滚到接近顶部时补一页更早的消息。
	if p.hasMore && !p.follow && p.chatScroll.MaxY > 0 && p.chatScroll.Y <= 32 {
		p.loadEarlier()
	}

	ui.Column(c).Grow(1).Children(func() {
		if len(p.messages) == 0 {
			components.EmptyState(c, components.EmptyProps{
				Icon:  components.IconMessage,
				Title: "开始和助理聊聊吧",
				Hint:  "回复会以流式方式逐步出现，可以随时停止或重答。",
			})
			return
		}

		var questions []components.QuestionNavItem

		list := ui.Scroll(c).Grow(1).TrackScroll(&p.chatScroll)
		list.Children(func() {
			ui.Column(c).FillWidth().Padding(t.Space(3)).Gap(t.Space(3)).Children(func() {
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

// scheduleMemoryRefresh 在记忆面板打开时每 2 秒请求一次重绘，
// 让异步抽取完成的新记忆无需重启应用就能出现。
func (p *Page) scheduleMemoryRefresh() {
	if !p.memoryOpen || p.memoryRefreshPending || p.hooks.Update == nil {
		return
	}
	p.memoryRefreshPending = true
	go func() {
		time.Sleep(2 * time.Second)
		p.hooks.Update(func() {
			p.memoryRefreshPending = false
			p.memoryState.Loaded = false
		})
	}()
}

// greetOnLaunch 在应用启动后的首次进入时接收流式问候；
// 持久化实现没有启动（如内存后端）时退回旧的自动回复逻辑。
func (p *Page) greetOnLaunch() {
	if p.svc == nil {
		return
	}
	sessionID, messageID, events, started := p.svc.GreetOnLaunch()
	if !started {
		p.autoReply(p.activeID)
		return
	}

	p.reloadSessions()
	p.activeID = sessionID
	p.reloadMessages()
	if events != nil {
		p.streams[sessionID] = messageID
		p.consume(sessionID, events)
	}
	p.stickToBottom()
}

func (p *Page) messageItem(c *ui.Context, m contract.ChatMessage) ui.Element {
	return components.MessageView(c, components.MessageViewProps{
		Message:    m,
		AvatarText: "助手",
		ShowRetry:  m.Role == contract.RoleAssistant && m.ID == p.lastAssistantID() && m.Status != contract.MessageStreaming,
		OnStop:     func() { p.stop(m.ConversationID) },
		OnRetry:    func() { p.regenerate(m.ID) },
		OnDelete:   func() { p.openDeleteMessage(m) },
		OnMemory:   func() { p.memoryOpen = true },
	})
}

func (p *Page) composer(c *ui.Context) {
	streamID := p.streams[p.activeID]

	components.Composer(c, components.ComposerProps{
		Draft:          &p.draft,
		FocusRequested: &p.focusDraft,
		Placeholder:    "输入消息，Enter 发送，Shift+Enter 换行",
		Error:          p.sendErr,
		Submit:         p.sendText,
		Cancel: func() {
			if p.streaming(p.activeID) {
				p.stop(p.activeID)
			}
		},
		OnInput: func() {
			p.sendErr = ""
		},
		Actions: func(c *ui.Context) {
			t := c.Theme()

			p.modelPicker(c)

			if streamID != "" {
				stop := components.CircleButton(c, components.IconStop, t.Danger, t.AccentText, true)
				if stop.Tooltip("停止生成").Clicked() {
					p.stop(p.activeID)
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

// panel 是右侧会话列表：外层按展开进度收窄并裁剪，内层保持固定宽度，
// 避免过渡中文字重新换行。宽度与间距都由本页控制，不收进组件。
func (p *Page) panel(c *ui.Context, progress float32) {
	t := c.Theme()
	const panelWidth float32 = 300

	root := ui.Box(c).Width(panelWidth * progress).FillHeight().Background(t.Surface).Radius(theme.Radius).Shrink(0).Clip().Children(func() {
		ui.Column(c).
			Width(panelWidth).
			FillHeight().
			Children(func() {
				ui.Row(c).
					FillWidth().
					PaddingX(t.Space(3)).
					Height(58).
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

				ui.Scroll(c).Grow(1).TrackScroll(&p.panelScroll).Children(func() {
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
	sessions []contract.ChatSession
}

// sessionGroups 按置顶和更新时间分组，顺序与 Web 端会话面板一致。
func (p *Page) sessionGroups() []sessionGroup {
	sorted := append([]contract.ChatSession(nil), p.sessions...)
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
	open := p.groupOpen[group.key]
	if open == nil {
		open = boolValue(group.key == "pinned" || group.key == "today")
		p.groupOpen[group.key] = open
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

func (p *Page) sessionRow(c *ui.Context, s contract.ChatSession) {
	menuOpen := p.menuOpen[s.ID]
	if menuOpen == nil {
		menuOpen = boolValue(false)
		p.menuOpen[s.ID] = menuOpen
	}

	components.SessionItem(c, components.SessionItemProps{
		Title:       s.Title,
		Summary:     s.Summary,
		UpdatedAt:   s.UpdatedAt,
		Active:      s.ID == p.activeID,
		Pinned:      s.Pinned,
		MenuOpen:    menuOpen,
		OnSelect:    func() { p.selectSession(s.ID) },
		OnRename:    func() { p.openRename(s) },
		OnTogglePin: func() { p.togglePin(s) },
		OnDelete:    func() { p.openDelete(s) },
	})
}

func (p *Page) renameModal(c *ui.Context) {
	t := c.Theme()

	canSave := strings.TrimSpace(p.renameValue) != ""

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
				p.saveRename()
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
			p.saveRename()
		}
	})
}

func (p *Page) deleteModal(c *ui.Context) {
	session := p.sessionByID(p.deleteID)
	if session == nil {
		return
	}

	switch ui.AlertDialog(c, &p.deleteOpen, "删除会话",
		fmt.Sprintf("确定删除「%s」吗？删除后消息无法恢复。", session.Title), "取消", "删除") {
	case 1:
		p.deleteSession()
	}
}

func (p *Page) deleteMessageModal(c *ui.Context) {
	switch ui.AlertDialog(c, &p.deleteMessageOpen, "删除消息", "删除后无法恢复，确定删除这条消息吗？", "取消", "删除") {
	case 1:
		p.deleteMessage()
	}
}

// personaModal 编辑主助手人设：姓名、性别、年龄、性格、角色与特殊要求。
func (p *Page) personaModal(c *ui.Context) {
	t := c.Theme()

	canSave := strings.TrimSpace(p.personaDraft.Name) != ""

	field := func(label, placeholder string, value *string) {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
		input := ui.TextInput(c, value).Placeholder(placeholder)
		if input.Changed() {
			p.personaErr = ""
		}
	}

	components.Modal(c, components.ModalProps{
		Open:  &p.personaOpen,
		Title: "助手人设",
		Width: 520,
		Footer: func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.personaOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:    "保存",
				Variant:  components.ButtonPrimary,
				Disabled: !canSave,
			}).Clicked() {
				p.savePersona()
			}
		},
	}, func() {
		ui.Text(c, "人设会影响助手的自我介绍、说话语气与每次回复，保存后立即生效。").
			FontSize(12).
			TextColor(t.TextMuted)

		field("姓名", "Bowknot", &p.personaDraft.Name)
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				field("性别", "例如：女", &p.personaDraft.Gender)
			})
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				field("年龄", "例如：22 岁", &p.personaDraft.Age)
			})
		})
		field("性格", "例如：温柔、直接、好奇", &p.personaDraft.Personality)
		field("角色或职能", "例如：你的私人助理", &p.personaDraft.Role)
		field("对用户的称呼", "例如：老板、小明", &p.personaDraft.UserName)

		ui.Text(c, "其它要求").FontSize(12).TextColor(t.TextMuted)
		area := ui.TextAreaBase(c, &p.personaDraft.Custom).
			Placeholder("例如：回答先给结论，遇到风险要提醒。").
			Lines(3, 6).
			MaxLength(300)
		if area.Changed() {
			p.personaErr = ""
		}

		if p.personaErr != "" {
			ui.Text(c, p.personaErr).FontSize(12).TextColor(t.Danger)
		}
	})
}

func (p *Page) openPersona() {
	p.personaDraft = p.persona()
	p.personaErr = ""
	p.personaOpen = true
}

func (p *Page) persona() contract.AssistantPersona {
	if p.svc == nil {
		return contract.DefaultAssistantPersona()
	}
	return p.svc.Persona()
}

func (p *Page) savePersona() {
	if p.svc == nil {
		return
	}
	if err := p.svc.SetPersona(p.personaDraft); err != nil {
		p.personaErr = err.Error()
		return
	}
	p.personaErr = ""
	p.personaOpen = false
}

// send 用输入框里的草稿发送，供按钮调用。
func (p *Page) send() {
	if p.sendText(p.draft) {
		p.draft = ""
	}
}

// sendText 发送一条消息；成功返回 true，由调用方决定是否清空输入。
func (p *Page) sendText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || p.svc == nil || p.streaming(p.activeID) {
		return false
	}

	// 没有会话时直接发消息，自动创建一个新会话。
	if p.activeID == "" {
		session := p.svc.CreateSession()
		if session.ID == "" {
			p.sendErr = "创建会话失败，请重试"
			return false
		}
		p.sessions = append([]contract.ChatSession{session}, p.sessions...)
		p.activeID = session.ID
		p.follow = true
	}

	sessionID := p.activeID
	messageID, events, err := p.svc.Send(sessionID, text, p.modelID())
	if err != nil {
		p.sendErr = err.Error()
		return false
	}

	p.draft = ""
	p.sendErr = ""
	p.focusDraft = true
	p.streams[sessionID] = messageID
	p.reloadMessages()
	p.reloadSessions()
	p.stickToBottom()
	p.consume(sessionID, events)
	return true
}

// autoReply 在历史会话没有助手回复时补一次演示流，每个会话只播放一次。
func (p *Page) autoReply(sessionID string) {
	if p.svc == nil || sessionID == "" || p.streaming(sessionID) {
		return
	}
	messageID, events, started := p.svc.AutoReply(sessionID)
	if !started || events == nil {
		return
	}
	p.streams[sessionID] = messageID
	p.reloadMessages()
	p.consume(sessionID, events)
}

func (p *Page) regenerate(messageID string) {
	if p.svc == nil || p.streaming(p.activeID) {
		if p.svc != nil {
			p.sendErr = "请先停止当前生成"
		}
		return
	}
	sessionID := p.activeID
	events, err := p.svc.Regenerate(sessionID, messageID)
	if err != nil {
		p.sendErr = err.Error()
		return
	}
	p.sendErr = ""
	p.streams[sessionID] = messageID
	p.reloadMessages()
	p.stickToBottom()
	p.consume(sessionID, events)
}

func (p *Page) stop(sessionID string) {
	if p.svc != nil {
		p.svc.Stop(sessionID)
	}
}

// consume 在后台读取流式事件，通过壳层回到主线程更新页面。
func (p *Page) consume(sessionID string, events <-chan contract.ChatEvent) {
	go func() {
		for event := range events {
			ev := event
			p.update(func() {
				p.applyEvent(sessionID, ev)
			})
		}
	}()
}

// applyEvent 用后端快照覆盖消息，避免重复拼接。
func (p *Page) applyEvent(sessionID string, ev contract.ChatEvent) {
	if ev.Type == contract.ChatEventApproval {
		p.openApproval(ev)
		return
	}
	if ev.Type == contract.ChatEventToolCall || ev.Type == contract.ChatEventToolResult {
		return
	}

	if p.activeID == sessionID {
		found := false
		for i := range p.messages {
			if p.messages[i].ID != ev.MessageID {
				continue
			}
			found = true
			// 切换会话重载后，旧事件可能晚到，不覆盖更长的内容。
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
		delete(p.streams, sessionID)
		p.reloadSessions()
		// 重新读取消息，把刚落库的工具调用一并展示出来。
		if p.activeID == sessionID {
			p.reloadMessages()
		}
	}
}

// openApproval 弹出危险工具的确认框。
func (p *Page) openApproval(ev contract.ChatEvent) {
	p.approvalRunID = ev.RunID
	p.approvalCallID = ev.ToolCallID
	p.approvalTool = ev.ToolName
	p.approvalArgs = ev.Arguments
	p.approvalOpen = true
}

func (p *Page) approvalModal(c *ui.Context) {
	if !p.approvalOpen {
		return
	}

	args := strings.TrimSpace(p.approvalArgs)
	if args == "" {
		args = "无"
	}
	text := fmt.Sprintf("助手请求执行危险工具「%s」，参数：%s。是否允许本次执行？", p.approvalTool, args)
	// 「拒绝」放在最后，回车默认不执行危险操作。
	switch ui.AlertDialog(c, &p.approvalOpen, "确认工具执行", text, "允许", "拒绝") {
	case 0:
		p.resolveApproval(true)
	case 1:
		p.resolveApproval(false)
	}
}

func (p *Page) resolveApproval(approved bool) {
	if svc, ok := p.svc.(contract.ApprovalService); ok {
		svc.ResolveApproval(p.approvalRunID, p.approvalCallID, approved)
	}
	p.approvalOpen = false
	p.approvalRunID = ""
	p.approvalCallID = ""
	p.approvalTool = ""
	p.approvalArgs = ""
}

func (p *Page) selectSession(id string) {
	if id == p.activeID || id == "" {
		return
	}
	p.activeID = id
	p.sendErr = ""
	p.focusDraft = true
	p.reloadMessages()
	p.stickToBottom()
	p.autoReply(id)
}

func (p *Page) createSession() {
	if p.svc == nil {
		return
	}
	session := p.svc.CreateSession()
	p.activeID = session.ID
	p.sendErr = ""
	p.focusDraft = true
	p.reloadSessions()
	p.reloadMessages()
	p.stickToBottom()
}

func (p *Page) openRename(s contract.ChatSession) {
	p.renameID = s.ID
	p.renameValue = s.Title
	p.renameErr = ""
	p.renameOpen = true
}

func (p *Page) saveRename() {
	if p.svc == nil {
		return
	}
	if err := p.svc.RenameSession(p.renameID, p.renameValue); err != nil {
		p.renameErr = err.Error()
		return
	}
	p.renameOpen = false
	p.reloadSessions()
}

func (p *Page) togglePin(s contract.ChatSession) {
	if p.svc == nil {
		return
	}
	if err := p.svc.PinSession(s.ID, !s.Pinned); err != nil {
		p.sendErr = err.Error()
		return
	}
	p.reloadSessions()
}

func (p *Page) openDelete(s contract.ChatSession) {
	p.deleteID = s.ID
	p.deleteOpen = true
}

func (p *Page) deleteSession() {
	if p.svc == nil {
		return
	}
	if err := p.svc.DeleteSession(p.deleteID); err != nil {
		mygo.Dialog.Error("删除会话失败", err.Error())
		p.deleteOpen = false
		return
	}
	delete(p.streams, p.deleteID)
	p.deleteOpen = false
	if p.deleteID == p.activeID {
		p.activeID = ""
	}
	p.deleteID = ""
	p.reloadSessions()
	if p.activeID == "" && len(p.sessions) > 0 {
		p.activeID = p.sessions[0].ID
	}
	p.reloadMessages()
	p.stickToBottom()
}

func (p *Page) openDeleteMessage(m contract.ChatMessage) {
	p.deleteMessageID = m.ID
	p.deleteMessageSession = m.ConversationID
	p.deleteMessageOpen = true
}

func (p *Page) deleteMessage() {
	if p.svc == nil {
		return
	}
	if p.streams[p.deleteMessageSession] == p.deleteMessageID {
		delete(p.streams, p.deleteMessageSession)
	}
	if err := p.svc.DeleteMessage(p.deleteMessageSession, p.deleteMessageID); err != nil {
		p.sendErr = err.Error()
	}
	p.deleteMessageOpen = false
	p.deleteMessageID = ""
	p.deleteMessageSession = ""
	p.reloadMessages()
}

// OnShow 在切回助手页时刷新会话与消息，后台委派的汇报可能已经落库。
func (p *Page) OnShow() {
	p.reloadSessions()
	p.reloadModels()
	if !p.streaming(p.activeID) {
		p.reloadMessages()
	}
}

// refreshBackgroundReports 在后台追加汇报消息后刷新当前会话。
// 生成中先不打断流式渲染，等这一轮结束后再补一次刷新。
func (p *Page) refreshBackgroundReports() {
	if p.svc == nil {
		return
	}
	revision := p.svc.Revision()
	if revision == p.lastReportRevision || p.streaming(p.activeID) {
		return
	}
	p.lastReportRevision = revision
	p.reloadMessages()
	p.reloadSessions()
}

func (p *Page) reloadSessions() {
	if p.svc == nil {
		p.sessions = nil
		return
	}
	p.sessions = p.svc.Sessions()
	if p.sessionByID(p.activeID) == nil && len(p.sessions) > 0 {
		if p.activeID != "" {
			p.activeID = p.sessions[0].ID
			p.reloadMessages()
		}
	}
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

func (p *Page) reloadMessages() {
	p.messages = nil
	p.oldestSeq = 0
	p.hasMore = false
	if p.svc == nil || p.activeID == "" {
		return
	}
	page := p.svc.MessagesPage(p.activeID, 0, contract.DefaultMessagePageSize)
	p.messages = page.Items
	p.hasMore = page.HasMore
	if len(page.Items) > 0 {
		p.oldestSeq = page.Items[0].Seq
	}
}

// loadEarlier 加载一页更早的消息并插到列表头部。
// 头部插入会抬高列表，记录当前滚动位置供下一帧补偿。
func (p *Page) loadEarlier() {
	if p.svc == nil || p.activeID == "" || !p.hasMore || p.oldestSeq <= 0 {
		return
	}
	page := p.svc.MessagesPage(p.activeID, p.oldestSeq, contract.DefaultMessagePageSize)
	p.hasMore = page.HasMore
	if len(page.Items) == 0 {
		return
	}

	p.previousMaxY = p.chatScroll.MaxY
	p.previousY = p.chatScroll.Y
	p.scrollShift = true
	p.follow = false
	p.messages = append(page.Items, p.messages...)
	p.oldestSeq = page.Items[0].Seq
}

// reconcileScrollShift 按列表增长的高度平移滚动位置，避免历史消息插入后视口跳动。
func (p *Page) reconcileScrollShift() {
	if !p.scrollShift {
		return
	}
	if p.chatScroll.MaxY > p.previousMaxY {
		p.chatScroll.Y = p.previousY + (p.chatScroll.MaxY - p.previousMaxY)
	}
	p.scrollShift = false
}

// stickToBottom 让下一次重绘贴住消息列表底部。
func (p *Page) stickToBottom() {
	p.follow = true
	p.lastMaxY = 0
	p.chatScroll.Y = math.MaxFloat32
}

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

func (p *Page) update(fn func()) {
	if p.hooks.Update == nil {
		fn()
		return
	}
	p.hooks.Update(fn)
}

func (p *Page) streaming(sessionID string) bool {
	return p.streams[sessionID] != ""
}

func (p *Page) activeTitle() string {
	if s := p.sessionByID(p.activeID); s != nil {
		return s.Title
	}
	return "助手"
}

func (p *Page) lastAssistantID() string {
	for i := len(p.messages) - 1; i >= 0; i-- {
		if p.messages[i].Role == contract.RoleAssistant {
			return p.messages[i].ID
		}
	}
	return ""
}

func (p *Page) sessionByID(id string) *contract.ChatSession {
	if id == "" {
		return nil
	}
	for i := range p.sessions {
		if p.sessions[i].ID == id {
			return &p.sessions[i]
		}
	}
	return nil
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

func boolValue(v bool) *bool {
	return &v
}
