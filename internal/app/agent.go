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
	artifacts *ArtifactService
	groups    *GroupService
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

// SetArtifacts 注入产物服务，用于消息读取时附加产物摘要。
func (s *AgentService) SetArtifacts(artifacts *ArtifactService) {
	s.artifacts = artifacts
}

// SetGroups 注入群聊编排服务，由 main 在 GroupService 创建后调用。
func (s *AgentService) SetGroups(groups *GroupService) {
	s.groups = groups
}

// GroupRevision 返回群聊协作版本号，UI 据此刷新成员发言。
func (s *AgentService) GroupRevision() uint64 {
	if s.groups == nil {
		return 0
	}
	return s.groups.Revision()
}

// GroupBusy 判断指定群聊是否仍在进行协作轮次。
func (s *AgentService) GroupBusy(conversationID string) bool {
	if s.groups == nil {
		return false
	}
	return s.groups.Busy(conversationID)
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
	storeID, _, isGroup := resolveConversationID(conversationID)
	items, err := s.store.Messages.List(context.Background(), storeID)
	if err != nil {
		s.logger.Error("读取 Agent 消息失败", "conversation", conversationID, "error", err)
		return nil
	}
	calls, err := s.store.ToolCalls.ListByConversation(context.Background(), storeID)
	if err != nil {
		s.logger.Error("读取 Agent 工具调用失败", "conversation", conversationID, "error", err)
	}
	out := toContractMessages(items, conversationID, calls)
	if isGroup {
		stripGroupSpeakerPrefixes(out)
	}
	attachTaskBriefs(s.store, out)
	attachArtifacts(s.store, s.artifacts, out)
	return out
}

// stripGroupSpeakerPrefixes 清理群聊历史里已经落库的发言人前缀，避免界面上重复显示。
func stripGroupSpeakerPrefixes(items []contract.ChatMessage) {
	for i := range items {
		if items[i].Role != contract.RoleAssistant {
			continue
		}
		items[i].Content = stripSpeakerPrefix(items[i].Content, items[i].SenderName)
	}
}

// MessagesPage 分页读取 Agent 消息，beforeSeq <= 0 表示从最新开始。
func (s *AgentService) MessagesPage(conversationID string, beforeSeq int64, limit int) contract.MessagePage {
	storeID, _, isGroup := resolveConversationID(conversationID)
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
	out := toContractMessages(items, conversationID, calls)
	if isGroup {
		stripGroupSpeakerPrefixes(out)
	}
	attachTaskBriefs(s.store, out)
	attachArtifacts(s.store, s.artifacts, out)
	return contract.MessagePage{Items: out, HasMore: hasMore}
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

// TurnInput 描述一次员工生成。Text 为空时只启动生成，
// 由调用方（任务线程）提前写入派发消息。
type TurnInput struct {
	ConversationID string
	Text           string
	// 以下元信息写入本轮助手消息，用于任务卡片与消息分栏。
	SenderKind  string
	SenderID    string
	MessageType string
	TaskID      string
	Visibility  string
	// SystemExtra 追加到系统提示词末尾，用于注入任务上下文。
	SystemExtra string
	// GroupAgentID 非空时以该员工身份在群聊中发言，不再走主助手群主提示词。
	GroupAgentID string
	// GroupSummary 表示群主在成员讨论后生成最终答复。
	GroupSummary bool
	// Finished 非空时在本轮生成完全结束时关闭，供群聊编排等待主回复完成。
	Finished chan struct{}
}

func (s *AgentService) Send(conversationID, text, _ string) (string, <-chan contract.ChatEvent, error) {
	if _, _, isGroup := resolveConversationID(conversationID); isGroup && s.groups != nil {
		return s.groups.Start(conversationID, text)
	}
	return s.StartTurn(TurnInput{
		ConversationID: conversationID,
		Text:           text,
		SenderKind:     domain.SenderAgent,
		MessageType:    domain.MessageTypeChat,
		Visibility:     domain.VisibilityUser,
	})
}

// AppendGroupUserMessage 只写入群聊用户消息，不生成群主回复。
// 用于用户直接 @ 成员、由成员直接回答的场景。
func (s *AgentService) AppendGroupUserMessage(conversationID, text string) error {
	storeID, groupID, isGroup := resolveConversationID(conversationID)
	if !isGroup {
		return errors.New("群聊会话 ID 无效")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("请输入消息内容")
	}
	group, err := s.store.Groups.Get(context.Background(), groupID)
	if err != nil {
		return groupError(err, groupID)
	}

	ctx := context.Background()
	now := time.Now().UnixMilli()
	return s.store.Tx(ctx, func(tx *store.Store) error {
		item, err := tx.Conversations.Get(ctx, storeID)
		if errors.Is(err, store.ErrNotFound) {
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

		messages, err := tx.Messages.List(ctx, storeID)
		if err != nil {
			return err
		}
		title := item.Title
		if title == "" || title == "新会话" || len(messages) == 0 {
			title = group.Name
		}
		if _, err := tx.Messages.Append(ctx, domain.Message{
			ID:             id.New(),
			ConversationID: storeID,
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
		return tx.Conversations.UpdateMeta(ctx, storeID, title, snippet(text, 60), item.Pinned, now)
	})
}

// StartTurn 追加用户消息（可选）并启动一次生成，供直聊、任务线程与群聊共用。
func (s *AgentService) StartTurn(in TurnInput) (string, <-chan contract.ChatEvent, error) {
	text := strings.TrimSpace(in.Text)
	memberID := strings.TrimSpace(in.GroupAgentID)
	if text == "" && strings.TrimSpace(in.TaskID) == "" && memberID == "" && !in.GroupSummary {
		return "", nil, errors.New("请输入消息内容")
	}

	conversationID := in.ConversationID
	storeID, groupID, isGroup := resolveConversationID(conversationID)
	if (memberID != "" || in.GroupSummary) && !isGroup {
		return "", nil, errors.New("群聊协作只能在群聊会话中使用")
	}
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
	var member domain.Agent
	senderName, senderAvatar := "", ""
	if memberID != "" {
		item, err := s.store.Agents.Get(ctx, memberID)
		if err != nil {
			s.runner.finish(conversationID, slot)
			return "", nil, agentError(err, memberID)
		}
		member = item
		senderName, senderAvatar = item.Name, firstRune(item.Name)
	} else if isGroup {
		name := s.mainDisplayName()
		senderName, senderAvatar = name, firstRune(name)
	}
	meta := domain.Message{
		SenderKind:  in.SenderKind,
		SenderID:    in.SenderID,
		MessageType: in.MessageType,
		TaskID:      in.TaskID,
		Visibility:  in.Visibility,
	}
	if meta.SenderKind == "" {
		meta.SenderKind = domain.SenderAgent
	}
	if meta.MessageType == "" {
		meta.MessageType = domain.MessageTypeChat
	}
	if meta.Visibility == "" {
		meta.Visibility = domain.VisibilityUser
	}
	if memberID != "" {
		meta.SenderKind = domain.SenderAgent
		meta.SenderID = memberID
	} else if isGroup {
		meta.SenderKind = domain.SenderMain
		meta.SenderID = mainAgentID
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
		// 任务线程的标题由派发方设置，生成时不覆盖。
		if text != "" && (title == "新会话" || title == "" || len(messages) == 0) {
			if isGroup {
				title = group.Name
			} else {
				title = snippet(text, 20)
			}
		}

		summary := item.Summary
		if text != "" {
			summary = snippet(text, 60)
		}
		if text != "" {
			if _, err := tx.Messages.Append(ctx, domain.Message{
				ID:             userID,
				ConversationID: storeID,
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
		}
		if _, err := tx.Messages.Append(ctx, domain.Message{
			ID:             messageID,
			ConversationID: storeID,
			Role:           domain.RoleAssistant,
			Status:         domain.MessageStreaming,
			CreatedAt:      now,
			SenderName:     senderName,
			SenderAvatar:   senderAvatar,
			SenderKind:     meta.SenderKind,
			SenderID:       meta.SenderID,
			MessageType:    meta.MessageType,
			TaskID:         meta.TaskID,
			Visibility:     meta.Visibility,
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
		return tx.Conversations.UpdateMeta(ctx, storeID, title, summary, item.Pinned, now)
	}); err != nil {
		s.runner.finish(conversationID, slot)
		return "", nil, conversationError(err, conversationID)
	}

	req := llm.ChatRequest{}
	agentID := mainAgentID
	switch {
	case memberID != "":
		agentID = memberID
		req = llm.ChatRequest{
			System: s.agentSystemPrompt(member) + "\n\n" + s.groupMemberGuide(group, member),
			Model:  member.ModelID,
		}
	case isGroup:
		req = llm.ChatRequest{System: s.groupMainPrompt(group)}
	default:
		agentID = conversation.OwnerID
		item, err := s.store.Agents.Get(ctx, conversation.OwnerID)
		if err != nil {
			s.logger.Warn("读取 Agent 人设失败，使用默认人设", "agent", conversation.OwnerID, "error", err)
			req = llm.ChatRequest{System: defaultAgentSystemPrompt}
		} else {
			req = llm.ChatRequest{System: s.agentSystemPrompt(item), Model: item.ModelID}
		}
	}
	if extra := strings.TrimSpace(in.SystemExtra); extra != "" {
		req.System = strings.TrimSpace(req.System + "\n\n" + extra)
	}
	history, err := s.store.Messages.List(ctx, storeID)
	if err != nil {
		s.runner.finish(conversationID, slot)
		return "", nil, conversationError(err, conversationID)
	}
	if isGroup {
		req.Messages = toGroupLLMMessages(history)
	} else {
		req.Messages = toLLMMessages(history)
	}
	if s.memory != nil {
		query := text
		if query == "" {
			query = conversation.Title
		}
		if block := s.memory.PromptBlock(agentID, query); block != "" {
			req.System = strings.TrimSpace(req.System + "\n\n" + block)
		}
	}
	// 直聊时把员工自己的历史任务摘要作为只读上下文注入；
	// 任务线程本身已有任务说明，不再重复注入。
	if !isGroup && strings.TrimSpace(in.TaskID) == "" && text != "" {
		if block := s.taskContextBlock(agentID); block != "" {
			req.System = strings.TrimSpace(req.System + "\n\n" + block)
		}
	}

	events := make(chan contract.ChatEvent, eventBuffer)
	memoryWritten := false
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
			SenderKind:     meta.SenderKind,
			SenderID:       meta.SenderID,
			MessageType:    meta.MessageType,
			TaskID:         meta.TaskID,
			Visibility:     meta.Visibility,
		},
		runID:         runID,
		provider:      s.provider,
		req:           req,
		events:        events,
		slot:          slot,
		tools:         s.tools,
		approvals:     s.approvals,
		agentID:       agentID,
		memoryWritten: &memoryWritten,
		onFinish: func() {
			if in.Finished != nil {
				close(in.Finished)
			}
		},
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
	memoryWritten := false
	go runGeneration(genStream{
		store:         s.store,
		logger:        s.logger,
		runner:        s.runner,
		runnerKey:     conversationID,
		message:       target,
		runID:         runID,
		provider:      s.provider,
		req:           req,
		events:        events,
		slot:          slot,
		tools:         s.tools,
		approvals:     s.approvals,
		agentID:       agentID,
		memoryWritten: &memoryWritten,
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
	if s.groups != nil {
		s.groups.Cancel(conversationID)
	}
}

// agentSystemPrompt 返回 Agent 独立的结构化人设。
// 历史自由文本会退回到「其它要求」；其它字段为空时用名称与职责兜底。
// 子 Agent 对主助手的称呼固定为「主助手姓名 + 秘书长」。
// maxContextTasks 限制直聊注入的历史任务条数。
const maxContextTasks = 8

// taskContextBlock 把员工自己的历史任务摘要注入直聊提示词。
// 记录只来自该员工的任务表，不涉及用户全局记忆或其它会话内容。
func (s *AgentService) taskContextBlock(agentID string) string {
	items, err := s.store.Tasks.List(context.Background(), store.TaskFilter{
		AssigneeID: agentID,
		Limit:      maxContextTasks,
	})
	if err != nil || len(items) == 0 {
		return ""
	}

	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := "- " + taskStatusLabel(item.Status) + "｜" + item.Title
		switch {
		case strings.TrimSpace(item.ResultSummary) != "":
			line += "｜" + snippet(item.ResultSummary, 80)
		case strings.TrimSpace(item.Error) != "":
			line += "｜" + snippet(item.Error, 60)
		}
		lines = append(lines, line)
	}

	return "【你的历史任务】以下是你自己执行过的任务记录，仅作只读参考：\n" + strings.Join(lines, "\n") +
		"\n这些记录来自你自己的任务线程，不包含用户的其它会话内容；不要主动复述，也不要把它们当成本轮任务。"
}

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
	return block + "\n\n" + globalRules + memoryToolGuide + memoryCheckGuide
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

// mainDisplayName 返回群聊里展示的主助手姓名。
func (s *AgentService) mainDisplayName() string {
	persona, err := loadPersona(context.Background(), s.store)
	name := strings.TrimSpace(persona.Name)
	if err != nil || name == "" {
		return "助理"
	}
	return name
}

// groupMembers 返回群内仍然存在的成员。
func (s *AgentService) groupMembers(group domain.Group) []domain.Agent {
	out := make([]domain.Agent, 0, len(group.MemberIDs))
	for _, id := range group.MemberIDs {
		item, err := s.store.Agents.Get(context.Background(), id)
		if err != nil {
			s.logger.Warn("读取群成员失败", "group", group.ID, "agent", id, "error", err)
			continue
		}
		out = append(out, item)
	}
	return out
}

// groupMainPrompt 是群主提示词：成员清单 + @ 规则 + 轮次上限。
func (s *AgentService) groupMainPrompt(group domain.Group) string {
	members := s.groupMembers(group)
	lines := make([]string, 0, len(members))
	for _, item := range members {
		desc := strings.TrimSpace(item.Description)
		if desc == "" {
			desc = "未填写简介"
		}
		lines = append(lines, "- "+item.Name+"："+desc)
	}
	list := "（暂无成员）"
	if len(lines) > 0 {
		list = strings.Join(lines, "\n")
	}

	persona, _ := loadPersona(context.Background(), s.store)
	if strings.TrimSpace(persona.Name) == "" {
		persona.Name = s.mainDisplayName()
	}
	block := personaPrompt(persona)
	if block == "" {
		block = "你是用户的私人助手。"
	}

	return block + "\n\n" + globalRules + memoryToolGuide + memoryCheckGuide + "\n\n" +
		"你是群聊「" + group.Name + "」的群主「" + s.mainDisplayName() + "」。\n" +
		"群成员：\n" + list + "\n\n" +
		"协作规则：\n" +
		"1. 用户发言后，先判断是否需要成员参与；需要时在回复中用 @成员名 点名，可以一次点多个。\n" +
		"2. 被点名的成员会在群里依次发言；单条用户消息最多进行 3 轮成员讨论，不要重复点名已经发言的成员，也不要点名群主。\n" +
		"3. 成员讨论结束后，系统会要求你面向用户给出最终答复；如果不需要成员参与，直接回答用户。\n" +
		"4. 所有协作只发生在当前群聊里，不要私聊成员，也不要虚构成员的发言。\n" +
		"5. 用户直接 @ 成员时不要抢答，也不要用成员的口吻替他们发言；只用一句话确认，把发言权交给成员。\n" +
		"6. 成员已经直接回答用户时，不要复述或总结成员的内容。"
}

// groupMemberGuide 是成员在群内发言时的补充规则。
func (s *AgentService) groupMemberGuide(group domain.Group, member domain.Agent) string {
	return "【群聊协作】你是群成员「" + member.Name + "」，当前在群聊「" + group.Name + "」中发言。\n" +
		"群主是「" + s.mainDisplayName() + "」。你只在本群发言，不要私聊其他成员。\n" +
		"如确实需要其他成员补充，可在回复中用 @成员名 点名；最多 3 轮，不要点名自己、群主或已经发言的成员。\n" +
		"直接回应用户与群主的讨论，保持简短，不要重复别人的话。"
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
