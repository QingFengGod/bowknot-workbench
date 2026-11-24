package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/platform/id"
	"bowknot-workbench/internal/store"
)

const (
	// taskTimeout 是单个后台任务的最长执行时间，超时后停止线程并汇报。
	taskTimeout = 10 * time.Minute
	// maxTaskActive 限制全局同时执行的任务数，避免一次派发压满模型网关。
	maxTaskActive = 4
	// maxTaskSummaryChars 限制写入任务的结果摘要长度。
	maxTaskSummaryChars = 1200
	// maxTaskAttempts 是任务总执行次数上限（首次 + 2 次自动重试）。
	maxTaskAttempts = 3
	// auditRecentLimit 限制审计页返回的最近任务条数。
	auditRecentLimit = 100
)

// taskRetryDelays 是自动重试的退避间隔，下标为已失败的次数。
var taskRetryDelays = []time.Duration{5 * time.Second, 15 * time.Second}

// TaskDispatchInput 是一次任务派发的入参。
type TaskDispatchInput struct {
	AgentID string
	Brief   string
	// OriginConversationID 是任务来源会话，汇报写回这里。
	OriginConversationID string
	OriginMessageID      string
}

// TaskService 负责主助手派发任务的排队、执行、终态收敛与汇报。
type TaskService struct {
	store     *store.Store
	logger    *slog.Logger
	agents    *AgentService
	assistant *AssistantService
	provider  llm.Provider
	reports   *ReportService
	memory    *MemoryService
	// settings 为 nil 时使用内置默认值，便于测试直接构造服务。
	settings *SettingsService

	mu       sync.Mutex
	queues   map[string][]string
	busy     map[string]bool
	done     map[string]chan struct{}
	canceled map[string]bool
	threads  map[string]string
	active   int
	closed   bool

	wake   chan struct{}
	stop   chan struct{}
	wg     sync.WaitGroup
	report sync.WaitGroup

	revision atomic.Uint64
}

func NewTaskService(st *store.Store, logger *slog.Logger, provider llm.Provider, agents *AgentService, assistant *AssistantService) *TaskService {
	if logger == nil {
		logger = slog.Default()
	}
	if provider == nil {
		provider = llm.NewFake(llm.FakeOptions{})
	}
	s := &TaskService{
		store:     st,
		logger:    logger,
		agents:    agents,
		assistant: assistant,
		provider:  provider,
		queues:    map[string][]string{},
		busy:      map[string]bool{},
		done:      map[string]chan struct{}{},
		canceled:  map[string]bool{},
		threads:   map[string]string{},
		wake:      make(chan struct{}, 1),
		stop:      make(chan struct{}),
	}
	s.reports = NewReportService(st, logger, provider, assistant)
	go s.loop()
	return s
}

// SetMemory 注入长期记忆服务；任务结束后会写入一条结果快照。
func (s *TaskService) SetMemory(memory *MemoryService) {
	s.memory = memory
}

// SetSettings 注入运行设置；为 nil 时回退内置默认值。
func (s *TaskService) SetSettings(settings *SettingsService) {
	s.settings = settings
}

// Shutdown 停止调度并等待在途任务收尾；不等待汇报补写，避免阻塞退出。
func (s *TaskService) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.stop)
	threads := make([]string, 0, len(s.threads))
	for _, threadID := range s.threads {
		threads = append(threads, threadID)
	}
	s.mu.Unlock()

	// 先停止在途生成，避免退出时一直等待任务自然结束。
	for _, threadID := range threads {
		s.agents.Stop(threadID)
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return nil
}

// Wait 等待排队任务全部执行、汇报结束，供测试使用。
func (s *TaskService) Wait() {
	for {
		s.mu.Lock()
		pending := s.active > 0
		for _, queue := range s.queues {
			if len(queue) > 0 {
				pending = true
				break
			}
		}
		s.mu.Unlock()
		if !pending {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	s.wg.Wait()
	s.report.Wait()
}

func (s *TaskService) TaskRevision() uint64 {
	return s.revision.Load()
}

func (s *TaskService) bump() {
	s.revision.Add(1)
}

// Dispatch 创建任务与任务线程并立即入队，调用方不需要等待执行。
func (s *TaskService) Dispatch(ctx context.Context, in TaskDispatchInput) (contract.TaskDetail, error) {
	agentID := strings.TrimSpace(in.AgentID)
	brief := strings.TrimSpace(in.Brief)
	if agentID == "" {
		return contract.TaskDetail{}, errors.New("agentId 不能为空，请先调用 builtin_list_agents")
	}
	if brief == "" {
		return contract.TaskDetail{}, errors.New("委派任务不能为空")
	}

	agent, err := s.store.Agents.Get(ctx, agentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return contract.TaskDetail{}, fmt.Errorf("Agent 不存在：%s，请先调用 builtin_list_agents 重新选择", agentID)
		}
		return contract.TaskDetail{}, fmt.Errorf("读取 Agent 失败：%w", err)
	}

	now := time.Now().UnixMilli()
	title := taskTitle(brief)
	origin := strings.TrimSpace(in.OriginConversationID)
	groupID := ""
	if origin != "" {
		if conversation, err := s.store.Conversations.Get(ctx, origin); err == nil && conversation.Kind == domain.KindGroup {
			groupID = conversation.OwnerID
		}
	}
	task := domain.Task{
		ID:                   "task-" + id.New(),
		Title:                title,
		Brief:                brief,
		Status:               domain.TaskQueued,
		AssignerKind:         domain.AssignerMain,
		AssignerID:           mainAgentID,
		AssigneeID:           agentID,
		OriginConversationID: origin,
		OriginMessageID:      strings.TrimSpace(in.OriginMessageID),
		ThreadConversationID: "conv-" + id.New(),
		GroupID:              groupID,
		CreatedAt:            now,
	}
	thread := domain.Conversation{
		ID:        task.ThreadConversationID,
		Kind:      domain.KindAgent,
		OwnerID:   agentID,
		Title:     "任务：" + truncateRunes(title, 18),
		Summary:   snippet(brief, 60),
		CreatedAt: now,
		UpdatedAt: now,
	}
	dispatchText := s.dispatchText(agent, task)

	if err := s.store.Tx(ctx, func(tx *store.Store) error {
		if err := tx.Conversations.Create(ctx, thread); err != nil {
			return err
		}
		if err := tx.Tasks.Create(ctx, task); err != nil {
			return err
		}
		_, err := tx.Messages.Append(ctx, domain.Message{
			ID:             id.New(),
			ConversationID: thread.ID,
			Role:           domain.RoleUser,
			Content:        dispatchText,
			Status:         domain.MessageDone,
			CreatedAt:      now,
			SenderKind:     domain.SenderMain,
			SenderID:       mainAgentID,
			MessageType:    domain.MessageTypeTaskDispatch,
			TaskID:         task.ID,
			Visibility:     domain.VisibilityInternal,
		})
		return err
	}); err != nil {
		return contract.TaskDetail{}, fmt.Errorf("创建任务失败：%w", err)
	}

	s.enqueue(task.ID, task.AssigneeID)
	s.bump()
	return s.detail(ctx, task), nil
}

// DispatchTask 供 UI 的显式「转交」入口调用，来源会话用于回写汇报。
func (s *TaskService) DispatchTask(agentID, brief, originConversationID string) (contract.TaskDetail, error) {
	return s.Dispatch(context.Background(), TaskDispatchInput{
		AgentID:              agentID,
		Brief:                brief,
		OriginConversationID: originConversationID,
	})
}

// CancelTask 取消排队中或运行中的任务。
func (s *TaskService) CancelTask(taskID string) error {
	ctx := context.Background()
	task, err := s.store.Tasks.Get(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("任务不存在：%s", taskID)
		}
		return fmt.Errorf("读取任务失败：%w", err)
	}
	if task.Status.Terminal() {
		return errors.New("任务已结束，无需取消")
	}

	s.mu.Lock()
	s.canceled[taskID] = true
	queue := s.queues[task.AssigneeID]
	for i, queued := range queue {
		if queued == taskID {
			s.queues[task.AssigneeID] = append(queue[:i], queue[i+1:]...)
			break
		}
	}
	done := s.done[taskID]
	s.mu.Unlock()

	if task.Status == domain.TaskQueued {
		s.finalize(task, domain.TaskCanceled, "", "", "任务已取消")
		return nil
	}

	s.agents.Stop(task.ThreadConversationID)
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
	// 执行协程可能已经收尾；条件更新保证只有一方能写终态并汇报。
	s.finalize(task, domain.TaskCanceled, "", "", "任务已取消")
	return nil
}

// RetryTask 重新执行一个已结束的任务，复用原线程。
func (s *TaskService) RetryTask(taskID string) error {
	ctx := context.Background()
	task, err := s.store.Tasks.Get(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("任务不存在：%s", taskID)
		}
		return fmt.Errorf("读取任务失败：%w", err)
	}
	if !task.Status.Terminal() {
		return errors.New("任务尚未结束，无法重试")
	}

	now := time.Now().UnixMilli()
	ok, err := s.store.Tasks.Requeue(ctx, task.ID, now)
	if err != nil {
		return fmt.Errorf("重试任务失败：%w", err)
	}
	if !ok {
		return errors.New("任务状态已变化，请刷新后重试")
	}

	retryText := "主助手要求你继续完成上一个任务（第 " + fmt.Sprint(task.Attempt+2) + " 次尝试）：\n\n【任务】" + task.Brief
	if _, err := s.store.Messages.Append(ctx, domain.Message{
		ID:             id.New(),
		ConversationID: task.ThreadConversationID,
		Role:           domain.RoleUser,
		Content:        retryText,
		Status:         domain.MessageDone,
		CreatedAt:      now,
		SenderKind:     domain.SenderMain,
		SenderID:       mainAgentID,
		MessageType:    domain.MessageTypeTaskDispatch,
		TaskID:         task.ID,
		Visibility:     domain.VisibilityInternal,
	}); err != nil {
		return fmt.Errorf("写入重试说明失败：%w", err)
	}
	s.mu.Lock()
	delete(s.canceled, task.ID)
	s.mu.Unlock()
	s.enqueue(task.ID, task.AssigneeID)
	s.bump()
	return nil
}

func (s *TaskService) enqueue(taskID, assigneeID string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.queues[assigneeID] = append(s.queues[assigneeID], taskID)
	s.mu.Unlock()
	s.notify()
}

func (s *TaskService) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *TaskService) loop() {
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
		}
		s.schedule()
	}
}

// schedule 按员工串行、全局并发的规则认领排队任务。
func (s *TaskService) schedule() {
	for {
		// 每次认领都读取当前并发上限，调小后不再派发新任务。
		limit := maxTaskActive
		if s.settings != nil {
			limit = s.settings.Task().MaxConcurrent
		}
		if limit <= 0 {
			limit = maxTaskActive
		}

		s.mu.Lock()
		if s.closed || s.active >= limit {
			s.mu.Unlock()
			return
		}
		assignee, taskID := "", ""
		for id, queue := range s.queues {
			if s.busy[id] || len(queue) == 0 {
				continue
			}
			assignee, taskID = id, queue[0]
			s.queues[id] = queue[1:]
			break
		}
		if taskID == "" {
			s.mu.Unlock()
			return
		}
		s.busy[assignee] = true
		s.active++
		s.mu.Unlock()

		claimed, err := s.store.Tasks.Claim(context.Background(), taskID, time.Now().UnixMilli())
		if err != nil || !claimed {
			if err != nil {
				s.logger.Warn("认领任务失败", "task", taskID, "error", err)
			}
			s.release(assignee)
			continue
		}

		done := make(chan struct{})
		s.mu.Lock()
		s.done[taskID] = done
		s.mu.Unlock()
		s.bump()

		s.wg.Add(1)
		go s.execute(taskID, assignee)
	}
}

func (s *TaskService) release(assignee string) {
	s.mu.Lock()
	delete(s.busy, assignee)
	if s.active > 0 {
		s.active--
	}
	s.mu.Unlock()
	s.notify()
}

// releaseTask 在任务执行结束时释放员工槽位并关闭等待通道。
func (s *TaskService) releaseTask(assignee, taskID string) {
	s.mu.Lock()
	if ch := s.done[taskID]; ch != nil {
		close(ch)
		delete(s.done, taskID)
	}
	delete(s.canceled, taskID)
	delete(s.threads, taskID)
	delete(s.busy, assignee)
	if s.active > 0 {
		s.active--
	}
	s.mu.Unlock()
	s.notify()
}

func (s *TaskService) execute(taskID, assignee string) {
	defer s.wg.Done()
	defer s.releaseTask(assignee, taskID)

	ctx := context.Background()
	task, err := s.store.Tasks.Get(ctx, taskID)
	if err != nil {
		s.logger.Warn("读取待执行任务失败", "task", taskID, "error", err)
		return
	}
	// 认领与取消可能并发：已取消的任务不再启动生成。
	s.mu.Lock()
	canceled := s.canceled[task.ID]
	s.mu.Unlock()
	if task.Status.Terminal() {
		return
	}
	if canceled {
		s.complete(task, "", "", "任务已取消")
		return
	}

	s.mu.Lock()
	s.threads[task.ID] = task.ThreadConversationID
	s.mu.Unlock()

	messageID, events, err := s.agents.StartTurn(TurnInput{
		ConversationID: task.ThreadConversationID,
		SenderKind:     domain.SenderAgent,
		SenderID:       task.AssigneeID,
		MessageType:    domain.MessageTypeTaskReport,
		TaskID:         task.ID,
		Visibility:     domain.VisibilityInternal,
		SystemExtra:    s.taskSystemExtra(task),
		AutoApprove:    true,
	})
	if err != nil {
		s.finalize(task, domain.TaskFailed, "", "", "无法启动任务："+err.Error())
		return
	}

	// 生成启动期间可能收到取消请求，立即停止本轮生成。
	s.mu.Lock()
	canceled = s.canceled[task.ID]
	s.mu.Unlock()
	if canceled {
		s.agents.Stop(task.ThreadConversationID)
		s.complete(task, messageID, "", "任务已取消")
		return
	}

	timeout := taskTimeout
	if s.settings != nil {
		timeout = time.Duration(s.settings.Task().TimeoutMinutes) * time.Minute
	}
	if timeout <= 0 {
		timeout = taskTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var (
		result  string
		errText string
	)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				s.complete(task, messageID, result, errText)
				return
			}
			if !event.Done {
				continue
			}
			result = event.Content
			if event.Status != contract.MessageDone {
				errText = strings.TrimSpace(event.Error)
				if errText == "" {
					errText = "任务没有正常完成"
				}
			}
		case <-timer.C:
			s.agents.Stop(task.ThreadConversationID)
			s.finalize(task, domain.TaskTimeout, "", messageID, "任务执行超时")
			return
		}
	}
}

// complete 把生成结果映射为任务终态并触发汇报。
func (s *TaskService) complete(task domain.Task, messageID, content, errText string) {
	s.mu.Lock()
	canceled := s.canceled[task.ID]
	s.mu.Unlock()

	status := domain.TaskDone
	switch {
	case canceled:
		status = domain.TaskCanceled
		errText = "任务已取消"
	case strings.TrimSpace(errText) != "":
		status = domain.TaskFailed
	}

	// 网络、限流等临时错误先静默退避重试，不向用户汇报中间失败。
	if status == domain.TaskFailed && s.scheduleAutoRetry(task, errText) {
		return
	}

	summary := ""
	if status == domain.TaskDone {
		summary = s.summarize(content)
	}
	s.finalize(task, status, summary, messageID, errText)
}

// scheduleAutoRetry 对可恢复错误安排一次延迟重试；返回 true 表示已排队。
func (s *TaskService) scheduleAutoRetry(task domain.Task, errText string) bool {
	attempts := maxTaskAttempts
	if s.settings != nil {
		attempts = s.settings.Task().MaxAttempts
	}
	if attempts <= 0 {
		attempts = maxTaskAttempts
	}
	if task.Attempt+1 >= attempts || !isRetryableTaskError(errText) {
		return false
	}
	s.mu.Lock()
	closing := s.closed
	s.mu.Unlock()
	if closing {
		return false
	}

	ok, err := s.store.Tasks.RetryLater(context.Background(), task.ID, errText)
	if err != nil {
		s.logger.Warn("安排任务自动重试失败", "task", task.ID, "error", err)
		return false
	}
	if !ok {
		return false
	}
	s.bump()

	delay := s.retryDelay(task.Attempt)
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-s.stop:
			return
		case <-timer.C:
		}
		s.mu.Lock()
		closing := s.closed
		s.mu.Unlock()
		if closing {
			return
		}
		s.enqueue(task.ID, task.AssigneeID)
	}()
	return true
}

// retryDelay 返回第 attempt 次失败后的退避间隔。
// 未注入设置时沿用包级变量，测试可以把它改成毫秒级。
func (s *TaskService) retryDelay(attempt int) time.Duration {
	if s.settings != nil {
		delays := s.settings.Task().RetryDelaysSeconds
		if len(delays) == 0 {
			return 0
		}
		if attempt < 0 {
			attempt = 0
		}
		if attempt >= len(delays) {
			attempt = len(delays) - 1
		}
		return time.Duration(delays[attempt]) * time.Second
	}
	if attempt >= 0 && attempt < len(taskRetryDelays) {
		return taskRetryDelays[attempt]
	}
	return taskRetryDelays[len(taskRetryDelays)-1]
}

// isRetryableTaskError 只对临时性错误自动重试，权限与参数错误直接失败。
func isRetryableTaskError(text string) bool {
	for _, marker := range []string{"限流", "超时", "无法连接", "连接中断", "网络"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// finalize 抢到终态写入权后才汇报，保证每个任务只汇报一次。
func (s *TaskService) finalize(task domain.Task, status domain.TaskStatus, summary, messageID, errText string) {
	ok, err := s.store.Tasks.Finish(context.Background(), task.ID, status, summary, messageID, errText, time.Now().UnixMilli())
	if err != nil {
		s.logger.Warn("写入任务终态失败", "task", task.ID, "error", err)
		return
	}
	if !ok {
		return
	}

	task.Status = status
	task.ResultSummary = summary
	task.ResultMessageID = messageID
	task.Error = errText
	task.FinishedAt = time.Now().UnixMilli()
	s.bump()

	// 退出过程中不补写汇报，避免拖住关闭流程。
	s.mu.Lock()
	closing := s.closed
	s.mu.Unlock()
	if closing {
		return
	}

	s.report.Add(1)
	go func() {
		defer s.report.Done()
		s.snapshotMemory(task)
		s.reports.Report(task)
	}()
}

// snapshotMemory 把任务结果写入员工自己的长期记忆，供后续跨任务检索。
func (s *TaskService) snapshotMemory(task domain.Task) {
	if s.memory == nil {
		return
	}
	if task.Status != domain.TaskDone && task.Status != domain.TaskFailed {
		return
	}
	if !s.memory.MemoryEnabled(task.AssigneeID) {
		return
	}

	var content string
	switch task.Status {
	case domain.TaskDone:
		summary := strings.TrimSpace(task.ResultSummary)
		if summary == "" {
			summary = "没有返回具体结果。"
		}
		content = fmt.Sprintf("我完成了任务《%s》。结果：%s", task.Title, truncateRunes(summary, 500))
	case domain.TaskFailed:
		reason := strings.TrimSpace(task.Error)
		if reason == "" {
			reason = "任务没有正常完成"
		}
		content = fmt.Sprintf("任务《%s》执行失败：%s", task.Title, reason)
	}

	if artifacts, err := s.store.Artifacts.ListByTaskIDs(context.Background(), []string{task.ID}); err == nil && len(artifacts) > 0 {
		content += fmt.Sprintf(" 产出 %d 个文件。", len(artifacts))
	}

	if _, err := s.memory.Remember(context.Background(), task.AssigneeID, task.ThreadConversationID,
		task.ResultMessageID, content, string(domain.MemoryKindEvent), time.UnixMilli(task.FinishedAt)); err != nil {
		s.logger.Debug("写入任务记忆快照失败", "task", task.ID, "error", err)
	}
}

// summarize 让模型把员工结果压缩成要点，失败时回退为截断原文。
func (s *TaskService) summarize(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	fallback := truncateRunes(content, maxTaskSummaryChars)
	if s.provider == nil {
		return fallback
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reply, err := s.provider.Complete(ctx, llm.ChatRequest{
		System: "你把员工的任务结果压缩成便于主助手转述的要点，输出 3-6 行纯文本，只保留结果与关键信息，不要评价，不要提到「用户」。",
		Messages: []llm.Message{{
			Role:    "user",
			Content: truncateRunes(content, 6000),
		}},
	})
	reply = strings.TrimSpace(reply)
	if err != nil || reply == "" {
		if err != nil {
			s.logger.Warn("压缩任务结果失败，使用原文截断", "error", err)
		}
		return fallback
	}
	return truncateRunes(reply, maxTaskSummaryChars)
}

// dispatchText 是写入任务线程的第一条消息，用员工对用户的称呼组织任务指令。
func (s *TaskService) dispatchText(agent domain.Agent, task domain.Task) string {
	address := strings.TrimSpace(parsePersonaText(agent.Persona).UserName)
	sender := "主助手「" + s.agents.leaderTitle() + "」"
	text := sender + "把任务交给你，请独立完成并把结果写在本会话：\n\n【任务】" + task.Brief
	if address != "" {
		text = sender + "把" + address + "交代的任务转给你，请独立完成并把结果写在本会话：\n\n【任务】" + task.Brief
	}
	return text
}

// taskSystemExtra 是任务模式下追加到系统提示词的上下文。
func (s *TaskService) taskSystemExtra(task domain.Task) string {
	return "【任务模式】你正在执行" + s.agents.leaderTitle() + "派发的任务「" + task.Title + "」。\n" +
		"任务要求：\n" + task.Brief + "\n" +
		"你只能把结果写进本会话，由主助手向用户汇报；不要直接联系用户，不要称呼用户，也不要说「我会转告」之类的话。\n" +
		"请给出完整、可直接交付的结果。"
}

// taskStatusLabel 把任务状态转成中文文案，供工具输出与提示词共用。
func taskStatusLabel(status domain.TaskStatus) string {
	switch status {
	case domain.TaskQueued:
		return "排队中"
	case domain.TaskRunning:
		return "运行中"
	case domain.TaskDone:
		return "已完成"
	case domain.TaskFailed:
		return "失败"
	case domain.TaskCanceled:
		return "已取消"
	case domain.TaskTimeout:
		return "已超时"
	default:
		return "未知"
	}
}

// taskTitle 从任务文本里取一个短标题：优先取第一句。
func taskTitle(text string) string {
	text = strings.TrimSpace(text)
	if idx := strings.IndexAny(text, "。\n；;"); idx > 0 {
		text = strings.TrimSpace(text[:idx])
	}
	text = strings.TrimSpace(strings.TrimSuffix(text, "要求"))
	return truncateRunes(text, 24)
}

// ---------- 查询 ----------

func (s *TaskService) Tasks(agentID string, status contract.TaskStatus, limit int) []contract.TaskDetail {
	items, err := s.store.Tasks.List(context.Background(), store.TaskFilter{
		AssigneeID: agentID,
		Status:     domain.TaskStatus(status),
		Limit:      limit,
	})
	if err != nil {
		s.logger.Error("读取任务列表失败", "error", err)
		return nil
	}
	out := make([]contract.TaskDetail, 0, len(items))
	for _, item := range items {
		out = append(out, s.detail(context.Background(), item))
	}
	return out
}

func (s *TaskService) Task(taskID string) contract.TaskDetail {
	item, err := s.store.Tasks.Get(context.Background(), taskID)
	if err != nil {
		return contract.TaskDetail{}
	}
	return s.detail(context.Background(), item)
}

// SearchTasks 按标题与任务描述检索历史任务，供跨任务查找。
func (s *TaskService) SearchTasks(query string, limit int) []contract.TaskDetail {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	items, err := s.store.Tasks.List(context.Background(), store.TaskFilter{
		Search: query,
		Limit:  limit,
	})
	if err != nil {
		s.logger.Error("检索任务失败", "query", query, "error", err)
		return nil
	}
	out := make([]contract.TaskDetail, 0, len(items))
	for _, item := range items {
		out = append(out, s.detail(context.Background(), item))
	}
	return out
}

// TaskAudit 汇总最近 days 天的任务统计与最近任务；days <= 0 表示全部。
func (s *TaskService) TaskAudit(days int) contract.TaskAudit {
	ctx := context.Background()
	items, err := s.store.Tasks.List(ctx, store.TaskFilter{})
	if err != nil {
		s.logger.Error("读取任务审计失败", "error", err)
		return contract.TaskAudit{}
	}

	cutoff := int64(0)
	if days > 0 {
		cutoff = time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	}

	overall := &taskStatsAccumulator{}
	byAgent := make(map[string]*taskStatsAccumulator)
	names := make(map[string]string)
	audit := contract.TaskAudit{}

	for _, item := range items {
		if cutoff > 0 && item.CreatedAt < cutoff {
			continue
		}
		overall.add(item)
		acc := byAgent[item.AssigneeID]
		if acc == nil {
			acc = &taskStatsAccumulator{}
			byAgent[item.AssigneeID] = acc
		}
		acc.add(item)
		if _, ok := names[item.AssigneeID]; !ok {
			name := ""
			if agent, err := s.store.Agents.Get(ctx, item.AssigneeID); err == nil {
				name = agent.Name
			}
			names[item.AssigneeID] = name
		}
		if len(audit.Recent) < auditRecentLimit {
			audit.Recent = append(audit.Recent, s.detail(ctx, item))
		}
	}

	audit.Overall = overall.result()
	for id, acc := range byAgent {
		audit.Agents = append(audit.Agents, contract.AgentTaskStats{
			AgentID:   id,
			AgentName: names[id],
			Stats:     acc.result(),
		})
	}
	sort.Slice(audit.Agents, func(i, j int) bool {
		if audit.Agents[i].AgentName != audit.Agents[j].AgentName {
			return audit.Agents[i].AgentName < audit.Agents[j].AgentName
		}
		return audit.Agents[i].AgentID < audit.Agents[j].AgentID
	})
	return audit
}

// taskStatsAccumulator 逐条累计任务状态与耗时。
type taskStatsAccumulator struct {
	stats         contract.TaskStats
	durationSum   int64
	durationCount int
}

func (a *taskStatsAccumulator) add(item domain.Task) {
	a.stats.Total++
	switch item.Status {
	case domain.TaskQueued:
		a.stats.Queued++
	case domain.TaskRunning:
		a.stats.Running++
	case domain.TaskDone:
		a.stats.Done++
	case domain.TaskFailed:
		a.stats.Failed++
	case domain.TaskCanceled:
		a.stats.Canceled++
	case domain.TaskTimeout:
		a.stats.Timeout++
	}
	if item.StartedAt > 0 && item.FinishedAt >= item.StartedAt {
		a.durationSum += item.FinishedAt - item.StartedAt
		a.durationCount++
	}
}

func (a *taskStatsAccumulator) result() contract.TaskStats {
	stats := a.stats
	terminal := stats.Done + stats.Failed + stats.Canceled + stats.Timeout
	if terminal > 0 {
		stats.SuccessRate = float64(stats.Done) / float64(terminal)
	}
	if a.durationCount > 0 {
		stats.AvgDurationMs = a.durationSum / int64(a.durationCount)
	}
	return stats
}

func (s *TaskService) TaskMessages(taskID string) []contract.ChatMessage {
	task, err := s.store.Tasks.Get(context.Background(), taskID)
	if err != nil {
		return nil
	}
	return s.agents.Messages(task.ThreadConversationID)
}

func (s *TaskService) detail(ctx context.Context, item domain.Task) contract.TaskDetail {
	detail := contract.TaskDetail{
		ID:                   item.ID,
		Title:                item.Title,
		Brief:                item.Brief,
		Status:               contract.TaskStatus(item.Status),
		AssigneeID:           item.AssigneeID,
		OriginConversationID: item.OriginConversationID,
		ThreadConversationID: item.ThreadConversationID,
		Attempt:              item.Attempt,
		ResultSummary:        item.ResultSummary,
		Error:                item.Error,
		CreatedAt:            item.CreatedAt,
		StartedAt:            item.StartedAt,
		FinishedAt:           item.FinishedAt,
	}
	if item.StartedAt > 0 && item.FinishedAt >= item.StartedAt {
		detail.DurationMs = item.FinishedAt - item.StartedAt
	}
	if agent, err := s.store.Agents.Get(ctx, item.AssigneeID); err == nil {
		detail.AssigneeName = agent.Name
	}
	if conversation, err := s.store.Conversations.Get(ctx, item.OriginConversationID); err == nil {
		detail.OriginTitle = conversation.Title
	}
	return detail
}
