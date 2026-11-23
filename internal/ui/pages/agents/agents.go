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
	svc       contract.AgentService
	tasks     contract.TaskService
	artifacts contract.ArtifactService
	memory    contract.MemoryService
	hooks     Hooks

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
	// timeline 按消息 ID 保存思考与工具步骤，进程内一直保留。
	timeline *components.Timeline
	// 消息分页：oldestSeq 是已加载消息的最小 seq，hasMore 表示还有更早的消息。
	oldestSeq int64
	hasMore   bool
	// scrollShift 标记头部刚插入历史消息，下一帧需要按高度差补偿滚动位置。
	scrollShift  bool
	previousMaxY float32
	previousY    float32

	draft      string
	sendErr    string
	focusDraft bool
	// mentionOpen 是群聊 @ 成员面板的开关，指针每帧指向同一地址。
	mentionOpen bool

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
	// scrollToMessageID 在下一帧把指定消息滚动到可视区域，用于产物回链。
	scrollToMessageID string
	// lastMaxY 记录上一帧的最大滚动距离，用来判断内容是否变高。
	lastMaxY float32

	// streams 记录正在生成的会话与消息。
	streams map[string]string

	renameOpen  bool
	renameID    string
	renameValue string
	renameErr   string

	agentDialogOpen  bool
	editAgentID      string
	agentName        string
	agentDesc        string
	agentGender      string
	agentAge         string
	agentPersonality string
	agentRole        string
	agentUserName    string
	agentCustom      string
	agentErr         string

	groupDialogOpen bool
	editGroupID     string
	groupName       string
	groupMembers    map[string]*bool
	groupErr        string

	deleteOpen bool
	deleteKey  string

	deleteMessageOpen bool
	deleteMessageID   string

	// 危险工具审批状态，由流式审批事件驱动。
	approvalOpen   bool
	approvalRunID  string
	approvalCallID string
	approvalTool   string
	approvalArgs   string

	// 长期记忆面板及其清空状态。
	memoryOpen      bool
	memoryClearOpen bool
	memoryState     components.MemoryPanelState

	// 任务线程：按会话 ID 索引，任务线程只读并显示状态标签。
	tasksByThread    map[string]contract.TaskDetail
	lastTaskRevision uint64
	lastTaskRefresh  time.Time

	// 文件预览：抽屉状态与删除确认统一由 ArtifactHost 管理。
	artifactHost         *components.ArtifactHost
	lastArtifactRevision uint64
	// filesOpen 是「会话文件」抽屉的开关。
	filesOpen bool

	// 群聊协作：版本号用于刷新成员发言，运行中定时轮询流式内容。
	lastGroupRevision uint64
	lastGroupRefresh  time.Time
}

// New 创建 agents 页。
func New(svc contract.AgentService, tasks contract.TaskService, artifacts contract.ArtifactService, memory contract.MemoryService, hooks Hooks) *Page {
	p := &Page{
		svc:           svc,
		tasks:         tasks,
		artifacts:     artifacts,
		memory:        memory,
		hooks:         hooks,
		streams:       map[string]string{},
		timeline:      components.NewTimeline(),
		tasksByThread: map[string]contract.TaskDetail{},
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
		artifactHost:    components.NewArtifactHost(artifacts),
	}
	p.reloadAgents()
	p.reloadModels()
	return p
}

func (p *Page) View(c *ui.Context) {
	t := c.Theme()

	p.refreshTaskRevisions()
	p.refreshArtifactRevisions()
	p.refreshGroupRevisions()
	p.reconcileScrollShift()
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
			if p.activeTask() != nil {
				p.taskReadOnlyBar(c)
			} else {
				p.composer(c)
			}
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
	p.approvalModal(c)

	memoryAgentID, memoryAgentName := "", ""
	if agent := p.activeAgent(); agent != nil {
		memoryAgentID, memoryAgentName = agent.ID, agent.Name
	}
	components.MemoryModal(c, components.MemoryModalProps{
		Service:   p.memory,
		Open:      &p.memoryOpen,
		State:     &p.memoryState,
		AgentID:   memoryAgentID,
		AgentName: memoryAgentName,
		ClearOpen: &p.memoryClearOpen,
		OnChanged: p.reloadMessages,
	})
	p.artifactDrawer(c)
	components.SessionFilesDrawer(c, &p.filesOpen, components.SessionArtifacts(p.messages), p.openArtifact)
}

// openArtifact 打开文件预览抽屉。
func (p *Page) openArtifact(id string) {
	if p.artifacts == nil || id == "" {
		return
	}
	p.artifactHost.Open(id)
}

func (p *Page) artifactDrawer(c *ui.Context) {
	p.artifactHost.Render(c, components.ArtifactHostProps{
		OnReveal: func(messageID string) {
			p.follow = false
			p.scrollToMessageID = messageID
		},
		OnDelete: p.deleteArtifact,
	})
}

func (p *Page) deleteArtifact(id string) {
	if p.artifacts == nil || id == "" {
		return
	}
	go func() {
		err := p.artifacts.DeleteArtifact(id)
		p.update(func() {
			if err != nil {
				p.sendErr = err.Error()
				return
			}
			p.artifactHost.Close()
			p.reloadMessages()
			p.reloadTasks()
		})
	}()
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
				if task := p.activeTask(); task != nil {
					components.TaskStatusChip(c, task.Status)
				}
			})

			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				if components.SessionFilesButton(c, len(components.SessionArtifacts(p.messages))) {
					p.filesOpen = true
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
	// 滚到接近顶部时补一页更早的消息。
	if p.hasMore && !p.follow && p.chatScroll.MaxY > 0 && p.chatScroll.Y <= 32 {
		p.loadEarlier()
	}

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

	// 任务线程是只读的工作记录，不提供停止 / 重答 / 删除入口。
	task := p.activeTask()
	if m.MessageType == "task_dispatch" {
		t := c.Theme()
		return ui.Column(c).
			FillWidth().
			Padding(10).
			Gap(4).
			Radius(theme.RadiusSM).
			Background(t.SurfaceHover).
			Children(func() {
				ui.Text(c, "主助手派发任务").FontSize(11).Bold().TextColor(t.TextMuted)
				ui.Text(c, m.Content).FontSize(12).TextColor(t.Text)
			})
	}
	if task != nil {
		avatar = firstRuneOr(task.AssigneeName, avatar)
	}
	var props components.MessageViewProps
	if task != nil {
		props = components.MessageViewProps{
			Message:        m,
			AvatarText:     avatar,
			OnOpenArtifact: p.openArtifact,
			Steps:          p.timeline.Steps(m.ID),
			StepOpen:       func(index int) *bool { return p.timeline.StepOpen(m.ID, index) },
			ToolOpen:       func(index int) *bool { return p.timeline.ToolOpen(m.ID, index) },
			ThinkingOpen:   p.timeline.ThinkingOpen(m.ID),
			ToolsOpen:      p.timeline.ToolsOpen(m.ID),
		}
	} else {
		showRetry := m.Role == contract.RoleAssistant && m.ID == p.lastAssistantID() && m.Status != contract.MessageStreaming
		// 群聊回复依赖 @ 编排上下文，单条重答会失去成员协作，因此不提供入口。
		if p.activeGroup() != nil {
			showRetry = false
		}
		props = components.MessageViewProps{
			Message:        m,
			AvatarText:     avatar,
			OnOpenArtifact: p.openArtifact,
			ShowRetry:      showRetry,
			Steps:          p.timeline.Steps(m.ID),
			StepOpen:       func(index int) *bool { return p.timeline.StepOpen(m.ID, index) },
			ToolOpen:       func(index int) *bool { return p.timeline.ToolOpen(m.ID, index) },
			ThinkingOpen:   p.timeline.ThinkingOpen(m.ID),
			ToolsOpen:      p.timeline.ToolsOpen(m.ID),
			OnStop:         func() { p.stop(p.conversationID()) },
			OnRetry:        func() { p.regenerate(m.ID) },
			OnDelete: func() {
				p.deleteMessageID = m.ID
				p.deleteMessageOpen = true
			},
			OnMemory: func() { p.memoryOpen = true },
		}
	}

	if p.artifacts == nil || len(m.Artifacts) == 0 {
		props.OnOpenArtifact = nil
	}
	element := components.MessageView(c, props)
	if p.scrollToMessageID != "" && m.ID == p.scrollToMessageID {
		element.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter, Animation: &ui.Smooth})
		p.scrollToMessageID = ""
	}
	return element
}

// taskReadOnlyBar 替换输入区：任务线程是主助手派发的内部工作记录。
func (p *Page) taskReadOnlyBar(c *ui.Context) {
	t := c.Theme()

	ui.Row(c).
		FillWidth().
		Shrink(0).
		Padding(t.Space(3)).
		Gap(8).
		BorderWidth(1, 0, 0, 0).
		BorderColor(t.Border).
		AlignItems(ui.Center).
		Children(func() {
			components.Icon(c, components.IconMessage, 14).TextColor(t.TextMuted)
			ui.Text(c, "任务线程由主助手派发，只读查看；需要继续沟通请打开该员工的直聊会话。").
				FontSize(12).
				TextColor(t.TextMuted)
		})
}

// refreshTaskRevisions 在任务状态变化或运行中定时刷新任务线程。
func (p *Page) refreshTaskRevisions() {
	if p.tasks == nil {
		return
	}
	revision := p.tasks.TaskRevision()
	changed := revision != p.lastTaskRevision
	if changed {
		p.lastTaskRevision = revision
		p.reloadSessions()
		// 群聊里的任务汇报直接写回群会话，需要同步刷新消息。
		if p.activeGroup() != nil && !p.streamingActive() {
			p.reloadMessages()
		}
	}

	task := p.activeTask()
	if task == nil {
		return
	}
	if changed || (task.Status == contract.TaskQueued || task.Status == contract.TaskRunning) && time.Since(p.lastTaskRefresh) > time.Second {
		p.lastTaskRefresh = time.Now()
		p.reloadMessages()
	}
}

// refreshArtifactRevisions 在产物创建或删除后刷新当前会话。
func (p *Page) refreshArtifactRevisions() {
	if p.artifacts == nil {
		return
	}
	revision := p.artifacts.ArtifactRevision()
	if revision == p.lastArtifactRevision || p.streamingActive() {
		return
	}
	p.lastArtifactRevision = revision
	p.reloadMessages()
}

// refreshGroupRevisions 在群聊协作推进时刷新成员发言；运行中定时轮询流式内容。
func (p *Page) refreshGroupRevisions() {
	if p.svc == nil || p.activeGroup() == nil {
		return
	}
	conversationID := p.conversationID()
	revision := p.svc.GroupRevision()
	busy := p.svc.GroupBusy(conversationID)
	changed := revision != p.lastGroupRevision

	if !changed && !(busy && time.Since(p.lastGroupRefresh) > 600*time.Millisecond) {
		return
	}
	if changed {
		p.lastGroupRevision = revision
		p.reloadSessions()
	}
	p.lastGroupRefresh = time.Now()
	// 主助手自己的流式回复由事件更新，其余成员发言靠轮询刷新。
	if !p.streamingActive() || !busy {
		p.reloadMessages()
	}
}

// reloadTasks 建立任务线程与会话的映射，供状态标签与只读判断使用。
func (p *Page) reloadTasks() {
	p.tasksByThread = map[string]contract.TaskDetail{}
	agent := p.activeAgent()
	if p.tasks == nil || agent == nil {
		return
	}
	for _, task := range p.tasks.Tasks(agent.ID, "", 100) {
		if task.ThreadConversationID != "" {
			p.tasksByThread[task.ThreadConversationID] = task
		}
	}
}

func (p *Page) activeTask() *contract.TaskDetail {
	if p.activeSessionID == "" {
		return nil
	}
	task, ok := p.tasksByThread[p.activeSessionID]
	if !ok {
		return nil
	}
	return &task
}

func (p *Page) composer(c *ui.Context) {
	streaming := p.streamingActive()
	placeholder := fmt.Sprintf("给 %s 发消息…", p.activeName())
	if p.activeGroup() != nil {
		placeholder = "给群成员发消息，输入 @ 可提及成员…"
	}
	if p.groupBusy() {
		placeholder = "成员正在发言，请稍候…"
	}

	components.Composer(c, components.ComposerProps{
		Draft:          &p.draft,
		FocusRequested: &p.focusDraft,
		Placeholder:    placeholder,
		Error:          p.sendErr,
		Submit:         p.sendText,
		Cancel: func() {
			if streaming {
				p.stop(p.conversationID())
			}
		},
		OnInput: func() {
			p.sendErr = ""
			// 在群聊里输入 @ 时直接弹出成员列表，省去点击按钮。
			if p.activeGroup() != nil && mentionsOpenTrigger(p.draft) {
				p.mentionOpen = true
			}
		},
		Actions: func(c *ui.Context) {
			t := c.Theme()

			// 群聊时把 @ 成员按钮放在左侧，模型选择与发送仍在右侧。
			if p.activeGroup() != nil && !p.groupBusy() {
				ui.Box(c).Grow(1).Children(func() {
					p.mentionButton(c)
				})
			}

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

// mentionsOpenTrigger 判断草稿末尾是否刚输入了 @，用于自动打开成员列表。
func mentionsOpenTrigger(draft string) bool {
	trimmed := strings.TrimRight(draft, " \t\n")
	return strings.HasSuffix(trimmed, "@") || strings.HasSuffix(trimmed, "＠")
}

// mentionButton 是群聊输入区的 @ 成员入口：点击或输入 @ 后选择成员，
// 把「@名字 」插入草稿并重新聚焦输入框。
func (p *Page) mentionButton(c *ui.Context) {
	t := c.Theme()

	trigger := ui.ButtonBase(c).
		Height(28).
		PaddingX(8).
		Radius(theme.RadiusSM).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Gap(4)
	if trigger.Hovered() {
		trigger.Background(t.SurfaceHover)
	}
	trigger.Children(func() {
		components.Icon(c, components.IconAtSign, 13).TextColor(t.TextMuted)
		ui.Text(c, "提及成员").FontSize(12).TextColor(t.TextMuted)
	})
	if trigger.Clicked() {
		p.mentionOpen = !p.mentionOpen
	}

	components.Popover(c, trigger, &p.mentionOpen, func() {
		members := p.groupMemberInfos()
		if len(members) == 0 {
			ui.Text(c, "群里还没有成员").FontSize(12).TextColor(t.TextMuted)
			return
		}
		for _, member := range members {
			info := member
			components.PopoverItem(c, components.PopoverItemProps{
				Label: info.Name,
				OnClick: func() {
					p.insertMention(info.Name)
				},
				Content: func(c *ui.Context, selected bool) {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
						components.Icon(c, components.IconAtSign, 12).TextColor(c.Theme().TextMuted)
						ui.Text(c, info.Name).FontSize(13).TextColor(c.Theme().Text).MaxLines(1)
					})
				},
			})
		}
	})
}

// groupMemberInfos 按群成员顺序返回仍然存在的成员信息。
func (p *Page) groupMemberInfos() []contract.AgentInfo {
	group := p.activeGroup()
	if group == nil {
		return nil
	}
	index := make(map[string]contract.AgentInfo, len(p.agents))
	for _, agent := range p.agents {
		index[agent.ID] = agent
	}
	out := make([]contract.AgentInfo, 0, len(group.MemberIDs))
	for _, id := range group.MemberIDs {
		if agent, ok := index[id]; ok {
			out = append(out, agent)
		}
	}
	return out
}

// insertMention 把成员名追加到草稿末尾；如果正在输入 @ 名字，用选中成员替换这一段。
func (p *Page) insertMention(name string) {
	p.mentionOpen = false
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	draft := strings.TrimRight(p.draft, " \t\n")
	runes := []rune(draft)
	last := -1
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == '@' || runes[i] == '＠' {
			last = i
			break
		}
	}
	if last >= 0 && !strings.ContainsAny(string(runes[last+1:]), " \t\n") {
		draft = strings.TrimRight(string(runes[:last]), " \t\n")
	}
	if draft != "" {
		draft += " "
	}
	p.draft = draft + "@" + name + " "
	p.focusDraft = true
	p.sendErr = ""
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

	statusText, statusTone := "", components.ToneNeutral
	if task, ok := p.tasksByThread[s.ID]; ok {
		statusText = components.TaskStatusText(task.Status)
		statusTone = components.TaskStatusTone(task.Status)
	}

	components.SessionItem(c, components.SessionItemProps{
		Title:          s.Title,
		Summary:        s.Summary,
		UpdatedAt:      s.UpdatedAt,
		Active:         s.ID == p.activeSessionID,
		Pinned:         s.Pinned,
		MenuOpen:       menuOpen,
		Status:         statusText,
		StatusTone:     statusTone,
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

	// 取消在左、确定在右，与其它确认弹窗保持一致。
	switch ui.AlertDialog(c, &p.sessionDeleteOpen, "删除会话",
		fmt.Sprintf("确定删除「%s」吗？删除后消息无法恢复。", title), "取消", "删除") {
	case 1:
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
	if text == "" || p.svc == nil || p.streamingActive() || p.activeTask() != nil {
		return false
	}
	if p.groupBusy() {
		p.sendErr = "群成员正在发言，请稍候再发送"
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
	p.mentionOpen = false
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
	switch ev.Type {
	case contract.ChatEventApproval:
		p.openApproval(ev)
		return
	case contract.ChatEventReasoning:
		p.timeline.ApplyReasoning(ev.MessageID, ev.Reasoning)
		return
	case contract.ChatEventToolCall:
		p.timeline.ApplyToolCall(ev)
		return
	case contract.ChatEventToolResult:
		p.timeline.ApplyToolResult(ev)
		return
	}

	if p.conversationID() == conversationID {
		found := false
		for i := range p.messages {
			if p.messages[i].ID != ev.MessageID {
				continue
			}
			found = true
			// Done 是最终快照，可能比流式内容短（例如清理了发言人前缀），
			// 仍需覆盖；失败或停止时内容为空则保留已显示的部分。
			if ev.Done {
				if ev.Content != "" {
					p.messages[i].Content = ev.Content
				}
				p.messages[i].Status = ev.Status
				p.messages[i].Error = ev.Error
			} else if len(ev.Content) >= len(p.messages[i].Content) {
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
		// 重新读取消息，把刚落库的工具调用一并展示出来。
		if p.conversationID() == conversationID {
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
	text := fmt.Sprintf("Agent 请求执行危险工具「%s」，参数：%s。是否允许本次执行？", p.approvalTool, args)
	// 取消在左、确定在右，与其它确认弹窗保持一致。
	switch ui.AlertDialog(c, &p.approvalOpen, "确认工具执行", text, "拒绝", "允许") {
	case 1:
		p.resolveApproval(true)
	case 0:
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

// ---------- 数据加载 ----------

// OpenAgentChat 从其它页面跳转到员工的直聊会话：
// 优先复用最近的非任务会话，没有直聊会话时自动创建一个。
func (p *Page) OpenAgentChat(agentID string) {
	agentID = strings.TrimSpace(agentID)
	if p.svc == nil || agentID == "" {
		return
	}

	p.reloadAgents()
	p.activeKey = "agent:" + agentID
	p.sendErr = ""
	p.follow = true
	p.sessionsCollapsed = false
	p.reloadSessions()

	var preferred *contract.AgentSession
	for i := range p.sessions {
		// 任务线程只读，不作为跳转目标。
		if _, isTask := p.tasksByThread[p.sessions[i].ID]; isTask {
			continue
		}
		if preferred == nil || p.sessions[i].UpdatedAt > preferred.UpdatedAt {
			preferred = &p.sessions[i]
		}
	}

	if preferred == nil {
		session := p.svc.CreateSession(agentID)
		p.reloadSessions()
		p.activeSessionID = session.ID
	} else {
		p.activeSessionID = preferred.ID
	}
	p.reloadMessages()
	p.stickToBottom()
	p.focusDraft = true
}

// OnShow 在切换到本页时刷新数据，主助手可能刚创建了 Agent
// 或产生了新的委派会话。生成中不动消息列表，避免打断流式渲染。
func (p *Page) OnShow() {
	p.reloadAgents()
	p.reloadModels()
	p.reloadSessions()
	if !p.streamingActive() {
		p.reloadMessages()
	}
}

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
		p.reloadTasks()
		return
	}
	p.sessions = p.svc.Sessions(agent.ID)
	p.reloadTasks()
}

func (p *Page) reloadMessages() {
	p.messages = nil
	p.oldestSeq = 0
	p.hasMore = false
	conv := p.conversationID()
	if p.svc == nil || conv == "" {
		return
	}
	page := p.svc.MessagesPage(conv, 0, contract.DefaultMessagePageSize)
	p.messages = page.Items
	p.hasMore = page.HasMore
	if len(page.Items) > 0 {
		p.oldestSeq = page.Items[0].Seq
	}
}

// loadEarlier 加载一页更早的消息并插到列表头部。
// 头部插入会抬高列表，记录当前滚动位置供下一帧补偿。
func (p *Page) loadEarlier() {
	conv := p.conversationID()
	if p.svc == nil || conv == "" || !p.hasMore || p.oldestSeq <= 0 {
		return
	}
	page := p.svc.MessagesPage(conv, p.oldestSeq, contract.DefaultMessagePageSize)
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

// groupBusy 表示当前群聊仍在协作轮次中，成员发言期间暂停发送。
func (p *Page) groupBusy() bool {
	if p.svc == nil || p.activeGroup() == nil {
		return false
	}
	return p.svc.GroupBusy(p.conversationID())
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
	if p.activeTask() != nil {
		return "任务线程 · 由主助手派发，只读"
	}
	if agent := p.activeAgent(); agent != nil {
		desc := agent.ModelName
		if desc == "" {
			desc = "默认模型"
		}
		// 有后台任务时在标题下补充排队与运行中数量。
		queued, running := p.taskCounts(agent.ID)
		if queued > 0 || running > 0 {
			desc += fmt.Sprintf(" · 运行中 %d · 排队 %d", running, queued)
		}
		return desc
	}
	if group := p.activeGroup(); group != nil {
		return fmt.Sprintf("%d 个 Agent", len(group.MemberIDs)+1)
	}
	return ""
}

// taskCounts 统计当前员工排队中与运行中的任务数。
func (p *Page) taskCounts(agentID string) (queued, running int) {
	for _, task := range p.tasksByThread {
		if task.AssigneeID != agentID {
			continue
		}
		switch task.Status {
		case contract.TaskQueued:
			queued++
		case contract.TaskRunning:
			running++
		}
	}
	return queued, running
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

func firstRuneOr(text, fallback string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return fallback
	}
	return string(runes[0])
}
