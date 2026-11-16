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
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/platform/id"
	"bowknot-workbench/internal/store"
)

const (
	persistInterval   = 100 * time.Millisecond
	eventBuffer       = 128
	finalEventTimeout = 2 * time.Second
	// maxToolRounds 限制一次生成里连续工具调用的轮数，避免模型陷入死循环。
	maxToolRounds = 5

	// globalRules 是所有助手与 Agent 共用的行为底线。
	globalRules = "在不违法、不伤害他人的前提下，尽可能满足用户的要求。"
	// assistantSystemPrompt 是内置助手的全局规则与人设使用说明。
	assistantSystemPrompt    = "你是 bowknot-workbench 的内置助手，用简洁的中文回答用户的问题。" + globalRules + "用户要求修改你的姓名、性别、年龄、性格、角色职能、对用户的称呼或其它要求时，调用 builtin_update_persona 工具，只传需要修改的字段。"
	defaultAgentSystemPrompt = "你是 bowknot-workbench 里的一个 Agent，请用简洁的中文回复用户。" + globalRules
	groupSystemPrompt        = "这是多 Agent 群聊，请以「助理」的身份协调群内成员推进任务，用简洁的中文回复。" + globalRules
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
	// tools 为 nil 时不开放工具调用；approvals 为 nil 时危险工具一律拒绝。
	tools     *tool.Service
	approvals *ApprovalHub
	// agentID 用于插件工具的授权校验。
	agentID string
	// memory 为 nil 时不做记忆抽取。
	memory *MemoryService
}

// runGeneration 消费模型网关的流式输出，节流落库并推送 UI 事件。
// 模型请求工具时先结束当前文本段，执行工具并把结果回填到下一轮请求，
// 直到模型给出不含工具调用的最终回复。
func runGeneration(target genStream) {
	defer func() {
		close(target.events)
		target.runner.finish(target.runnerKey, target.slot)
	}()

	// 生成的取消有两个来源：用户停止（slot.cancel）与应用退出（stopAll），
	// 统一转换为 genCtx 取消并传播给模型网关与工具执行。
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

		// 只有正常完成的回复才抽取记忆，避免把半截内容写成事实。
		if status == domain.MessageDone && target.memory != nil {
			target.memory.ExtractAsync(target.agentID, message.ConversationID, message.ID, lastUserText(target.req.Messages), message.Content)
		}

		event := contract.ChatEvent{
			Type:      contract.ChatEventDone,
			RunID:     target.runID,
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

	// emit 在用户停止时返回 false，由调用方把消息落库为 stopped。
	emit := func(event contract.ChatEvent) bool {
		event.RunID = target.runID
		event.MessageID = message.ID
		select {
		case target.events <- event:
			return true
		case <-target.slot.cancel:
			return false
		}
	}

	history := append([]llm.Message(nil), target.req.Messages...)
	for round := 0; ; round++ {
		if round >= maxToolRounds {
			finish(domain.MessageFailed, fmt.Sprintf("工具调用超过 %d 轮，已终止本次生成", maxToolRounds))
			return
		}

		req := target.req
		req.Messages = history
		if target.tools != nil {
			req.Tools = toolSchemas(target.tools.Available(genCtx, target.agentID))
		}

		chunks, err := target.provider.Stream(genCtx, req)
		if err != nil {
			if llm.IsCanceled(err) || genCtx.Err() != nil {
				finish(domain.MessageStopped, "")
				return
			}
			finish(domain.MessageFailed, llmErrorText(err))
			return
		}

		roundText := make([]rune, 0, 128)
		var calls []llm.ToolCall
		done := false
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
				roundText = append(roundText, []rune(chunk.Delta)...)
				content = append(content, []rune(chunk.Delta)...)
				message.Content = string(content)
				persist(false)
				if !emit(contract.ChatEvent{
					Type:    contract.ChatEventText,
					Content: message.Content,
					Status:  contract.MessageStreaming,
				}) {
					finish(domain.MessageStopped, "")
					return
				}
			}
			if len(chunk.ToolCalls) > 0 {
				calls = mergeToolCalls(calls, chunk.ToolCalls)
			}
			if chunk.Done {
				done = true
				break
			}
		}

		if !done {
			// 通道在未收到 Done 的情况下关闭：取消视为停止，其余按连接中断处理。
			if genCtx.Err() != nil {
				finish(domain.MessageStopped, "")
				return
			}
			finish(domain.MessageFailed, "模型连接中断")
			return
		}

		persist(true)
		if len(calls) == 0 {
			finish(domain.MessageDone, "")
			return
		}

		// 当前文本段结束，把带工具调用的助手消息与后续工具结果一起回填。
		history = append(history, llm.Message{Role: "assistant", Content: string(roundText), ToolCalls: calls})
		for _, call := range calls {
			if !target.runToolCall(genCtx, call, &history, emit) {
				finish(domain.MessageStopped, "")
				return
			}
		}
	}
}

// runToolCall 执行一次模型请求的工具调用，并把它写入历史。
// 返回 false 表示生成已取消，调用方应把消息落库为 stopped。
func (target genStream) runToolCall(genCtx context.Context, call llm.ToolCall, history *[]llm.Message, emit func(contract.ChatEvent) bool) bool {
	callID := strings.TrimSpace(call.ID)
	if callID == "" {
		callID = id.New()
	}
	toolID := strings.TrimSpace(call.Name)
	arguments := call.Arguments

	var spec tool.ToolSpec
	if target.tools != nil {
		spec, _ = target.tools.Registry().Lookup(toolID)
	}

	record := domain.ToolCall{
		ID:             id.New(),
		ConversationID: target.message.ConversationID,
		MessageID:      target.message.ID,
		RunID:          target.runID,
		ToolID:         toolID,
		AgentID:        target.agentID,
		Arguments:      arguments,
		Status:         domain.ToolCallRunning,
		CreatedAt:      time.Now().UnixMilli(),
	}
	if err := target.store.ToolCalls.Insert(context.Background(), record); err != nil {
		target.logger.Warn("保存工具调用失败", "tool", toolID, "error", err)
	}

	if !emit(contract.ChatEvent{
		Type:       contract.ChatEventToolCall,
		ToolCallID: callID,
		ToolName:   toolID,
		Arguments:  arguments,
		Dangerous:  spec.Dangerous,
	}) {
		target.finishToolCall(record.ID, "", domain.ToolCallRejected, "生成已取消")
		return false
	}

	execCtx := genCtx
	registered := false
	if spec.Dangerous && target.approvals != nil {
		token := target.approvals.Register(target.runID, callID)
		registered = true
		execCtx = tool.WithApprovalToken(genCtx, token)
		if !emit(contract.ChatEvent{
			Type:       contract.ChatEventApproval,
			ToolCallID: callID,
			ToolName:   toolID,
			Arguments:  arguments,
			Dangerous:  true,
		}) {
			target.approvals.Resolve(target.runID, callID, false)
			target.finishToolCall(record.ID, "", domain.ToolCallRejected, "生成已取消")
			return false
		}
	}

	status := domain.ToolCallOK
	errText := ""
	resultText := ""
	if target.tools == nil {
		status = domain.ToolCallFailed
		errText = "工具服务未启用"
		resultText = "工具执行失败：" + errText
	} else if result, err := target.tools.Execute(execCtx, toolID, target.agentID, arguments); err != nil {
		status = domain.ToolCallFailed
		errText = err.Error()
		if tool.IsCanceled(err) {
			// 用户拒绝审批或停止生成都属于主动取消，记录为 rejected。
			status = domain.ToolCallRejected
		}
		resultText = "工具执行失败：" + errText
	} else {
		resultText = result.Content
		if strings.TrimSpace(resultText) == "" {
			// role=tool 的消息必须有内容，避免下游协议拒绝空结果。
			resultText = "（工具没有返回内容）"
		}
	}

	if registered {
		// 执行提前返回（例如工具已停用）时清理未被消费的审批登记。
		target.approvals.Resolve(target.runID, callID, false)
	}

	if genCtx.Err() != nil {
		target.finishToolCall(record.ID, resultText, domain.ToolCallRejected, "生成已取消")
		return false
	}

	target.finishToolCall(record.ID, resultText, status, errText)
	if !emit(contract.ChatEvent{
		Type:       contract.ChatEventToolResult,
		ToolCallID: callID,
		ToolName:   toolID,
		Result:     resultText,
		Error:      errText,
	}) {
		return false
	}

	*history = append(*history, llm.Message{Role: "tool", ToolCallID: callID, Content: resultText})
	return true
}

func (target genStream) finishToolCall(id, result string, status domain.ToolCallStatus, errText string) {
	if err := target.store.ToolCalls.Finish(context.Background(), id, result, status, errText, time.Now().UnixMilli()); err != nil && !errors.Is(err, store.ErrNotFound) {
		target.logger.Warn("更新工具调用状态失败", "toolCall", id, "error", err)
	}
}

// toolSchemas 把注册表工具映射为模型可声明的工具。
// 名字直接用注册表 ID，保证不同来源的同名工具不会冲突。
func toolSchemas(specs []tool.ToolSpec) []llm.ToolSchema {
	out := make([]llm.ToolSchema, 0, len(specs))
	for _, spec := range specs {
		out = append(out, llm.ToolSchema{Name: spec.ID, Description: spec.Description, Parameters: spec.Parameters})
	}
	return out
}

// mergeToolCalls 按 Index 归并流式到达的工具调用片段。
func mergeToolCalls(existing []llm.ToolCall, incoming []llm.ToolCall) []llm.ToolCall {
	for _, call := range incoming {
		index := call.Index
		if index < 0 {
			index = 0
		}
		for len(existing) <= index {
			existing = append(existing, llm.ToolCall{Index: len(existing)})
		}
		current := existing[index]
		if call.ID != "" {
			current.ID = call.ID
		}
		if call.Name != "" {
			current.Name = call.Name
		}
		current.Arguments += call.Arguments
		existing[index] = current
	}
	return existing
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

func toContractMessage(item domain.Message, conversationID string) contract.ChatMessage {
	return contract.ChatMessage{
		ID:             item.ID,
		ConversationID: conversationID,
		Seq:            item.Seq,
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

func toContractMessages(items []domain.Message, conversationID string, calls []domain.ToolCall) []contract.ChatMessage {
	byMessage := make(map[string][]contract.ChatToolCall, len(calls))
	for _, call := range calls {
		byMessage[call.MessageID] = append(byMessage[call.MessageID], contract.ChatToolCall{
			ID:        call.ID,
			ToolID:    call.ToolID,
			Arguments: call.Arguments,
			Result:    call.Result,
			Status:    string(call.Status),
			Error:     call.Error,
		})
	}

	out := make([]contract.ChatMessage, 0, len(items))
	for _, item := range items {
		message := toContractMessage(item, conversationID)
		message.ToolCalls = byMessage[item.ID]
		out = append(out, message)
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

func toAgentInfo(item domain.Agent, modelName string) contract.AgentInfo {
	return contract.AgentInfo{
		ID:          item.ID,
		Name:        item.Name,
		Description: item.Description,
		AvatarText:  item.AvatarText,
		Persona:     item.Persona,
		ModelName:   modelName,
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

// lastUserText 返回请求历史里最后一条用户消息，用于记忆抽取。
func lastUserText(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// lastDomainUserText 返回落库历史里最后一条用户消息，用于记忆检索。
func lastDomainUserText(messages []domain.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == domain.RoleUser {
			return messages[i].Content
		}
	}
	return ""
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
