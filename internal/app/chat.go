package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/store"
)

const (
	persistInterval   = 100 * time.Millisecond
	eventBuffer       = 128
	finalEventTimeout = 2 * time.Second

	assistantSystemPrompt    = "你是 bowknot-workbench 的内置助手，用简洁的中文回答用户的问题。"
	defaultAgentSystemPrompt = "你是 bowknot-workbench 里的一个 Agent，请用简洁的中文回复用户。"
	groupSystemPrompt        = "这是多 Agent 群聊，请以「助理」的身份协调群内成员推进任务，用简洁的中文回复。"
)

// streamRunner 管理每个会话唯一的生成任务，保证同一会话不会并发写入。
type streamRunner struct {
	mu     sync.Mutex
	active map[string]*activeStream
	wg     sync.WaitGroup
}

type activeStream struct {
	messageID string
	cancel    chan struct{}
	done      chan struct{}
	shutdown  atomic.Bool
	cancelOne sync.Once
	doneOne   sync.Once
}

func newStreamRunner() *streamRunner {
	return &streamRunner{active: map[string]*activeStream{}}
}

func (s *activeStream) stop() {
	s.cancelOne.Do(func() { close(s.cancel) })
}

func (s *activeStream) markDone() {
	s.doneOne.Do(func() { close(s.done) })
}

func (r *streamRunner) begin(conversationID, messageID string) (*activeStream, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.active[conversationID]; ok {
		return nil, false
	}
	slot := &activeStream{
		messageID: messageID,
		cancel:    make(chan struct{}),
		done:      make(chan struct{}),
	}
	r.active[conversationID] = slot
	r.wg.Add(1)
	return slot, true
}

func (r *streamRunner) finish(conversationID string, slot *activeStream) {
	r.mu.Lock()
	owned := r.active[conversationID] == slot
	if owned {
		delete(r.active, conversationID)
	}
	r.mu.Unlock()
	if owned {
		slot.markDone()
		r.wg.Done()
	}
}

func (r *streamRunner) stop(conversationID string) {
	r.mu.Lock()
	slot := r.active[conversationID]
	r.mu.Unlock()
	if slot != nil {
		slot.stop()
	}
}

func (r *streamRunner) stopAndWait(ctx context.Context, conversationID string) {
	r.mu.Lock()
	slot := r.active[conversationID]
	r.mu.Unlock()
	if slot == nil {
		return
	}
	slot.stop()
	select {
	case <-slot.done:
	case <-ctx.Done():
	}
}

func (r *streamRunner) streamingMessage(conversationID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if slot := r.active[conversationID]; slot != nil {
		return slot.messageID
	}
	return ""
}

func (r *streamRunner) stopAll(ctx context.Context) {
	r.mu.Lock()
	slots := make([]*activeStream, 0, len(r.active))
	for _, slot := range r.active {
		slots = append(slots, slot)
	}
	r.mu.Unlock()

	for _, slot := range slots {
		slot.shutdown.Store(true)
		slot.stop()
	}

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// genStream 携带一次模型生成的落库与推送上下文。
type genStream struct {
	store     *store.Store
	logger    *slog.Logger
	runner    *streamRunner
	runnerKey string
	message   domain.Message
	runID     string
	provider  llm.Provider
	req       llm.ChatRequest
	events    chan contract.ChatEvent
	slot      *activeStream
}

// runGeneration 消费模型网关的流式输出，节流落库并推送 UI 事件。
func runGeneration(target genStream) {
	defer func() {
		close(target.events)
		target.runner.finish(target.runnerKey, target.slot)
	}()

	// 生成的取消有两个来源：用户停止（slot.cancel）与应用退出（stopAll），
	// 统一转换为 genCtx 取消并传播给模型网关。
	genCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-target.slot.cancel:
			cancel()
		case <-genCtx.Done():
		}
	}()

	content := make([]rune, 0, 256)
	message := target.message
	message.Status = domain.MessageStreaming
	lastPersist := time.Time{}

	persist := func(force bool) {
		now := time.Now()
		if !force && now.Sub(lastPersist) < persistInterval {
			return
		}
		lastPersist = now
		message.Content = string(content)
		if err := target.store.Messages.Update(context.Background(), message); err != nil && !errors.Is(err, store.ErrNotFound) {
			target.logger.Error("保存流式消息失败", "message", message.ID, "error", err)
		}
	}

	finish := func(status domain.MessageStatus, errText string) {
		message.Content = string(content)
		message.Status = status
		message.Error = errText
		if err := target.store.Messages.Update(context.Background(), message); err != nil && !errors.Is(err, store.ErrNotFound) {
			target.logger.Error("保存消息最终状态失败", "message", message.ID, "error", err)
		}
		runStatus := domain.RunDone
		switch status {
		case domain.MessageStopped:
			runStatus = domain.RunStopped
		case domain.MessageFailed:
			runStatus = domain.RunFailed
		}
		if err := target.store.Runs.Finish(context.Background(), target.runID, runStatus, time.Now().UnixMilli()); err != nil && !errors.Is(err, store.ErrNotFound) {
			target.logger.Error("更新生成任务状态失败", "run", target.runID, "error", err)
		}

		event := contract.ChatEvent{
			MessageID: message.ID,
			Content:   message.Content,
			Status:    contract.MessageStatus(status),
			Error:     errText,
			Done:      true,
		}
		if target.slot.shutdown.Load() {
			select {
			case target.events <- event:
			default:
			}
			return
		}
		select {
		case target.events <- event:
		case <-time.After(finalEventTimeout):
		}
	}

	chunks, err := target.provider.Stream(genCtx, target.req)
	if err != nil {
		if llm.IsCanceled(err) || genCtx.Err() != nil {
			finish(domain.MessageStopped, "")
			return
		}
		finish(domain.MessageFailed, llmErrorText(err))
		return
	}

	for chunk := range chunks {
		if chunk.Err != nil {
			if llm.IsCanceled(chunk.Err) || genCtx.Err() != nil {
				finish(domain.MessageStopped, "")
			} else {
				finish(domain.MessageFailed, llmErrorText(chunk.Err))
			}
			return
		}
		if chunk.Delta != "" {
			content = append(content, []rune(chunk.Delta)...)
			message.Content = string(content)
			persist(false)
			select {
			case target.events <- contract.ChatEvent{
				MessageID: message.ID,
				Content:   message.Content,
				Status:    contract.MessageStreaming,
			}:
			case <-target.slot.cancel:
				finish(domain.MessageStopped, "")
				return
			}
		}
		if chunk.Done {
			finish(domain.MessageDone, "")
			return
		}
	}

	// 通道在未收到 Done 的情况下关闭：取消视为停止，其余按连接中断处理。
	if genCtx.Err() != nil {
		finish(domain.MessageStopped, "")
		return
	}
	finish(domain.MessageFailed, "模型连接中断")
}

// llmErrorText 把网关错误映射为可展示的中文文案；错误信息不包含密钥。
func llmErrorText(err error) string {
	var e *llm.Error
	if errors.As(err, &e) {
		switch e.Kind {
		case llm.KindAuth:
			return "模型密钥无效或未授权，请检查 config.json 中的 model.apiKey"
		case llm.KindRateLimit:
			return "模型请求触发限流，请稍后重试"
		case llm.KindTimeout:
			return "模型请求超时，请稍后重试"
		case llm.KindNetwork:
			return "无法连接模型服务，请检查网络与 model.baseUrl 配置"
		}
		if e.Message != "" {
			return "模型服务返回错误：" + e.Message
		}
	}
	return "生成回复失败：" + err.Error()
}

func chatModels() []contract.ChatModel {
	return []contract.ChatModel{
		{ID: "bk-flash", Name: "Bowknot Flash", Description: "日常对话，响应最快"},
		{ID: "bk-pro", Name: "Bowknot Pro", Description: "复杂任务与长文本"},
		{ID: "bk-reason", Name: "Bowknot Reason", Description: "推理与代码分析"},
	}
}

// requestModel 把 UI 选择的模型 ID 转换为网关请求的模型名。
// 内置演示模型尚未与真实供应商打通，选择演示模型时留空，
// 由网关回退到 config.json 配置的 model.defaultModel。
func requestModel(modelID string) string {
	for _, item := range chatModels() {
		if item.ID == modelID {
			return ""
		}
	}
	return modelID
}

func modelName(id string) string {
	for _, item := range chatModels() {
		if item.ID == id {
			return item.Name
		}
	}
	return "默认模型"
}

func toContractMessage(item domain.Message, conversationID string) contract.ChatMessage {
	return contract.ChatMessage{
		ID:             item.ID,
		ConversationID: conversationID,
		Role:           contract.ChatRole(item.Role),
		Content:        item.Content,
		Status:         contract.MessageStatus(item.Status),
		CreatedAt:      item.CreatedAt,
		Error:          item.Error,
		MemoryWritten:  item.MemoryWritten,
		Emotion:        item.Emotion,
		SenderName:     item.SenderName,
		SenderAvatar:   item.SenderAvatar,
	}
}

func toContractMessages(items []domain.Message, conversationID string) []contract.ChatMessage {
	out := make([]contract.ChatMessage, 0, len(items))
	for _, item := range items {
		out = append(out, toContractMessage(item, conversationID))
	}
	return out
}

func toChatSession(item domain.Conversation) contract.ChatSession {
	return contract.ChatSession{
		ID:        item.ID,
		Title:     item.Title,
		Summary:   item.Summary,
		Pinned:    item.Pinned,
		UpdatedAt: item.UpdatedAt,
	}
}

func toAgentSession(item domain.Conversation) contract.AgentSession {
	return contract.AgentSession{
		ID:        item.ID,
		AgentID:   item.OwnerID,
		Title:     item.Title,
		Summary:   item.Summary,
		Pinned:    item.Pinned,
		UpdatedAt: item.UpdatedAt,
	}
}

func toAgentInfo(item domain.Agent) contract.AgentInfo {
	return contract.AgentInfo{
		ID:          item.ID,
		Name:        item.Name,
		Description: item.Description,
		AvatarText:  item.AvatarText,
		Persona:     item.Persona,
		ModelName:   modelName(item.ModelID),
		Pinned:      item.Pinned,
	}
}

func toAgentGroup(item domain.Group) contract.AgentGroup {
	return contract.AgentGroup{
		ID:        item.ID,
		Name:      item.Name,
		MemberIDs: append([]string(nil), item.MemberIDs...),
		Pinned:    item.Pinned,
	}
}

func groupConversationID(groupID string) string {
	return "group-" + groupID
}

func groupKey(groupID string) string {
	return "group:" + groupID
}

func resolveConversationID(input string) (storeID, groupID string, isGroup bool) {
	if strings.HasPrefix(input, "group:") {
		groupID = strings.TrimPrefix(input, "group:")
		return groupConversationID(groupID), groupID, true
	}
	return input, "", false
}

// toLLMMessages 把落库消息转换为网关请求；跳过空内容（例如流式占位消息）。
func toLLMMessages(items []domain.Message) []llm.Message {
	out := make([]llm.Message, 0, len(items))
	for _, item := range items {
		if item.Content == "" {
			continue
		}
		out = append(out, llm.Message{Role: string(item.Role), Content: item.Content})
	}
	return out
}

func snippet(text string, limit int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
}

func firstRune(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return "?"
	}
	return string(runes[0])
}

func conversationError(err error, id string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("会话不存在：%s", id)
	case errors.Is(err, store.ErrClosed):
		return errors.New("数据存储已关闭")
	default:
		return fmt.Errorf("会话操作失败：%w", err)
	}
}
