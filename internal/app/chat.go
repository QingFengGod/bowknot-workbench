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
	// memoryToolGuide 说明长期记忆工具的存在与基本用法，主助手与子 Agent 共用。
	memoryToolGuide = "你可以使用长期记忆工具：builtin_remember 写入值得长期保留的信息，builtin_search_memory 检索过往记忆，builtin_update_memory 与 builtin_forget_memory 修改或删除记忆。" +
		"用户提到以前的事、要求回忆细节，或你需要过往信息才能回答时，先调用 builtin_search_memory，不要凭空猜测。" +
		"用户要求修改或忘记某条记忆时，先用检索工具拿到记忆 ID，再调用修改或删除工具，不要编造 ID。" +
		"不要记录密码、密钥、身份证、银行卡等敏感信息。"
	// memoryCheckGuide 是每轮回复前的记忆检查清单，固定放在系统提示词末尾，
	// 避免被前文的人设与其它工具说明稀释。
	memoryCheckGuide = "【回复前的记忆检查】每次回复用户前，先快速判断：用户这轮是否透露了关于自己的新信息？" +
		"包括身份背景（姓名、职业、所在城市、使用的工具或项目）、偏好与习惯（喜欢或不喜欢什么、常用表达、工作方式）、" +
		"长期目标与计划、重要决定、值得记住的经历，以及对你的长期要求（称呼、语气、回复格式）。" +
		"只要命中任意一类，必须先调用 builtin_remember 写入一条记忆，再回复用户；一轮最多写两条，只写长期有用的内容。" +
		"不要写入：普通问答、一次性任务、临时情绪、寒暄、转述他人的信息。\n" +
		"示例：「我以后都用简体中文回复」→ 调用；「我在杭州做后端，最近在重构支付系统」→ 调用（可拆成两条）；「今天天气怎么样」→ 不调用。\n" +
		"如果系统提示词里没有【长期记忆】块，说明目前还没有任何记忆，这是正常的，按上面的规则正常写入即可。" +
		"调用记忆工具时不要向用户宣告，也不要因此中断正常回复。"
	// fileToolGuide 说明工作区文件工具，避免模型把文件写到工作区外，
	// 或只把长内容贴在回复里而不落盘。
	fileToolGuide = "需要生成或修改文件时，直接使用内置文件工具：builtin_write_file 写入、builtin_edit_file 精确替换、builtin_list_dir 查看目录、builtin_make_dir 建目录、builtin_glob 查找文件、builtin_search_files 搜索内容。" +
		"所有路径都相对于工作区，例如 docs/报告.md；不要使用绝对路径或 ..，也不要尝试访问工作区以外的文件。" +
		"生成报告、代码、文档等交付物时优先写入文件，写完后在回复里用工具返回的 [文件名](artifact://ID) 链接引用，不要重复整段内容。"
	// assistantSystemPrompt 是内置助手的全局规则与人设使用说明。
	// 明确身份完全由用户设定，避免模型把默认姓名当成不可更改的产品身份而拒绝改名。
	assistantSystemPrompt = "你是一个由用户完全自定义的私人助手，当前运行在 bowknot-workbench 中，用简洁的中文回答用户的问题。" +
		globalRules +
		memoryToolGuide +
		fileToolGuide +
		"你的姓名、性别、年龄、性格、角色或职责、对用户的称呼、其它要求都完全由用户设定，不存在任何不可更改的公司、产品或预设身份。" +
		"用户要求修改这些信息（包括改名、换称呼、设定角色）时，这是完全允许的，必须立即调用 builtin_update_persona 工具，只传需要修改的字段，不要说不能修改，也不要用预设身份作为拒绝理由。" +
		"修改成功后，按新的姓名与称呼回应用户。"
	defaultAgentSystemPrompt = "你是 bowknot-workbench 里的一个 Agent，请用简洁的中文回复用户。" + globalRules + memoryToolGuide + fileToolGuide
	groupSystemPrompt        = "这是多 Agent 群聊，请以「助理」的身份协调群内成员推进任务，用简洁的中文回复。" + globalRules + memoryToolGuide + fileToolGuide
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
	// memoryWritten 指向本次生成的记忆写入标记，由记忆工具执行成功后置位；
	// 只在消息上显示「已记住」，不把记忆工具调用暴露给 UI。
	memoryWritten *bool
	// onFinish 在本轮生成完全结束时调用，供群聊编排等待主回复完成。
	onFinish func()
	// maxToolRounds 是本次生成的工具轮次上限；0 表示使用默认值。
	maxToolRounds int
}

// runGeneration 消费模型网关的流式输出，节流落库并推送 UI 事件。
// 模型请求工具时先结束当前文本段，执行工具并把结果回填到下一轮请求，
// 直到模型给出不含工具调用的最终回复。
func runGeneration(target genStream) {
	defer func() {
		// 先释放会话槽位再关闭事件流，避免消费方在通道关闭后
		// 立刻启动下一轮生成时仍被判定为「正在生成」。
		target.runner.finish(target.runnerKey, target.slot)
		close(target.events)
		if target.onFinish != nil {
			target.onFinish()
		}
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
	// 思维链与正文分开累积，一并落库，重启后仍可回看。
	var reasoning strings.Builder
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
		message.Reasoning = reasoning.String()
		if err := target.store.Messages.Update(context.Background(), message); err != nil && !errors.Is(err, store.ErrNotFound) {
			target.logger.Error("保存流式消息失败", "message", message.ID, "error", err)
		}
	}

	finish := func(status domain.MessageStatus, errText string) {
		// 群聊历史会带「【发言人】」前缀，模型偶尔会模仿到回复开头；
		// 落库与最终事件统一剥离，避免前缀在消息里累积。
		message.Content = stripSpeakerPrefix(string(content), message.SenderName)
		message.Reasoning = reasoning.String()
		message.Status = status
		message.Error = errText
		if target.memoryWritten != nil && *target.memoryWritten {
			message.MemoryWritten = true
		}
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

	// 工具轮次上限在每次生成开始时读取，调整设置后下一轮立即生效。
	limit := target.maxToolRounds
	if limit <= 0 {
		limit = maxToolRounds
	}

	history := append([]llm.Message(nil), target.req.Messages...)
	for round := 0; ; round++ {
		if round >= limit {
			finish(domain.MessageFailed, fmt.Sprintf("工具调用超过 %d 轮，已终止本次生成", limit))
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
			if chunk.Reasoning != "" {
				reasoning.WriteString(chunk.Reasoning)
				if !emit(contract.ChatEvent{
					Type:      contract.ChatEventReasoning,
					Content:   message.Content,
					Reasoning: reasoning.String(),
					Status:    contract.MessageStreaming,
				}) {
					finish(domain.MessageStopped, "")
					return
				}
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

	// 模型侧拿到的可能是去掉冒号与斜杠的安全函数名，这里先还原成内部工具 ID。
	var spec tool.ToolSpec
	if target.tools != nil {
		if resolved, ok := target.tools.Resolve(toolID); ok {
			spec = resolved
			toolID = resolved.ID
		}
	}
	// 记忆工具属于内部动作，只落库审计，不向 UI 推送工具卡片。
	hidden := tool.IsMemoryTool(toolID)

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

	if !hidden {
		if !emit(contract.ChatEvent{
			Type:       contract.ChatEventToolCall,
			ToolCallID: callID,
			ToolName:   toolID,
			Arguments:  arguments,
			ToolStatus: string(domain.ToolCallRunning),
			Dangerous:  spec.Dangerous,
		}) {
			target.finishToolCall(record.ID, "", domain.ToolCallRejected, "生成已取消")
			return false
		}
	}

	// 执行上下文带上当前消息 ID：记忆工具据此溯源，任务工具据此记录派发来源。
	execCtx := tool.WithMessageID(genCtx, target.message.ID)
	registered := false
	if spec.Dangerous && target.approvals != nil {
		token := target.approvals.Register(target.runID, callID)
		registered = true
		// 审批令牌要叠加在带消息 ID 的上下文上，否则登记文件时溯源不到消息。
		execCtx = tool.WithApprovalToken(execCtx, token)
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
	} else if result, err := target.tools.Execute(execCtx, toolID, target.agentID, target.message.ConversationID, arguments); err != nil {
		status = domain.ToolCallFailed
		errText = err.Error()
		if tool.IsCanceled(err) {
			// 用户拒绝审批或停止生成都属于主动取消，记录为 rejected。
			status = domain.ToolCallRejected
		}
		resultText = "工具执行失败：" + errText
	} else {
		resultText = result.Content
		if toolID == tool.RememberToolID && target.memoryWritten != nil {
			*target.memoryWritten = true
		}
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
	if !hidden {
		if !emit(contract.ChatEvent{
			Type:       contract.ChatEventToolResult,
			ToolCallID: callID,
			ToolName:   toolID,
			Result:     resultText,
			Error:      errText,
			ToolStatus: string(status),
		}) {
			return false
		}
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
// 名字使用供应商能接受的字符集，内部 ID 由 Service.Resolve 反查。
func toolSchemas(specs []tool.ToolSpec) []llm.ToolSchema {
	out := make([]llm.ToolSchema, 0, len(specs))
	for _, spec := range specs {
		out = append(out, llm.ToolSchema{Name: tool.ModelName(spec.ID), Description: spec.Description, Parameters: spec.Parameters})
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
		Reasoning:      item.Reasoning,
		Status:         contract.MessageStatus(item.Status),
		CreatedAt:      item.CreatedAt,
		Error:          item.Error,
		MemoryWritten:  item.MemoryWritten,
		Emotion:        item.Emotion,
		SenderName:     item.SenderName,
		SenderAvatar:   item.SenderAvatar,
		SenderKind:     item.SenderKind,
		SenderID:       item.SenderID,
		MessageType:    item.MessageType,
		TaskID:         item.TaskID,
		Visibility:     item.Visibility,
	}
}

// attachTaskBriefs 给带 task_id 的消息补上任务摘要，供 UI 直接渲染任务卡片。
func attachTaskBriefs(st *store.Store, items []contract.ChatMessage) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.TaskID != "" {
			ids = append(ids, item.TaskID)
		}
	}
	if len(ids) == 0 {
		return
	}

	ctx := context.Background()
	tasks, err := st.Tasks.ListByIDs(ctx, ids)
	if err != nil || len(tasks) == 0 {
		return
	}
	byID := make(map[string]*contract.TaskBrief, len(tasks))
	names := make(map[string]string, len(tasks))
	for _, item := range tasks {
		name, ok := names[item.AssigneeID]
		if !ok {
			if agent, err := st.Agents.Get(ctx, item.AssigneeID); err == nil {
				name = agent.Name
			}
			names[item.AssigneeID] = name
		}
		byID[item.ID] = taskBrief(item, name)
	}
	for i := range items {
		if brief, ok := byID[items[i].TaskID]; ok {
			items[i].Task = brief
		}
	}
}

// attachArtifacts 给消息补上产物摘要：先按消息 ID，再按任务 ID 关联，
// 让任务汇报在来源会话里也能展示员工产出的文件。
func attachArtifacts(st *store.Store, service *ArtifactService, items []contract.ChatMessage) {
	messageIDs := make([]string, 0, len(items))
	taskIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" {
			messageIDs = append(messageIDs, item.ID)
		}
		if item.TaskID != "" {
			taskIDs = append(taskIDs, item.TaskID)
		}
	}
	if len(messageIDs) == 0 && len(taskIDs) == 0 {
		return
	}

	ctx := context.Background()
	byMessage := make(map[string][]domain.Artifact)
	byTask := make(map[string][]domain.Artifact)
	if artifacts, err := st.Artifacts.ListByMessageIDs(ctx, messageIDs); err == nil {
		for _, item := range artifacts {
			byMessage[item.MessageID] = append(byMessage[item.MessageID], item)
		}
	}
	if artifacts, err := st.Artifacts.ListByTaskIDs(ctx, taskIDs); err == nil {
		for _, item := range artifacts {
			byTask[item.TaskID] = append(byTask[item.TaskID], item)
		}
	}

	for i := range items {
		seen := make(map[string]struct{})
		list := make([]domain.Artifact, 0, 2)
		add := func(artifacts []domain.Artifact) {
			for _, item := range artifacts {
				if _, ok := seen[item.ID]; ok {
					continue
				}
				seen[item.ID] = struct{}{}
				list = append(list, item)
			}
		}
		add(byMessage[items[i].ID])
		// 任务汇报在来源会话里的消息 ID 与产物不同，按任务 ID 补全；
		// 任务线程内的消息已按 message_id 关联，避免把任务产物重复挂到每条消息。
		if items[i].TaskID != "" && items[i].MessageType == domain.MessageTypeTaskReport && items[i].Visibility != domain.VisibilityInternal {
			add(byTask[items[i].TaskID])
		}
		if len(list) == 0 {
			continue
		}

		briefs := make([]contract.ArtifactBrief, 0, len(list))
		for _, item := range list {
			if service != nil {
				briefs = append(briefs, service.brief(item))
				continue
			}
			briefs = append(briefs, contract.ArtifactBrief{
				ID:        item.ID,
				Title:     item.Title,
				Kind:      contract.ArtifactKind(item.Kind),
				MIME:      item.MIME,
				Filename:  item.Filename,
				Size:      item.Size,
				CreatedAt: item.CreatedAt,
				Available: true,
			})
		}
		items[i].Artifacts = briefs
	}
}

func toContractMessages(items []domain.Message, conversationID string, calls []domain.ToolCall) []contract.ChatMessage {
	byMessage := make(map[string][]contract.ChatToolCall, len(calls))
	for _, call := range calls {
		// 记忆工具是内部动作，重新加载历史时也不展示成工具卡片。
		if tool.IsMemoryTool(call.ToolID) {
			continue
		}
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

// stripSpeakerPrefix 去掉回复开头模仿出来的「【名字】」「[名字]」「名字：」等前缀。
// 只匹配当前发言人姓名，避免误删正文里的普通方括号内容。
func stripSpeakerPrefix(content, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return content
	}
	prefixes := []string{
		"【" + name + "】",
		"[" + name + "]",
		"［" + name + "］",
		name + "：",
		name + ":",
	}
	trimmed := strings.TrimLeft(content, " \t\r\n")
	for {
		removed := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(trimmed, prefix) {
				trimmed = strings.TrimLeft(strings.TrimPrefix(trimmed, prefix), " \t\r\n：:")
				removed = true
				break
			}
		}
		if !removed {
			break
		}
	}
	if strings.TrimSpace(trimmed) == "" {
		return content
	}
	return trimmed
}

// toGroupLLMMessages 在群聊历史里标注发言人，避免成员回复被模型当成群主发言。
func toGroupLLMMessages(items []domain.Message) []llm.Message {
	out := make([]llm.Message, 0, len(items))
	for _, item := range items {
		if item.Content == "" {
			continue
		}
		content := item.Content
		if item.Role == domain.RoleAssistant {
			name := strings.TrimSpace(item.SenderName)
			if name == "" {
				if item.SenderKind == domain.SenderAgent {
					name = "成员"
				} else {
					name = "群主"
				}
			}
			content := stripSpeakerPrefix(item.Content, name)
			content = "【" + name + "】" + content
		}
		out = append(out, llm.Message{Role: string(item.Role), Content: content})
	}
	return out
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
