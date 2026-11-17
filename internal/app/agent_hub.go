package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/infra/tool"
)

// maxDelegatedChars 限制汇报给主对话的子 Agent 结果长度，
// 完整内容保留在子 Agent 的会话里。
const maxDelegatedChars = 3000

// maxListedAgents 限制列举结果条数，避免把过多 Agent 塞进上下文。
const maxListedAgents = 20

// delegationTimeout 是后台委派的最长等待时间，超时后停止子 Agent 并汇报失败。
const delegationTimeout = 10 * time.Minute

// AgentHub 实现内置的创建 / 列举 / 委派 Agent 工具。
type AgentHub struct {
	agents *AgentService
	// assistant 用于子 Agent 完成后，在主对话里追加一条汇报消息。
	assistant *AssistantService
	// wg 跟踪后台委派，便于测试等待。
	wg sync.WaitGroup
}

func NewAgentHub(agents *AgentService, assistant *AssistantService) *AgentHub {
	return &AgentHub{agents: agents, assistant: assistant}
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

// RunAgent 立即把任务交给子 Agent 并在后台等待。
// 主对话不用阻塞，子 Agent 完成后会在原会话追加一条汇报消息。
func (h *AgentHub) RunAgent(ctx context.Context, in tool.RunAgentInput) (string, error) {
	agentID := strings.TrimSpace(in.AgentID)
	task := strings.TrimSpace(in.Task)
	if agentID == "" {
		return "", errors.New("agentId 不能为空，请先调用 builtin_list_agents")
	}
	if task == "" {
		return "", errors.New("委派任务不能为空")
	}

	var target contract.AgentInfo
	known := false
	for _, item := range h.agents.Agents() {
		if item.ID == agentID {
			target = item
			known = true
			break
		}
	}
	if !known {
		return "", fmt.Errorf("Agent 不存在：%s，请先调用 builtin_list_agents 重新选择", agentID)
	}

	session := h.agents.CreateSession(agentID)
	if session.ID == "" {
		return "", errors.New("创建子会话失败")
	}
	if err := h.agents.RenameSession(session.ID, "委派："+truncateRunes(task, 20)); err != nil {
		// 标题只是可读性优化，失败不影响委派本身。
		h.agents.logger.Warn("重命名委派会话失败", "session", session.ID, "error", err)
	}

	// 子 Agent 以自己的「对用户的称呼」接收任务，避免把主助手的称呼带进去。
	address := strings.TrimSpace(parsePersonaText(target.Persona).UserName)
	taskText := "用户要求你：" + task
	if address != "" {
		taskText = address + "要求你：" + task
	}

	_, events, err := h.agents.Send(session.ID, taskText, "")
	if err != nil {
		return "", err
	}

	h.wg.Add(1)
	go h.watchDelegated(in.ConversationID, target.Name, task, session.ID, events)

	return fmt.Sprintf("已把任务交给「%s」。它完成后我会马上向你汇报，你可以继续安排别的事。", target.Name), nil
}

// watchDelegated 在后台消费子 Agent 的事件流，结束后向来源会话汇报。
func (h *AgentHub) watchDelegated(origin, agentName, task, sessionID string, events <-chan contract.ChatEvent) {
	defer h.wg.Done()

	var (
		result  string
		status  = contract.MessageFailed
		errText = "任务没有正常完成"
	)
	timer := time.NewTimer(delegationTimeout)
	defer timer.Stop()

	for {
		select {
		case event, ok := <-events:
			if !ok {
				h.report(origin, agentName, task, result, status, errText)
				return
			}
			if !event.Done {
				continue
			}
			result = event.Content
			status = event.Status
			errText = event.Error
		case <-timer.C:
			h.agents.Stop(sessionID)
			h.report(origin, agentName, task, "", contract.MessageFailed, "任务执行超时")
			return
		}
	}
}

// report 让主助手润色子 Agent 的结果，再追加到来源会话。
func (h *AgentHub) report(origin, agentName, task, result string, status contract.MessageStatus, errText string) {
	if h.assistant == nil || strings.TrimSpace(origin) == "" {
		return
	}

	taskBrief := taskTitle(task)
	failed := status != contract.MessageDone
	reason := strings.TrimSpace(errText)
	if failed && reason == "" {
		reason = "任务没有正常完成"
	}
	polished := h.polishReport(agentName, task, result, reason, failed)

	var content string
	if failed {
		content = fmt.Sprintf("你让「%s」做的「%s」任务没有完成，%s", agentName, taskBrief, polished)
	} else {
		content = fmt.Sprintf("你让「%s」做的「%s」任务完成了，%s", agentName, taskBrief, polished)
	}
	content = humanizeReport(content)
	if persona, err := loadPersona(context.Background(), h.agents.store); err == nil {
		if name := strings.TrimSpace(persona.UserName); name != "" {
			content = name + "，" + content
		}
	}

	if err := h.assistant.AppendReport(origin, content); err != nil {
		h.agents.logger.Warn("汇报子 Agent 结果失败", "origin", origin, "error", err)
	}
}

// polishReport 让主助手用自己的语气总结结果；调用失败时退回原始文本。
func (h *AgentHub) polishReport(agentName, task, result, reason string, failed bool) string {
	fallback := strings.TrimSpace(result)
	if failed {
		fallback = reason
	}
	if fallback == "" {
		fallback = "它没有返回具体内容。"
	}
	if !failed {
		truncated := truncateRunes(fallback, maxDelegatedChars)
		if truncated != fallback {
			truncated += "\n…（结果已截断，完整内容见该 Agent 的会话）"
		}
		fallback = truncated
	}
	if h.assistant.provider == nil {
		return fallback
	}

	detail := "任务结果：\n" + truncateRunes(fallback, 2000)
	if failed {
		detail = "任务没有完成，原因：" + reason
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	model := ""
	if models := h.assistant.Models(); len(models) > 0 {
		model = models[0].ID
	}
	reply, err := h.assistant.provider.Complete(ctx, llm.ChatRequest{
		System: h.assistant.systemPrompt(),
		Model:  model,
		Messages: []llm.Message{{
			Role: "user",
			Content: fmt.Sprintf("子 Agent「%s」刚执行完我交代的任务「%s」。%s\n请用 1-3 句话向对方汇报执行结果，只写结果与关键信息，不要重复任务描述，不要提到子 Agent，也不要使用「用户」这个词，直接用「你」指代对方。",
				agentName, truncateRunes(strings.TrimSpace(task), 40), detail),
		}},
	})
	reply = strings.TrimSpace(reply)
	if err != nil || reply == "" {
		if err != nil {
			h.agents.logger.Warn("润色委派汇报失败", "agent", agentName, "error", err)
		}
		return fallback
	}
	return reply
}

// taskTitle 从委派文本里取一个短标题：优先取第一句，
// 去掉结尾的「要求」等连接词，避免标题出现半截话。
func taskTitle(task string) string {
	task = strings.TrimSpace(task)
	if idx := strings.IndexAny(task, "。\n；;"); idx > 0 {
		task = strings.TrimSpace(task[:idx])
	}
	task = strings.TrimSpace(strings.TrimSuffix(task, "要求"))
	return truncateRunes(task, 24)
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

func matchesAgent(item contract.AgentInfo, keyword string) bool {
	haystack := strings.ToLower(item.Name + " " + item.Description + " " + item.Persona)
	return strings.Contains(haystack, keyword)
}
