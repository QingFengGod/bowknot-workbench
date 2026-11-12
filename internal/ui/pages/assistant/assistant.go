package assistant

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

// markdownOptions 沿用默认主题，只把行高调得更接近 Web 端正文。
var markdownOptions = &markdown.Options{
	Theme: &markdown.Theme{LineHeight: 1.7},
}

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
	// follow 为 true 时消息列表贴着底部，用户上滚后关闭，回到底部再恢复。
	follow bool
	// lastMaxY 记录上一帧的最大滚动距离，用来区分内容变高和用户主动上滚。
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
		svc:     svc,
		hooks:   hooks,
		streams: map[string]string{},
		follow:  true,
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
	t := c.Theme()

	if !p.shown {
		p.shown = true
		p.autoReply(p.activeID)
	}

	p.followEnd()

	ui.Row(c).Fill().Background(t.Background).Children(func() {
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			p.header(c)
			p.messageList(c)
			p.composer(c)
		})

		if !p.panelCollapsed {
			p.panel(c)
		}
	})

	p.renameModal(c)
	p.deleteModal(c)
	p.deleteMessageModal(c)
}

func (p *Page) header(c *ui.Context) {
	t := c.Theme()
	title := p.activeTitle()

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
				components.Avatar(c, "助手", 32)
				ui.Column(c).Gap(1).Children(func() {
					ui.Text(c, title).FontSize(14).Bold().MaxLines(1).MaxWidth(360)
					ui.Text(c, "主 Agent · 在线").FontSize(11).TextColor(t.TextMuted)
				})
				if p.streaming(p.activeID) {
					components.Chip(c, "正在回复…", components.ToneBrand)
				}
			})

			toggle := iconButton(c, components.IconPanel)
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
	ui.Column(c).Grow(1).Children(func() {
		if len(p.messages) == 0 {
			components.EmptyState(c, components.EmptyProps{
				Icon:  components.IconMessage,
				Title: "开始和助理聊聊吧",
				Hint:  "回复会以流式方式逐步出现，可以随时停止或重答。",
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
				components.Avatar(c, "助手", 32)
			}

			column := ui.Column(c).Gap(3)
			if isUser {
				column.MaxWidth(620).AlignItems(ui.End)
			} else {
				column.Grow(1).MaxWidth(760)
			}

			column.Children(func() {
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
					p.stop(m.ConversationID)
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
		if m.Emotion != "" {
			ui.Text(c, "情绪："+emotionLabel(m.Emotion)).FontSize(11).TextColor(t.TextMuted)
		}
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
		copyButton := textButton(c, "复制", false)
		if copyButton.Clicked() && strings.TrimSpace(m.Content) != "" {
			c.Services().WriteClipboard(m.Content)
			c.Toast("已复制")
		}
		if lastAssistant && m.Status != contract.MessageStreaming {
			if textButton(c, "重答", false).Clicked() {
				p.regenerate(m.ID)
			}
		}
		if textButton(c, "删除", true).Clicked() {
			p.openDeleteMessage(m)
		}
	})
}

func (p *Page) composer(c *ui.Context) {
	t := c.Theme()
	streamID := p.streams[p.activeID]

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
					Placeholder("输入消息，Enter 发送，Shift+Enter 换行").
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
					if k.Key == ui.KeyEscape && p.streaming(p.activeID) {
						p.stop(p.activeID)
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

					if streamID != "" {
						stop := circleButton(c, components.IconStop, t.Danger, t.AccentText, true)
						if stop.Tooltip("停止生成").Clicked() {
							p.stop(p.activeID)
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

func (p *Page) panel(c *ui.Context) {
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
							p.panelCollapsed = true
						}
					})
				})

			ui.Scroll(c).Grow(1).TrackScroll(&p.panelScroll).Children(func() {
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

	panel := parts.Panel(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			for _, session := range group.sessions {
				p.sessionRow(c, session)
			}
		})
	})
	if panel.Valid() {
		panel.Padding(0, 0, 4, 0)
	}
}

func (p *Page) sessionRow(c *ui.Context, s contract.ChatSession) {
	t := c.Theme()
	active := s.ID == p.activeID

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
					p.openRename(s)
					handled = true
				}
				pinLabel := "置顶"
				if s.Pinned {
					pinLabel = "取消置顶"
				}
				if m.Item(pinLabel).Chosen() {
					p.togglePin(s)
					handled = true
				}
				m.Separator()
				if m.Item("删除").Danger().Disabled(len(p.sessions) <= 1).Chosen() {
					p.openDelete(s)
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

func (p *Page) renameModal(c *ui.Context) {
	if !p.renameOpen {
		return
	}
	t := c.Theme()

	canSave := strings.TrimSpace(p.renameValue) != ""

	ui.Modal(c, &p.renameOpen, func() {
		ui.Column(c).Width(420).Gap(10).Children(func() {
			ui.Text(c, "重命名会话").FontSize(18).Bold()
			input := ui.TextInput(c, &p.renameValue).Placeholder("会话名称").MaxLength(30)
			if input.Changed() {
				p.renameErr = ""
			}
			if p.renameErr != "" {
				ui.Text(c, p.renameErr).FontSize(12).TextColor(t.Danger)
			}

			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
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
			})

			if input.Submitted() && canSave {
				p.saveRename()
			}
		})
	})
}

func (p *Page) deleteModal(c *ui.Context) {
	if !p.deleteOpen {
		return
	}
	session := p.sessionByID(p.deleteID)
	if session == nil {
		return
	}
	t := c.Theme()

	ui.Modal(c, &p.deleteOpen, func() {
		ui.Column(c).Width(420).Gap(10).Children(func() {
			ui.Text(c, "删除会话").FontSize(18).Bold()
			ui.Text(c, fmt.Sprintf("确定删除「%s」吗？删除后消息无法恢复。", session.Title)).
				FontSize(13).
				TextColor(t.TextMuted)

			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if components.Button(c, components.ButtonProps{
					Label:   "取消",
					Variant: components.ButtonSecondary,
				}).Clicked() {
					p.deleteOpen = false
				}
				if components.Button(c, components.ButtonProps{
					Label:    "删除",
					Variant:  components.ButtonDanger,
					Disabled: len(p.sessions) <= 1,
				}).Clicked() {
					p.deleteSession()
				}
			})
		})
	})
}

func (p *Page) deleteMessageModal(c *ui.Context) {
	if !p.deleteMessageOpen {
		return
	}
	t := c.Theme()

	ui.Modal(c, &p.deleteMessageOpen, func() {
		ui.Column(c).Width(420).Gap(10).Children(func() {
			ui.Text(c, "删除消息").FontSize(18).Bold()
			ui.Text(c, "删除后无法恢复，确定删除这条消息吗？").
				FontSize(13).
				TextColor(t.TextMuted)

			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
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
					p.deleteMessage()
				}
			})
		})
	})
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

func emotionLabel(value string) string {
	switch value {
	case "happy":
		return "开心"
	case "shy":
		return "害羞"
	case "angry":
		return "生气"
	case "sad":
		return "难过"
	case "surprised":
		return "惊讶"
	case "neutral":
		return "平静"
	default:
		return value
	}
}

// iconButton 是面板里的轻量图标按钮。
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

// textButton 是消息操作栏里的轻量文字按钮。
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

// circleButton 是输入区右侧的圆形图标按钮。
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
