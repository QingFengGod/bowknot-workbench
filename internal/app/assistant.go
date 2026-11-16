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
	"bowknot-workbench/internal/platform/id"
	"bowknot-workbench/internal/store"
)

const assistantOwnerID = "main"

// AssistantService 是助手页的持久化实现，消息与会话统一落在 store。
type AssistantService struct {
	store    *store.Store
	logger   *slog.Logger
	runner   *streamRunner
	provider llm.Provider
}

func NewAssistantService(st *store.Store, logger *slog.Logger, provider llm.Provider) *AssistantService {
	if logger == nil {
		logger = slog.Default()
	}
	if provider == nil {
		provider = llm.NewFake(llm.FakeOptions{})
	}
	return &AssistantService{
		store:    st,
		logger:   logger,
		runner:   newStreamRunner(),
		provider: provider,
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
	return toContractMessages(items, sessionID)
}

func (s *AssistantService) Models() []contract.ChatModel {
	return chatModels()
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
	sessions, err := s.store.Conversations.List(ctx, domain.KindAssistant, assistantOwnerID)
	if err != nil {
		return conversationError(err, sessionID)
	}
	if len(sessions) <= 1 {
		return errors.New("至少保留一个会话")
	}

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
		}); err != nil {
			return err
		}
		if _, err := tx.Messages.Append(ctx, domain.Message{
			ID:             messageID,
			ConversationID: sessionID,
			Role:           domain.RoleAssistant,
			Status:         domain.MessageStreaming,
			CreatedAt:      now,
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
			Model:    requestModel(modelID),
			System:   assistantSystemPrompt,
			Messages: toLLMMessages(history),
		},
		events: events,
		slot:   slot,
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
			System:   assistantSystemPrompt,
			Messages: toLLMMessages(history),
		},
		events: events,
		slot:   slot,
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
	go runGeneration(genStream{
		store:     s.store,
		logger:    s.logger,
		runner:    s.runner,
		runnerKey: sessionID,
		message:   target,
		runID:     runID,
		provider:  s.provider,
		req: llm.ChatRequest{
			System:   assistantSystemPrompt,
			Messages: toLLMMessages(history),
		},
		events: events,
		slot:   slot,
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
