package app

import (
	"context"
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

// ReportService 把任务终态写成主助手对用户的汇报消息。
type ReportService struct {
	store     *store.Store
	logger    *slog.Logger
	provider  llm.Provider
	assistant *AssistantService
}

func NewReportService(st *store.Store, logger *slog.Logger, provider llm.Provider, assistant *AssistantService) *ReportService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReportService{store: st, logger: logger, provider: provider, assistant: assistant}
}

// Report 把任务结果写回来源会话；重复调用由 reported_at 去重。
func (r *ReportService) Report(task domain.Task) {
	origin := strings.TrimSpace(task.OriginConversationID)
	if origin == "" || origin == task.ThreadConversationID {
		return
	}

	ctx := context.Background()
	conversation, err := r.store.Conversations.Get(ctx, origin)
	if err != nil {
		r.logger.Warn("汇报任务失败：来源会话不存在", "task", task.ID, "origin", origin, "error", err)
		return
	}
	// 任务线程是内部工作记录，不写入汇报。
	if conversation.Kind == domain.KindAgent {
		return
	}

	agentName := r.agentName(ctx, task.AssigneeID)
	content := r.compose(task, agentName)

	now := time.Now().UnixMilli()
	if _, err := r.store.Messages.Append(ctx, domain.Message{
		ID:             id.New(),
		ConversationID: origin,
		Role:           domain.RoleAssistant,
		Content:        content,
		Status:         domain.MessageDone,
		CreatedAt:      now,
		SenderKind:     domain.SenderMain,
		SenderID:       mainAgentID,
		MessageType:    domain.MessageTypeTaskReport,
		TaskID:         task.ID,
		Visibility:     domain.VisibilityUser,
	}); err != nil {
		r.logger.Warn("写入任务汇报失败", "task", task.ID, "origin", origin, "error", err)
		return
	}
	if err := r.store.Conversations.UpdateMeta(ctx, origin, conversation.Title, snippet(content, 40), conversation.Pinned, now); err != nil {
		r.logger.Warn("更新汇报会话摘要失败", "task", task.ID, "error", err)
	}
	if err := r.store.Tasks.MarkReported(ctx, task.ID, now); err != nil {
		r.logger.Warn("标记任务已汇报失败", "task", task.ID, "error", err)
	}
	if r.assistant != nil {
		r.assistant.revision.Add(1)
	}
}

func (r *ReportService) agentName(ctx context.Context, agentID string) string {
	if agent, err := r.store.Agents.Get(ctx, agentID); err == nil && strings.TrimSpace(agent.Name) != "" {
		return agent.Name
	}
	return "员工"
}

// compose 让主助手用自己的语气汇报；模型失败时使用固定模板。
func (r *ReportService) compose(task domain.Task, agentName string) string {
	fallback := reportFallback(task, agentName)
	if r.provider == nil || r.assistant == nil {
		return fallback
	}

	model := ""
	if models := r.assistant.Models(); len(models) > 0 {
		model = models[0].ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reply, err := r.provider.Complete(ctx, llm.ChatRequest{
		Model:  model,
		System: r.assistant.systemPrompt(),
		Messages: []llm.Message{{
			Role:    "user",
			Content: reportPrompt(task, agentName),
		}},
	})
	reply = strings.TrimSpace(reply)
	if err != nil || reply == "" {
		if err != nil {
			r.logger.Warn("润色任务汇报失败，使用固定模板", "task", task.ID, "error", err)
		}
		return fallback
	}
	return humanizeReport(reply)
}

func reportPrompt(task domain.Task, agentName string) string {
	var detail string
	switch task.Status {
	case domain.TaskDone:
		summary := strings.TrimSpace(task.ResultSummary)
		if summary == "" {
			summary = "没有返回具体内容。"
		}
		detail = "任务结果：\n" + summary
	case domain.TaskCanceled:
		detail = "任务已被取消。"
	case domain.TaskTimeout:
		detail = "任务执行超时，已经停止。"
	default:
		reason := strings.TrimSpace(task.Error)
		if reason == "" {
			reason = "任务没有正常完成"
		}
		detail = "任务没有完成，原因：" + reason
	}

	return fmt.Sprintf("助手「%s」刚执行完任务「%s」。%s\n请用 1-3 句话向对方汇报，只写结果与关键信息，不要重复任务描述，不要提到「员工」「子 Agent」或「用户」这些词，直接用「你」指代对方。",
		agentName, truncateRunes(task.Title, 40), detail)
}

func reportFallback(task domain.Task, agentName string) string {
	title := taskTitle(task.Title)
	summary := strings.TrimSpace(task.ResultSummary)
	switch task.Status {
	case domain.TaskDone:
		if summary == "" {
			summary = "任务已完成，但没有返回具体内容。"
		}
		return fmt.Sprintf("「%s」的「%s」任务完成了：%s", agentName, title, summary)
	case domain.TaskCanceled:
		return fmt.Sprintf("已取消「%s」的「%s」任务。", agentName, title)
	case domain.TaskTimeout:
		return fmt.Sprintf("「%s」的「%s」任务执行超时，已经停止。", agentName, title)
	default:
		reason := strings.TrimSpace(task.Error)
		if reason == "" {
			reason = "任务没有正常完成"
		}
		return fmt.Sprintf("「%s」的「%s」任务没有完成：%s", agentName, title, reason)
	}
}

// humanizeReport 把汇报里的第三人称「用户」改成「你」，
// 避免主助手直接对用户说话时出现旁白式表达。
func humanizeReport(text string) string {
	pairs := []string{
		"为用户", "为你",
		"向用户", "向你",
		"给用户", "给你",
		"帮用户", "帮你",
		"对用户", "对你",
		"用户要求", "你要求",
		"用户提出", "你提出",
		"用户希望", "你希望",
		"用户需要", "你需要",
		"用户想要", "你想要",
		"用户选择", "你选择",
		"用户提供", "你提供",
		"用户反馈", "你反馈",
		"用户已", "你已",
		"用户可以", "你可以",
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// taskBrief 把任务转换成消息列表使用的摘要。
func taskBrief(item domain.Task, assigneeName string) *contract.TaskBrief {
	return &contract.TaskBrief{
		ID:                   item.ID,
		Title:                item.Title,
		Status:               contract.TaskStatus(item.Status),
		AssigneeID:           item.AssigneeID,
		AssigneeName:         assigneeName,
		ThreadConversationID: item.ThreadConversationID,
	}
}
