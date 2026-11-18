package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/tool"
)

// maxListedAgents 限制列举结果条数，避免把过多 Agent 塞进上下文。
const maxListedAgents = 20

// AgentHub 实现内置的创建 / 列举 / 派发 Agent 工具。
// 派发只创建任务并立即返回，执行、汇报由 TaskService 负责。
type AgentHub struct {
	agents *AgentService
	tasks  *TaskService
}

func NewAgentHub(agents *AgentService, tasks *TaskService) *AgentHub {
	return &AgentHub{agents: agents, tasks: tasks}
}

func (h *AgentHub) CreateAgent(ctx context.Context, in tool.CreateAgentInput) (tool.AgentRef, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return tool.AgentRef{}, errors.New("Agent 姓名不能为空")
	}
	profile := contract.AssistantPersona{
		Name:        name,
		Gender:      strings.TrimSpace(in.Gender),
		Age:         strings.TrimSpace(in.Age),
		Personality: strings.TrimSpace(in.Personality),
		Role:        strings.TrimSpace(in.Role),
		UserName:    strings.TrimSpace(in.UserName),
		Custom:      strings.TrimSpace(in.Custom),
	}
	info := h.agents.CreateAgent(name, profile.Role, personaText(profile))
	if info.ID == "" {
		return tool.AgentRef{}, errors.New("创建 Agent 失败，请查看日志")
	}
	return tool.AgentRef{
		ID:          info.ID,
		Name:        info.Name,
		Description: info.Description,
		Persona:     info.Persona,
	}, nil
}

func (h *AgentHub) ListAgents(ctx context.Context, query string) ([]tool.AgentRef, error) {
	keyword := strings.ToLower(strings.TrimSpace(query))
	out := make([]tool.AgentRef, 0, maxListedAgents)
	for _, item := range h.agents.Agents() {
		if len(out) >= maxListedAgents {
			break
		}
		if keyword != "" && !matchesAgent(item, keyword) {
			continue
		}
		out = append(out, tool.AgentRef{
			ID:          item.ID,
			Name:        item.Name,
			Description: item.Description,
			Persona:     item.Persona,
		})
	}
	return out, nil
}

// RunAgent 把任务交给 TaskService 排队执行，调用方立即拿到任务 ID。
func (h *AgentHub) RunAgent(ctx context.Context, in tool.RunAgentInput) (string, error) {
	agentID := strings.TrimSpace(in.AgentID)
	task := strings.TrimSpace(in.Task)
	if agentID == "" {
		return "", errors.New("agentId 不能为空，请先调用 builtin_list_agents")
	}
	if task == "" {
		return "", errors.New("委派任务不能为空")
	}
	if h.tasks == nil {
		return "", errors.New("任务服务未启用")
	}

	detail, err := h.tasks.Dispatch(ctx, TaskDispatchInput{
		AgentID:              agentID,
		Brief:                task,
		OriginConversationID: in.ConversationID,
		OriginMessageID:      tool.MessageID(ctx),
	})
	if err != nil {
		return "", err
	}

	name := detail.AssigneeName
	if name == "" {
		name = "该员工"
	}
	return fmt.Sprintf("已把任务交给「%s」（任务 ID：%s）。它完成后我会马上向你汇报，你可以继续安排别的事。", name, detail.ID), nil
}

// ListTasks 返回任务摘要，供主助手回答「进度如何」。
func (h *AgentHub) ListTasks(ctx context.Context, in tool.ListTasksInput) ([]tool.TaskRef, error) {
	if h.tasks == nil {
		return nil, errors.New("任务服务未启用")
	}
	status := contract.TaskStatus(strings.TrimSpace(in.Status))
	if status != "" && !status.Terminal() && status != contract.TaskQueued && status != contract.TaskRunning {
		return nil, fmt.Errorf("未知的任务状态：%s", in.Status)
	}
	limit := in.Limit
	if limit <= 0 || limit > 30 {
		limit = 10
	}
	agentID := strings.TrimSpace(in.AgentID)
	query := strings.TrimSpace(in.Query)
	var items []contract.TaskDetail
	if query != "" {
		items = h.tasks.SearchTasks(query, limit*3)
	} else {
		items = h.tasks.Tasks(agentID, status, limit)
	}
	out := make([]tool.TaskRef, 0, len(items))
	for _, item := range items {
		if agentID != "" && item.AssigneeID != agentID {
			continue
		}
		if status != "" && item.Status != status {
			continue
		}
		if len(out) >= limit {
			break
		}
		out = append(out, tool.TaskRef{
			ID:            item.ID,
			Title:         item.Title,
			Status:        string(item.Status),
			AssigneeName:  item.AssigneeName,
			Attempt:       item.Attempt,
			Error:         item.Error,
			ResultSummary: item.ResultSummary,
		})
	}
	return out, nil
}

// CancelTask 取消指定任务并返回可读结果。
func (h *AgentHub) CancelTask(ctx context.Context, taskID string) (string, error) {
	if h.tasks == nil {
		return "", errors.New("任务服务未启用")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", errors.New("taskId 不能为空，请先调用 builtin_list_tasks")
	}
	if err := h.tasks.CancelTask(taskID); err != nil {
		return "", err
	}
	return fmt.Sprintf("已取消任务 %s。", taskID), nil
}

func matchesAgent(item contract.AgentInfo, keyword string) bool {
	haystack := strings.ToLower(item.Name + " " + item.Description + " " + item.Persona)
	return strings.Contains(haystack, keyword)
}
