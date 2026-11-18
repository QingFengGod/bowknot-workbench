package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/platform/id"
	"bowknot-workbench/internal/store"
)

const assistantOwnerID = "main"

// AssistantService 是助手页的持久化实现，消息与会话统一落在 store。
type AssistantService struct {
	store     *store.Store
	logger    *slog.Logger
	runner    *streamRunner
	provider  llm.Provider
	tools     *tool.Service
	approvals *ApprovalHub
	memory    *MemoryService
	artifacts *ArtifactService
	// models 是启动时从网关缓存的可选模型，UI 只从这里取列表。
	models []contract.ChatModel
	// greeted 保证每次应用启动只主动问候一次。
	greeted atomic.Bool
	// revision 在后台追加消息时递增，UI 据此刷新当前会话。
	revision atomic.Uint64
}

func NewAssistantService(st *store.Store, logger *slog.Logger, provider llm.Provider, tools *tool.Service, approvals *ApprovalHub) *AssistantService {
	if logger == nil {
		logger = slog.Default()
	}
	if provider == nil {
		provider = llm.NewFake(llm.FakeOptions{})
	}
	return &AssistantService{
		store:     st,
		logger:    logger,
		runner:    newStreamRunner(),
		provider:  provider,
		tools:     tools,
		approvals: approvals,
	}
}

// SetMemory 注入长期记忆服务；为 nil 时关闭记忆注入与抽取。
func (s *AssistantService) SetMemory(memory *MemoryService) {
	s.memory = memory
}

// SetArtifacts 注入产物服务，用于消息读取时附加产物摘要。
func (s *AssistantService) SetArtifacts(artifacts *ArtifactService) {
	s.artifacts = artifacts
}

// SetModels 注入启动时解析到的真实模型列表。
func (s *AssistantService) SetModels(models []contract.ChatModel) {
	s.models = append([]contract.ChatModel(nil), models...)
}

// ResolveApproval 回传危险工具的用户确认结果。
func (s *AssistantService) ResolveApproval(runID, callID string, approved bool) {
	if s.approvals != nil {
		s.approvals.Resolve(runID, callID, approved)
	}
}

// Shutdown 停止所有流式任务并等待它们把最终状态写入数据库。
func (s *AssistantService) Shutdown(ctx context.Context) error {
	s.runner.stopAll(ctx)
	return nil
}

func (s *AssistantService) Sessions() []contract.ChatSession {
	items, err := s.store.Conversations.List(context.Background(), domain.KindAssistant, assistantOwnerID)
	if err != nil {
		s.logger.Error("读取助手会话失败", "error", err)
		return nil
	}
	out := make([]contract.ChatSession, 0, len(items))
	for _, item := range items {
		out = append(out, toChatSession(item))
	}
	return out
}

func (s *AssistantService) Messages(sessionID string) []contract.ChatMessage {
	items, err := s.store.Messages.List(context.Background(), sessionID)
	if err != nil {
		s.logger.Error("读取助手消息失败", "session", sessionID, "error", err)
		return nil
	}
	calls, err := s.store.ToolCalls.ListByConversation(context.Background(), sessionID)
	if err != nil {
		s.logger.Error("读取工具调用失败", "session", sessionID, "error", err)
	}
	out := toContractMessages(items, sessionID, calls)
	attachTaskBriefs(s.store, out)
	attachArtifacts(s.store, s.artifacts, out)
	return out
}

// MessagesPage 分页读取助手消息，beforeSeq <= 0 表示从最新开始。
func (s *AssistantService) MessagesPage(sessionID string, beforeSeq int64, limit int) contract.MessagePage {
	ctx := context.Background()
	items, hasMore, err := s.store.Messages.ListPage(ctx, sessionID, beforeSeq, limit)
	if err != nil {
		s.logger.Error("读取助手消息失败", "session", sessionID, "error", err)
		return contract.MessagePage{}
	}
	calls, err := s.store.ToolCalls.ListByConversation(ctx, sessionID)
	if err != nil {
		s.logger.Error("读取工具调用失败", "session", sessionID, "error", err)
	}
	out := toContractMessages(items, sessionID, calls)
	attachTaskBriefs(s.store, out)
	attachArtifacts(s.store, s.artifacts, out)
	return contract.MessagePage{Items: out, HasMore: hasMore}
}

func (s *AssistantService) Models() []contract.ChatModel {
	return append([]contract.ChatModel(nil), s.models...)
}

// Persona 返回主助手的人设，未配置时返回默认人设。
func (s *AssistantService) Persona() contract.AssistantPersona {
	persona, err := loadPersona(context.Background(), s.store)
	if err != nil {
		s.logger.Error("读取助手人设失败", "error", err)
		return contract.DefaultAssistantPersona()
	}
	return persona
}

// SetPersona 保存主助手的人设。
func (s *AssistantService) SetPersona(persona contract.AssistantPersona) error {
	return savePersona(context.Background(), s.store, persona)
}

// Greeting 返回进入空对话时的主动问候：首次使用自我介绍，之后按最近记忆关心近况。
func (s *AssistantService) Greeting() string {
	persona := s.Persona()
	name := persona.Name
	if name == "" {
		name = contract.DefaultAssistantPersona().Name
	}
	role := persona.Role
	if role == "" {
		role = contract.DefaultAssistantPersona().Role
	}

	sessions, err := s.store.Conversations.List(context.Background(), domain.KindAssistant, assistantOwnerID)
	if err == nil && len(sessions) == 0 {
		if persona.UserName != "" {
			return fmt.Sprintf("你好呀，我是%s，%s。%s，今天想聊点什么？", name, role, persona.UserName)
		}
		return fmt.Sprintf("你好呀，我是%s，%s。我该怎么称呼你呢？", name, role)
	}

	prefix := ""
	if persona.UserName != "" {
		prefix = persona.UserName + "，"
	}
	if s.memory != nil {
		for _, item := range s.memory.Memories("user", "") {
			if content := strings.TrimSpace(item.Content); content != "" {
				return fmt.Sprintf("%s最近怎么样？上次你提到「%s」，后来还顺利吗？", prefix, snippet(personalGreetingText(content, persona.UserName), 24))
			}
		}
		for _, item := range s.memory.Memories("agent", mainAgentID) {
			if content := strings.TrimSpace(item.Content); content != "" {
				return fmt.Sprintf("%s最近怎么样？上次你提到「%s」，后来还顺利吗？", prefix, snippet(personalGreetingText(content, persona.UserName), 24))
			}
		}
	}
	return prefix + "最近怎么样？有什么想聊的，随时叫我。"
}

// GreetOnLaunch 在每次应用启动后的首次进入时主动问候：
// 没有会话时创建默认会话，并把问候作为普通助手消息流式写入历史。
func (s *AssistantService) GreetOnLaunch() (string, string, <-chan contract.ChatEvent, bool) {
	if !s.greeted.CompareAndSwap(false, true) {
		return "", "", nil, false
	}

	// 先算问候语，避免刚创建的默认会话被误判成「非首次」。
	ctx := context.Background()
	sessions, err := s.store.Conversations.List(ctx, domain.KindAssistant, assistantOwnerID)
	if err != nil {
		s.logger.Error("读取助手会话失败", "error", err)
		return "", "", nil, false
	}
	first := len(sessions) == 0
	fallback := s.Greeting()

	sessionID := ""
	lastChatAt := int64(0)
	if len(sessions) > 0 {
		// 问候放在最近更新的会话里，而不是可能被置顶的旧会话。
		latest := sessions[0]
		for _, item := range sessions[1:] {
			if item.UpdatedAt > latest.UpdatedAt {
				latest = item
			}
		}
		sessionID = latest.ID
		lastChatAt = latest.UpdatedAt
	} else {
		session := s.CreateSession()
		if session.ID == "" {
			return "", "", nil, false
		}
		sessionID = session.ID
	}

	messageID := id.New()
	runID := id.New()
	now := time.Now().UnixMilli()
	if err := s.store.Tx(ctx, func(tx *store.Store) error {
		if _, err := tx.Messages.Append(ctx, domain.Message{
			ID:             messageID,
			ConversationID: sessionID,
			Role:           domain.RoleAssistant,
			Status:         domain.MessageStreaming,
			CreatedAt:      now,
			SenderKind:     domain.SenderMain,
			SenderID:       mainAgentID,
			MessageType:    domain.MessageTypeChat,
			Visibility:     domain.VisibilityUser,
		}); err != nil {
			return err
		}
		return tx.Runs.Start(ctx, domain.Run{
			ID:             runID,
			ConversationID: sessionID,
			MessageID:      messageID,
			Status:         domain.RunRunning,
			StartedAt:      now,
		})
	}); err != nil {
		s.logger.Error("创建问候消息失败", "session", sessionID, "error", err)
		return "", "", nil, false
	}
	// 问候会更新会话活跃时间，让它排在列表最前。
	if err := s.store.Conversations.Touch(ctx, sessionID, now); err != nil {
		s.logger.Error("更新会话活跃时间失败", "session", sessionID, "error", err)
	}

	events := make(chan contract.ChatEvent, eventBuffer)
	go s.streamGreeting(messageID, runID, first, fallback, lastChatAt, events)
	return sessionID, messageID, events, true
}

// generateGreeting 让模型结合人设、当前时间与最近记忆生成问候语，失败时返回空串由调用方兜底。
func (s *AssistantService) generateGreeting(first bool, lastChatAt int64) string {
	if s.provider == nil {
		return ""
	}

	system := s.systemPrompt()
	if s.memory != nil {
		if block := s.memory.PromptBlock(mainAgentID, ""); block != "" {
			system += "\n\n" + block
		}
	}

	now := time.Now()
	timeInfo := fmt.Sprintf("\n\n【时间信息】当前时间：%s %s。", now.Format("2006-01-02 15:04"), chineseWeekday(now))
	if lastChatAt > 0 {
		timeInfo += "上次对话时间：" + time.UnixMilli(lastChatAt).In(time.Local).Format("2006-01-02 15:04") + "。"
	}
	timeInfo += "请据此判断真实的时间间隔：刚刚发生的事不要说成「前几天」；记忆条目带有日期，只有日期确实较早时才使用相应的时间描述。"
	system += timeInfo
	if first {
		system += "\n\n【当前任务】这是用户第一次使用，请用一两句话介绍自己（使用上面人设里的姓名与角色），并询问用户希望你怎么称呼。不要编造任何记忆。"
	} else {
		system += "\n\n【当前任务】这不是用户第一次使用。请结合最近记忆，用一两句话主动关心用户近况，可以提到一件具体的事；不要编造记忆之外的内容。没有记忆时泛泛问候即可。"
	}
	system += "只输出问候语本身，不要加引号、标题、表情或额外解释。"
	system += "记忆里的「用户」指用户本人；输出时必须改成对用户的称呼（没有称呼就用「你」），不要出现「用户」这个词。"

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	model := ""
	if len(s.models) > 0 {
		model = s.models[0].ID
	}
	text, err := s.provider.Complete(ctx, llm.ChatRequest{
		Model:    model,
		System:   system,
		Messages: []llm.Message{{Role: "user", Content: "请生成这次的问候语。"}},
	})
	if err != nil {
		s.logger.Warn("生成问候语失败，使用默认文案", "error", err)
		return ""
	}

	text = strings.TrimSpace(text)
	text = strings.Trim(text, "\"“”'")
	text = personalGreetingText(text, s.Persona().UserName)
	// 个别模型会返回 JSON 或空结构，这类结果不适合当问候。
	if text == "" || strings.HasPrefix(text, "[") || strings.HasPrefix(text, "{") {
		return ""
	}
	runes := []rune(text)
	if len(runes) > 120 {
		text = string(runes[:120]) + "…"
	}
	return text
}

// streamGreeting 按小块输出问候文本，走与模型回复一致的流式事件与状态落库。
func (s *AssistantService) streamGreeting(messageID, runID string, first bool, fallback string, lastChatAt int64, events chan<- contract.ChatEvent) {
	defer close(events)

	content := s.generateGreeting(first, lastChatAt)
	if content == "" {
		content = fallback
	}

	runes := []rune(content)
	var text strings.Builder
	for i := 0; i < len(runes); i += 2 {
		end := i + 2
		if end > len(runes) {
			end = len(runes)
		}
		text.WriteString(string(runes[i:end]))
		events <- contract.ChatEvent{
			Type:      contract.ChatEventText,
			RunID:     runID,
			MessageID: messageID,
			Content:   text.String(),
			Status:    contract.MessageStreaming,
		}
		time.Sleep(30 * time.Millisecond)
	}

	ctx := context.Background()
	now := time.Now().UnixMilli()
	final := text.String()
	if err := s.store.Messages.Update(ctx, domain.Message{
		ID:      messageID,
		Content: final,
		Status:  domain.MessageDone,
	}); err != nil {
		s.logger.Error("保存问候消息失败", "error", err)
	}
	if err := s.store.Runs.Finish(ctx, runID, domain.RunDone, now); err != nil {
		s.logger.Error("结束问候任务失败", "error", err)
	}

	events <- contract.ChatEvent{
		Type:      contract.ChatEventDone,
		RunID:     runID,
		MessageID: messageID,
		Content:   final,
		Status:    contract.MessageDone,
		Done:      true,
	}
}

// chineseWeekday 返回中文星期，用于提示词里的时间信息。
func chineseWeekday(t time.Time) string {
	return [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[t.Weekday()]
}

// systemPrompt 把人设拼到内置助手规则之后。
func (s *AssistantService) systemPrompt() string {
	persona, err := loadPersona(context.Background(), s.store)
	if err != nil {
		s.logger.Error("读取助手人设失败", "error", err)
		persona = contract.DefaultAssistantPersona()
	}
	if block := personaPrompt(persona); block != "" {
		return assistantSystemPrompt + "\n\n" + block
	}
	return assistantSystemPrompt
}

// Revision 返回后台消息版本的当前值，UI 用它判断是否需要重新加载。
func (s *AssistantService) Revision() uint64 {
	return s.revision.Load()
}

func (s *AssistantService) CreateSession() contract.ChatSession {
	now := time.Now().UnixMilli()
	item := domain.Conversation{
		ID:        "session-" + id.New(),
		Kind:      domain.KindAssistant,
		OwnerID:   assistantOwnerID,
		Title:     "新会话",
		Summary:   "还没有消息",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.Conversations.Create(context.Background(), item); err != nil {
		s.logger.Error("创建助手会话失败", "error", err)
		return contract.ChatSession{}
	}
	return toChatSession(item)
}

func (s *AssistantService) RenameSession(sessionID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("会话标题不能为空")
	}

	ctx := context.Background()
	item, err := s.store.Conversations.Get(ctx, sessionID)
	if err != nil {
		return conversationError(err, sessionID)
	}
	if err := s.store.Conversations.UpdateMeta(ctx, sessionID, title, item.Summary, item.Pinned, item.UpdatedAt); err != nil {
		return conversationError(err, sessionID)
	}
	return nil
}

func (s *AssistantService) DeleteSession(sessionID string) error {
	ctx := context.Background()
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	s.runner.stopAndWait(waitCtx, sessionID)

	if err := s.store.Conversations.Delete(ctx, sessionID); err != nil {
		return conversationError(err, sessionID)
	}
	return nil
}

func (s *AssistantService) PinSession(sessionID string, pinned bool) error {
	ctx := context.Background()
	item, err := s.store.Conversations.Get(ctx, sessionID)
	if err != nil {
		return conversationError(err, sessionID)
	}
	if err := s.store.Conversations.UpdateMeta(ctx, sessionID, item.Title, item.Summary, pinned, item.UpdatedAt); err != nil {
		return conversationError(err, sessionID)
	}
	return nil
}

func (s *AssistantService) Send(sessionID, text, modelID string) (string, <-chan contract.ChatEvent, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil, errors.New("请输入消息内容")
	}
	if err := modelAvailable(s.models, modelID); err != nil {
		return "", nil, err
	}

	now := time.Now().UnixMilli()
	userID := id.New()
	messageID := id.New()
	runID := id.New()

	slot, ok := s.runner.begin(sessionID, messageID)
	if !ok {
		return "", nil, errors.New("当前会话正在生成回复，请先停止")
	}

	ctx := context.Background()
	var history []domain.Message
	if err := s.store.Tx(ctx, func(tx *store.Store) error {
		item, err := tx.Conversations.Get(ctx, sessionID)
		if err != nil {
			return err
		}

		messages, err := tx.Messages.List(ctx, sessionID)
		if err != nil {
			return err
		}
		history = messages
		title := item.Title
		if title == "新会话" || len(messages) == 0 {
			title = snippet(text, 20)
		}

		if _, err := tx.Messages.Append(ctx, domain.Message{
			ID:             userID,
			ConversationID: sessionID,
			Role:           domain.RoleUser,
			Content:        text,
			Status:         domain.MessageDone,
			CreatedAt:      now,
			SenderKind:     domain.SenderUser,
			MessageType:    domain.MessageTypeChat,
			Visibility:     domain.VisibilityUser,
		}); err != nil {
			return err
		}
		// 网关请求必须包含本轮用户消息，否则模型看不到最新问题。
		history = append(history, domain.Message{
			ID:             userID,
			ConversationID: sessionID,
			Role:           domain.RoleUser,
			Content:        text,
			Status:         domain.MessageDone,
			CreatedAt:      now,
			SenderKind:     domain.SenderUser,
			MessageType:    domain.MessageTypeChat,
			Visibility:     domain.VisibilityUser,
		})
		if _, err := tx.Messages.Append(ctx, domain.Message{
			ID:             messageID,
			ConversationID: sessionID,
			Role:           domain.RoleAssistant,
			Status:         domain.MessageStreaming,
			CreatedAt:      now,
			SenderKind:     domain.SenderMain,
			SenderID:       mainAgentID,
			MessageType:    domain.MessageTypeChat,
			Visibility:     domain.VisibilityUser,
		}); err != nil {
			return err
		}
		if err := tx.Runs.Start(ctx, domain.Run{
			ID:             runID,
			ConversationID: sessionID,
			MessageID:      messageID,
			Status:         domain.RunRunning,
			StartedAt:      now,
		}); err != nil {
			return err
		}
		return tx.Conversations.UpdateMeta(ctx, sessionID, title, snippet(text, 60), item.Pinned, now)
	}); err != nil {
		s.runner.finish(sessionID, slot)
		return "", nil, conversationError(err, sessionID)
	}

	events := make(chan contract.ChatEvent, eventBuffer)
	system := s.systemPrompt()
	if s.memory != nil {
		if block := s.memory.PromptBlock(mainAgentID, text); block != "" {
			system = system + "\n\n" + block
		}
	}
	memoryWritten := false
	go runGeneration(genStream{
		store:     s.store,
		logger:    s.logger,
		runner:    s.runner,
		runnerKey: sessionID,
		message: domain.Message{
			ID:             messageID,
			ConversationID: sessionID,
			Role:           domain.RoleAssistant,
			Status:         domain.MessageStreaming,
			CreatedAt:      now,
		},
		runID:    runID,
		provider: s.provider,
		req: llm.ChatRequest{
			Model:    modelID,
			System:   system,
			Messages: toLLMMessages(history),
		},
		events:        events,
		slot:          slot,
		tools:         s.tools,
		approvals:     s.approvals,
		agentID:       mainAgentID,
		memoryWritten: &memoryWritten,
	})
	return messageID, events, nil
}

func (s *AssistantService) AutoReply(sessionID string) (string, <-chan contract.ChatEvent, bool) {
	// 只有演示会话 daily 会在首次进入时自动播放，避免新会话无端产生回复。
	if sessionID != "daily" {
		return "", nil, false
	}

	messageID := id.New()
	runID := id.New()
	slot, ok := s.runner.begin(sessionID, messageID)
	if !ok {
		return "", nil, false
	}

	ctx := context.Background()
	var history []domain.Message
	if err := s.store.Tx(ctx, func(tx *store.Store) error {
		if _, err := tx.Conversations.Get(ctx, sessionID); err != nil {
			return err
		}
		messages, err := tx.Messages.List(ctx, sessionID)
		if err != nil {
			return err
		}
		for _, message := range messages {
			if message.Role == domain.RoleAssistant {
				return errAlreadyReplied
			}
		}
		history = messages
		if _, err := tx.Messages.Append(ctx, domain.Message{
			ID:             messageID,
			ConversationID: sessionID,
			Role:           domain.RoleAssistant,
			Status:         domain.MessageStreaming,
			CreatedAt:      time.Now().UnixMilli(),
			SenderKind:     domain.SenderMain,
			SenderID:       mainAgentID,
			MessageType:    domain.MessageTypeChat,
			Visibility:     domain.VisibilityUser,
		}); err != nil {
			return err
		}
		return tx.Runs.Start(ctx, domain.Run{
			ID:             runID,
			ConversationID: sessionID,
			MessageID:      messageID,
			Status:         domain.RunRunning,
			StartedAt:      time.Now().UnixMilli(),
		})
	}); err != nil {
		s.runner.finish(sessionID, slot)
		if !errors.Is(err, errAlreadyReplied) {
			s.logger.Error("自动回复失败", "session", sessionID, "error", err)
		}
		return "", nil, false
	}

	events := make(chan contract.ChatEvent, eventBuffer)
	system := s.systemPrompt()
	if s.memory != nil {
		if block := s.memory.PromptBlock(mainAgentID, ""); block != "" {
			system = system + "\n\n" + block
		}
	}
	memoryWritten := false
	go runGeneration(genStream{
		store:     s.store,
		logger:    s.logger,
		runner:    s.runner,
		runnerKey: sessionID,
		message: domain.Message{
			ID:             messageID,
			ConversationID: sessionID,
			Role:           domain.RoleAssistant,
			Status:         domain.MessageStreaming,
			CreatedAt:      time.Now().UnixMilli(),
		},
		runID:    runID,
		provider: s.provider,
		req: llm.ChatRequest{
			System:   system,
			Messages: toLLMMessages(history),
		},
		events:        events,
		slot:          slot,
		tools:         s.tools,
		approvals:     s.approvals,
		agentID:       mainAgentID,
		memoryWritten: &memoryWritten,
	})
	return messageID, events, true
}

func (s *AssistantService) Regenerate(sessionID, messageID string) (<-chan contract.ChatEvent, error) {
	ctx := context.Background()
	if _, err := s.store.Conversations.Get(ctx, sessionID); err != nil {
		return nil, conversationError(err, sessionID)
	}

	messages, err := s.store.Messages.List(ctx, sessionID)
	if err != nil {
		return nil, conversationError(err, sessionID)
	}
	var target domain.Message
	found := false
	for _, message := range messages {
		if message.ID == messageID {
			target = message
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("消息不存在")
	}
	if target.Role != domain.RoleAssistant {
		return nil, errors.New("只能重新生成助手消息")
	}

	slot, ok := s.runner.begin(sessionID, messageID)
	if !ok {
		return nil, errors.New("当前会话正在生成回复，请先停止")
	}

	// 重新生成时排除目标消息本身，让模型基于此前的上下文重新作答。
	history := make([]domain.Message, 0, len(messages))
	for _, message := range messages {
		if message.ID != messageID {
			history = append(history, message)
		}
	}

	runID := id.New()
	now := time.Now().UnixMilli()
	target.Content = ""
	target.Status = domain.MessageStreaming
	target.Error = ""
	target.MemoryWritten = false

	if err := s.store.Tx(ctx, func(tx *store.Store) error {
		if err := tx.Messages.Update(ctx, target); err != nil {
			return err
		}
		return tx.Runs.Start(ctx, domain.Run{
			ID:             runID,
			ConversationID: sessionID,
			MessageID:      messageID,
			Status:         domain.RunRunning,
			StartedAt:      now,
		})
	}); err != nil {
		s.runner.finish(sessionID, slot)
		return nil, conversationError(err, sessionID)
	}

	events := make(chan contract.ChatEvent, eventBuffer)
	question := lastDomainUserText(history)
	system := s.systemPrompt()
	if s.memory != nil {
		if block := s.memory.PromptBlock(mainAgentID, question); block != "" {
			system = system + "\n\n" + block
		}
	}
	memoryWritten := false
	go runGeneration(genStream{
		store:     s.store,
		logger:    s.logger,
		runner:    s.runner,
		runnerKey: sessionID,
		message:   target,
		runID:     runID,
		provider:  s.provider,
		req: llm.ChatRequest{
			System:   system,
			Messages: toLLMMessages(history),
		},
		events:        events,
		slot:          slot,
		tools:         s.tools,
		approvals:     s.approvals,
		agentID:       mainAgentID,
		memoryWritten: &memoryWritten,
	})
	return events, nil
}

func (s *AssistantService) DeleteMessage(sessionID, messageID string) error {
	if s.runner.streamingMessage(sessionID) == messageID {
		waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		s.runner.stopAndWait(waitCtx, sessionID)
		cancel()
	}
	if err := s.store.Messages.Delete(context.Background(), messageID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return errors.New("消息不存在")
		}
		return fmt.Errorf("删除消息失败：%w", err)
	}
	return nil
}

func (s *AssistantService) Stop(sessionID string) {
	s.runner.stop(sessionID)
}

var errAlreadyReplied = errors.New("会话已有助手回复")
