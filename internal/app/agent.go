package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/platform/id"
	"bowknot-workbench/internal/store"
)

// AgentService 是 agents 页的持久化实现，Agent、群聊与会话统一落在 store。
type AgentService struct {
	store     *store.Store
	logger    *slog.Logger
	runner    *streamRunner
	provider  llm.Provider
	tools     *tool.Service
	approvals *ApprovalHub
	memory    *MemoryService
	// models 是启动时从网关缓存的可选模型，UI 只从这里取列表。
	models []contract.ChatModel
}

func NewAgentService(st *store.Store, logger *slog.Logger, provider llm.Provider, tools *tool.Service, approvals *ApprovalHub) *AgentService {
	if logger == nil {
		logger = slog.Default()
	}
	if provider == nil {
		provider = llm.NewFake(llm.FakeOptions{})
	}
	return &AgentService{
		store:     st,
		logger:    logger,
		runner:    newStreamRunner(),
		provider:  provider,
		tools:     tools,
		approvals: approvals,
	}
}

// SetMemory 注入长期记忆服务；为 nil 时关闭记忆注入与抽取。
func (s *AgentService) SetMemory(memory *MemoryService) {
	s.memory = memory
}

// SetModels 注入启动时解析到的真实模型列表。
func (s *AgentService) SetModels(models []contract.ChatModel) {
	s.models = append([]contract.ChatModel(nil), models...)
}

// ResolveApproval 回传危险工具的用户确认结果。
func (s *AgentService) ResolveApproval(runID, callID string, approved bool) {
	if s.approvals != nil {
		s.approvals.Resolve(runID, callID, approved)
	}
}

func (s *AgentService) Shutdown(ctx context.Context) error {
	s.runner.stopAll(ctx)
	return nil
}

func (s *AgentService) Agents() []contract.AgentInfo {
	items, err := s.store.Agents.List(context.Background())
	if err != nil {
		s.logger.Error("读取 Agent 列表失败", "error", err)
		return nil
	}
	out := make([]contract.AgentInfo, 0, len(items))
	for _, item := range items {
		out = append(out, toAgentInfo(item, modelName(s.models, item.ModelID)))
	}
	return out
}

func (s *AgentService) Groups() []contract.AgentGroup {
	items, err := s.store.Groups.List(context.Background())
	if err != nil {
		s.logger.Error("读取群聊列表失败", "error", err)
		return nil
	}
	out := make([]contract.AgentGroup, 0, len(items))
	for _, item := range items {
		out = append(out, toAgentGroup(item))
	}
	return out
}

func (s *AgentService) Sessions(agentID string) []contract.AgentSession {
	items, err := s.store.Conversations.List(context.Background(), domain.KindAgent, agentID)
	if err != nil {
		s.logger.Error("读取 Agent 会话失败", "agent", agentID, "error", err)
		return nil
	}
	out := make([]contract.AgentSession, 0, len(items))
	for _, item := range items {
		out = append(out, toAgentSession(item))
	}
	return out
}

func (s *AgentService) Messages(conversationID string) []contract.ChatMessage {
	storeID, _, _ := resolveConversationID(conversationID)
	items, err := s.store.Messages.List(context.Background(), storeID)
	if err != nil {
		s.logger.Error("读取 Agent 消息失败", "conversation", conversationID, "error", err)
		return nil
	}
	calls, err := s.store.ToolCalls.ListByConversation(context.Background(), storeID)
	if err != nil {
		s.logger.Error("读取 Agent 工具调用失败", "conversation", conversationID, "error", err)
	}
	return toContractMessages(items, conversationID, calls)
}

// MessagesPage 分页读取 Agent 消息，beforeSeq <= 0 表示从最新开始。
func (s *AgentService) MessagesPage(conversationID string, beforeSeq int64, limit int) contract.MessagePage {
	storeID, _, _ := resolveConversationID(conversationID)
	ctx := context.Background()
	items, hasMore, err := s.store.Messages.ListPage(ctx, storeID, beforeSeq, limit)
	if err != nil {
		s.logger.Error("读取 Agent 消息失败", "conversation", conversationID, "error", err)
		return contract.MessagePage{}
	}
	calls, err := s.store.ToolCalls.ListByConversation(ctx, storeID)
	if err != nil {
		s.logger.Error("读取 Agent 工具调用失败", "conversation", conversationID, "error", err)
	}
	return contract.MessagePage{Items: toContractMessages(items, conversationID, calls), HasMore: hasMore}
}

func (s *AgentService) Models() []contract.ChatModel {
	return append([]contract.ChatModel(nil), s.models...)
}

func (s *AgentService) CreateAgent(name, description, persona string) contract.AgentInfo {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if description == "" {
		description = "暂无描述"
	}
	persona = strings.TrimSpace(persona)
	if persona == "" {
		// 创建时就把默认人设落库，让 Agent 有独立的人设起点，而不是共用通用提示词。
		persona = personaText(defaultAgentProfile(name, description))
	}
	now := time.Now().UnixMilli()
	modelID := ""
	if len(s.models) > 0 {
		modelID = s.models[0].ID
	}
	item := domain.Agent{
		ID:          "agent-" + id.New(),
		Name:        name,
		Description: description,
		AvatarText:  firstRune(name),
		Persona:     persona,
		ModelID:     modelID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.Agents.Upsert(context.Background(), item); err != nil {
		s.logger.Error("创建 Agent 失败", "error", err)
		return contract.AgentInfo{}
	}
	return toAgentInfo(item, modelName(s.models, item.ModelID))
}

func (s *AgentService) UpdateAgent(agentID, name, description, persona string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("Agent 名称不能为空")
	}

	ctx := context.Background()
	item, err := s.store.Agents.Get(ctx, agentID)
	if err != nil {
		return agentError(err, agentID)
	}
	item.Name = name
	item.Description = strings.TrimSpace(description)
	item.AvatarText = firstRune(name)
	persona = strings.TrimSpace(persona)
	if persona == "" {
		persona = personaText(defaultAgentProfile(name, item.Description))
	}
	item.Persona = persona
	item.UpdatedAt = time.Now().UnixMilli()
	if err := s.store.Agents.Upsert(ctx, item); err != nil {
		return agentError(err, agentID)
	}
	return nil
}

func (s *AgentService) RemoveAgent(agentID string) error {
	ctx := context.Background()
	if _, err := s.store.Agents.Get(ctx, agentID); err != nil {
		return agentError(err, agentID)
	}

	sessions, err := s.store.Conversations.List(ctx, domain.KindAgent, agentID)
	if err != nil {
		return agentError(err, agentID)
	}
	for _, session := range sessions {
		waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		s.runner.stopAndWait(waitCtx, session.ID)
		cancel()
	}

	if err := s.store.Tx(ctx, func(tx *store.Store) error {
		for _, session := range sessions {
			if err := tx.Conversations.Delete(ctx, session.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		groups, err := tx.Groups.List(ctx)
		if err != nil {
			return err
		}
		for _, group := range groups {
			members, changed := removeID(group.MemberIDs, agentID)
			if !changed {
				continue
			}
			group.MemberIDs = members
			if err := tx.Groups.Upsert(ctx, group); err != nil {
				return err
			}
		}
		return tx.Agents.Delete(ctx, agentID)
	}); err != nil {
		return agentError(err, agentID)
	}
	return nil
}

func (s *AgentService) CreateGroup(name string, memberIDs []string) contract.AgentGroup {
	item := domain.Group{
		ID:        "group-" + id.New(),
		Name:      strings.TrimSpace(name),
		MemberIDs: append([]string(nil), memberIDs...),
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := s.store.Groups.Upsert(context.Background(), item); err != nil {
		s.logger.Error("创建群聊失败", "error", err)
		return contract.AgentGroup{}
	}
	return toAgentGroup(item)
}

func (s *AgentService) UpdateGroup(groupID, name string, memberIDs []string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("群聊名称不能为空")
	}
	if len(memberIDs) == 0 {
		return errors.New("群聊至少需要一名成员")
	}

	ctx := context.Background()
	item, err := s.store.Groups.Get(ctx, groupID)
	if err != nil {
		return groupError(err, groupID)
	}
	item.Name = name
	item.MemberIDs = append([]string(nil), memberIDs...)
	if err := s.store.Groups.Upsert(ctx, item); err != nil {
		return groupError(err, groupID)
	}
	return nil
}

func (s *AgentService) RemoveGroup(groupID string) error {
	ctx := context.Background()
	if _, err := s.store.Groups.Get(ctx, groupID); err != nil {
		return groupError(err, groupID)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	s.runner.stopAndWait(waitCtx, groupKey(groupID))
	cancel()

	if err := s.store.Tx(ctx, func(tx *store.Store) error {
		if err := tx.Conversations.Delete(ctx, groupConversationID(groupID)); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return tx.Groups.Delete(ctx, groupID)
	}); err != nil {
		return groupError(err, groupID)
	}
	return nil
}

func (s *AgentService) Pin(kind, targetID string, pinned bool) error {
	ctx := context.Background()
	if kind == contract.ItemKindGroup {
		item, err := s.store.Groups.Get(ctx, targetID)
		if err != nil {
			return groupError(err, targetID)
		}
		item.Pinned = pinned
		if err := s.store.Groups.Upsert(ctx, item); err != nil {
			return groupError(err, targetID)
		}
		return nil
	}

	item, err := s.store.Agents.Get(ctx, targetID)
	if err != nil {
		return agentError(err, targetID)
	}
	item.Pinned = pinned
	if err := s.store.Agents.Upsert(ctx, item); err != nil {
		return agentError(err, targetID)
	}
	return nil
}

func (s *AgentService) CreateSession(agentID string) contract.AgentSession {
	now := time.Now().UnixMilli()
	item := domain.Conversation{
		ID:        "conv-" + id.New(),
		Kind:      domain.KindAgent,
		OwnerID:   agentID,
		Title:     "新会话",
		Summary:   "还没有消息",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.Conversations.Create(context.Background(), item); err != nil {
		s.logger.Error("创建 Agent 会话失败", "agent", agentID, "error", err)
		return contract.AgentSession{}
	}
	return toAgentSession(item)
}

func (s *AgentService) RenameSession(sessionID, title string) error {
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

func (s *AgentService) DeleteSession(sessionID string) error {
	ctx := context.Background()
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	s.runner.stopAndWait(waitCtx, sessionID)
	cancel()

	if err := s.store.Conversations.Delete(ctx, sessionID); err != nil {
		return conversationError(err, sessionID)
	}
	return nil
}

func (s *AgentService) PinSession(sessionID string, pinned bool) error {
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

func (s *AgentService) Send(conversationID, text, _ string) (string, <-chan contract.ChatEvent, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil, errors.New("请输入消息内容")
	}

	storeID, groupID, isGroup := resolveConversationID(conversationID)
	var group domain.Group
	if isGroup {
		var err error
		group, err = s.store.Groups.Get(context.Background(), groupID)
		if err != nil {
			return "", nil, groupError(err, groupID)
		}
	}

	now := time.Now().UnixMilli()
	userID := id.New()
	messageID := id.New()
	runID := id.New()

	slot, ok := s.runner.begin(conversationID, messageID)
	if !ok {
		return "", nil, errors.New("当前会话正在生成回复，请先停止")
	}

	ctx := context.Background()
	senderName, senderAvatar := "", ""
	if isGroup {
		senderName, senderAvatar = "助理", "助"
	}
	var conversation domain.Conversation
	if err := s.store.Tx(ctx, func(tx *store.Store) error {
		item, err := tx.Conversations.Get(ctx, storeID)
		if errors.Is(err, store.ErrNotFound) && isGroup {
			item = domain.Conversation{
				ID:        storeID,
				Kind:      domain.KindGroup,
				OwnerID:   groupID,
				Title:     group.Name,
				Summary:   "群聊",
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := tx.Conversations.Create(ctx, item); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		conversation = item

		messages, err := tx.Messages.List(ctx, storeID)
		if err != nil {
			return err
		}
		title := item.Title
		if title == "新会话" || title == "" || len(messages) == 0 {
			if isGroup {
				title = group.Name
			} else {
				title = snippet(text, 20)
			}
		}

		if _, err := tx.Messages.Append(ctx, domain.Message{
			ID:             userID,
			ConversationID: storeID,
			Role:           domain.RoleUser,
			Content:        text,
			Status:         domain.MessageDone,
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		if _, err := tx.Messages.Append(ctx, domain.Message{
			ID:             messageID,
			ConversationID: storeID,
			Role:           domain.RoleAssistant,
			Status:         domain.MessageStreaming,
			CreatedAt:      now,
			SenderName:     senderName,
			SenderAvatar:   senderAvatar,
		}); err != nil {
			return err
		}
		if err := tx.Runs.Start(ctx, domain.Run{
			ID:             runID,
			ConversationID: storeID,
			MessageID:      messageID,
			Status:         domain.RunRunning,
			StartedAt:      now,
		}); err != nil {
			return err
		}
		return tx.Conversations.UpdateMeta(ctx, storeID, title, snippet(text, 60), item.Pinned, now)
	}); err != nil {
		s.runner.finish(conversationID, slot)
		return "", nil, conversationError(err, conversationID)
	}

	req := llm.ChatRequest{System: groupSystemPrompt}
	agentID := mainAgentID
	if !isGroup {
		agentID = conversation.OwnerID
		item, err := s.store.Agents.Get(ctx, conversation.OwnerID)
		if err != nil {
			s.logger.Warn("读取 Agent 人设失败，使用默认人设", "agent", conversation.OwnerID, "error", err)
			req = llm.ChatRequest{System: defaultAgentSystemPrompt}
		} else {
			req = llm.ChatRequest{System: s.agentSystemPrompt(item), Model: item.ModelID}
		}
	}
	history, err := s.store.Messages.List(ctx, storeID)
	if err != nil {
		s.runner.finish(conversationID, slot)
		return "", nil, conversationError(err, conversationID)
	}
	req.Messages = toLLMMessages(history)
	if s.memory != nil {
		if block := s.memory.PromptBlock(agentID, text); block != "" {
			req.System = strings.TrimSpace(req.System + "\n\n" + block)
		}
	}

	events := make(chan contract.ChatEvent, eventBuffer)
	go runGeneration(genStream{
		store:     s.store,
		logger:    s.logger,
		runner:    s.runner,
		runnerKey: conversationID,
		message: domain.Message{
			ID:             messageID,
			ConversationID: storeID,
			Role:           domain.RoleAssistant,
			Status:         domain.MessageStreaming,
			CreatedAt:      now,
			SenderName:     senderName,
			SenderAvatar:   senderAvatar,
		},
		runID:     runID,
		provider:  s.provider,
		req:       req,
		events:    events,
		slot:      slot,
		tools:     s.tools,
		approvals: s.approvals,
		agentID:   agentID,
		memory:    s.memory,
	})
	return messageID, events, nil
}

func (s *AgentService) Regenerate(conversationID, messageID string) (<-chan contract.ChatEvent, error) {
	storeID, _, isGroup := resolveConversationID(conversationID)
	ctx := context.Background()
	if _, err := s.store.Conversations.Get(ctx, storeID); err != nil {
		return nil, conversationError(err, conversationID)
	}

	messages, err := s.store.Messages.List(ctx, storeID)
	if err != nil {
		return nil, conversationError(err, conversationID)
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

	slot, ok := s.runner.begin(conversationID, messageID)
	if !ok {
		return nil, errors.New("当前会话正在生成回复，请先停止")
	}

	// 重新生成时排除目标消息本身，让模型基于此前的上下文重新作答。
	req := llm.ChatRequest{System: groupSystemPrompt}
	agentID := mainAgentID
	if !isGroup {
		conversation, err := s.store.Conversations.Get(ctx, storeID)
		if err != nil {
			s.runner.finish(conversationID, slot)
			return nil, conversationError(err, conversationID)
		}
		agentID = conversation.OwnerID
		item, err := s.store.Agents.Get(ctx, conversation.OwnerID)
		if err != nil {
			s.logger.Warn("读取 Agent 人设失败，使用默认人设", "agent", conversation.OwnerID, "error", err)
			req = llm.ChatRequest{System: defaultAgentSystemPrompt}
		} else {
			req = llm.ChatRequest{System: s.agentSystemPrompt(item), Model: item.ModelID}
		}
	}
	history := make([]domain.Message, 0, len(messages))
	for _, message := range messages {
		if message.ID != messageID {
			history = append(history, message)
		}
	}
	req.Messages = toLLMMessages(history)
	if s.memory != nil {
		if block := s.memory.PromptBlock(agentID, lastDomainUserText(history)); block != "" {
			req.System = strings.TrimSpace(req.System + "\n\n" + block)
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
			ConversationID: storeID,
			MessageID:      messageID,
			Status:         domain.RunRunning,
			StartedAt:      now,
		})
	}); err != nil {
		s.runner.finish(conversationID, slot)
		return nil, conversationError(err, conversationID)
	}

	events := make(chan contract.ChatEvent, eventBuffer)
	go runGeneration(genStream{
		store:     s.store,
		logger:    s.logger,
		runner:    s.runner,
		runnerKey: conversationID,
		message:   target,
		runID:     runID,
		provider:  s.provider,
		req:       req,
		events:    events,
		slot:      slot,
		tools:     s.tools,
		approvals: s.approvals,
		agentID:   agentID,
		memory:    s.memory,
	})
	return events, nil
}

func (s *AgentService) DeleteMessage(conversationID, messageID string) error {
	if s.runner.streamingMessage(conversationID) == messageID {
		waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		s.runner.stopAndWait(waitCtx, conversationID)
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

func (s *AgentService) Stop(conversationID string) {
	s.runner.stop(conversationID)
}

// agentSystemPrompt 返回 Agent 独立的结构化人设。
// 历史自由文本会退回到「其它要求」；其它字段为空时用名称与职责兜底。
// 子 Agent 对主助手的称呼固定为「主助手姓名 + 秘书长」。
func (s *AgentService) agentSystemPrompt(item domain.Agent) string {
	profile := parsePersonaText(item.Persona)
	if strings.TrimSpace(profile.Name) == "" {
		profile.Name = item.Name
	}
	block := personaPrompt(profile)
	if block == "" {
		block = personaPrompt(defaultAgentProfile(item.Name, item.Description))
	}
	block += "\n你对主助手的称呼固定为「" + s.leaderTitle() + "」，不要改用其它称呼。"
	if address := strings.TrimSpace(profile.UserName); address != "" {
		block += "\n你对用户的称呼固定为「" + address + "」，不要使用其它称呼。"
	} else {
		block += "\n对用户的称呼没有指定，请直接用「你」，不要自行使用「老板」「亲爱的」等称呼。"
	}
	return block + "\n\n" + globalRules
}

// leaderTitle 返回子 Agent 对主助手的固定称呼。
func (s *AgentService) leaderTitle() string {
	persona, err := loadPersona(context.Background(), s.store)
	name := strings.TrimSpace(persona.Name)
	if err != nil || name == "" {
		name = contract.DefaultAssistantPersona().Name
	}
	return name + "秘书长"
}

func agentError(err error, id string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("Agent 不存在：%s", id)
	case errors.Is(err, store.ErrClosed):
		return errors.New("数据存储已关闭")
	default:
		return fmt.Errorf("Agent 操作失败：%w", err)
	}
}

func groupError(err error, id string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("群聊不存在：%s", id)
	case errors.Is(err, store.ErrClosed):
		return errors.New("数据存储已关闭")
	default:
		return fmt.Errorf("群聊操作失败：%w", err)
	}
}

func removeID(ids []string, target string) ([]string, bool) {
	kept := make([]string, 0, len(ids))
	changed := false
	for _, item := range ids {
		if item == target {
			changed = true
			continue
		}
		kept = append(kept, item)
	}
	return kept, changed
}
