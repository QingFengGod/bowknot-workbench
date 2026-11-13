package assistant

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

// Page 是助手页的原生实现。
type Page struct {
	svc   contract.AssistantService
	hooks Hooks

	sessions   []contract.ChatSession
	models     []contract.ChatModel
	modelLabel string
	// modelMenuOpen 是模型选择面板的开关，指针每帧指向同一地址。
	modelMenuOpen bool

	activeID string
	messages []contract.ChatMessage

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
}

// New 创建助手页。
func New(svc contract.AssistantService, hooks Hooks) *Page {
	p := &Page{
		svc:      svc,
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
	if !p.shown {
		p.shown = true
		p.autoReply(p.activeID)
	}

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
}

func (p *Page) messageList(c *ui.Context) {
	t := c.Theme()

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

func (p *Page) messageItem(c *ui.Context, m contract.ChatMessage) ui.Element {
	return components.MessageView(c, components.MessageViewProps{
		Message:    m,
		AvatarText: "助手",
		ShowRetry:  m.Role == contract.RoleAssistant && m.ID == p.lastAssistantID() && m.Status != contract.MessageStreaming,
		OnStop:     func() { p.stop(m.ConversationID) },
		OnRetry:    func() { p.regenerate(m.ID) },
		OnDelete:   func() { p.openDeleteMessage(m) },
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
		Title:          s.Title,
		Summary:        s.Summary,
		UpdatedAt:      s.UpdatedAt,
		Active:         s.ID == p.activeID,
		Pinned:         s.Pinned,
		MenuOpen:       menuOpen,
		DeleteDisabled: len(p.sessions) <= 1,
		OnSelect:       func() { p.selectSession(s.ID) },
		OnRename:       func() { p.openRename(s) },
		OnTogglePin:    func() { p.togglePin(s) },
		OnDelete:       func() { p.openDelete(s) },
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
	case 0:
		p.deleteSession()
	}
}

func (p *Page) deleteMessageModal(c *ui.Context) {
	switch ui.AlertDialog(c, &p.deleteMessageOpen, "删除消息", "删除后无法恢复，确定删除这条消息吗？", "取消", "删除") {
	case 0:
		p.deleteMessage()
	}
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
	}
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
	if p.svc == nil || p.activeID == "" {
		p.messages = nil
		return
	}
	p.messages = p.svc.Messages(p.activeID)
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
