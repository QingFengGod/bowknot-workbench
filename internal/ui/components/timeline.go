package components

import (
	"strings"

	"bowknot-workbench/internal/contract"
)

// AgentStepKind 是 Agent 消息时间线里的步骤类型。
type AgentStepKind string

const (
	// AgentStepThinking 是一次思维链片段。
	AgentStepThinking AgentStepKind = "thinking"
	// AgentStepTool 是一次工具调用。
	AgentStepTool AgentStepKind = "tool"
)

// AgentStep 是时间线上的一个步骤：思维链片段或工具调用。
type AgentStep struct {
	Kind AgentStepKind
	Text string
	Tool contract.ChatToolCall
}

// Timeline 按消息 ID 保存思考与工具步骤。
// 思维链只在进程内保留、不落库，因此页面切换或消息重新加载后仍可继续显示。
// 这是视图状态容器，不参与渲染，页面持有它即可。
type Timeline struct {
	steps        map[string][]AgentStep
	open         map[string][]*bool
	toolOpen     map[string][]*bool
	toolsOpen    map[string]*bool
	thinkingOpen map[string]*bool
}

func NewTimeline() *Timeline {
	return &Timeline{
		steps:        map[string][]AgentStep{},
		open:         map[string][]*bool{},
		toolOpen:     map[string][]*bool{},
		toolsOpen:    map[string]*bool{},
		thinkingOpen: map[string]*bool{},
	}
}

// Steps 返回消息当前的步骤快照。
func (t *Timeline) Steps(messageID string) []AgentStep {
	if t == nil || messageID == "" {
		return nil
	}
	return t.steps[messageID]
}

// StepOpen 返回第 index 个步骤的折叠状态指针，供每帧复用同一地址。
func (t *Timeline) StepOpen(messageID string, index int) *bool {
	if t == nil {
		return nil
	}
	opens := t.open[messageID]
	if index < 0 || index >= len(opens) {
		return nil
	}
	return opens[index]
}

// ToolOpen 返回历史消息里第 index 个工具调用的折叠状态，默认收起。
// 指针按需创建，保证同一调用每帧拿到同一地址。
func (t *Timeline) ToolOpen(messageID string, index int) *bool {
	if t == nil || messageID == "" || index < 0 {
		return nil
	}
	opens := t.toolOpen[messageID]
	for len(opens) <= index {
		opens = append(opens, newBool(false))
	}
	t.toolOpen[messageID] = opens
	return opens[index]
}

// ThinkingOpen 返回历史消息思维链块的折叠状态，默认收起。
func (t *Timeline) ThinkingOpen(messageID string) *bool {
	if t == nil || messageID == "" {
		return nil
	}
	if pointer, ok := t.thinkingOpen[messageID]; ok {
		return pointer
	}
	pointer := newBool(false)
	t.thinkingOpen[messageID] = pointer
	return pointer
}

// ToolsOpen 返回历史消息里工具调用分组的折叠状态，默认收起。
func (t *Timeline) ToolsOpen(messageID string) *bool {
	if t == nil || messageID == "" {
		return nil
	}
	if pointer, ok := t.toolsOpen[messageID]; ok {
		return pointer
	}
	pointer := newBool(false)
	t.toolsOpen[messageID] = pointer
	return pointer
}

// ApplyReasoning 用累计快照更新思维链：连续片段合并为同一块，中断后另起一块。
func (t *Timeline) ApplyReasoning(messageID, text string) {
	if t == nil || messageID == "" {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	steps := t.steps[messageID]
	if len(steps) > 0 && steps[len(steps)-1].Kind == AgentStepThinking {
		steps[len(steps)-1].Text = text
		t.steps[messageID] = steps
		return
	}

	steps = append(steps, AgentStep{Kind: AgentStepThinking, Text: text})
	t.steps[messageID] = steps
	t.open[messageID] = append(t.open[messageID], newBool(true))
}

// ApplyToolCall 记录一次工具调用开始；同一调用重复到达时只更新参数。
func (t *Timeline) ApplyToolCall(ev contract.ChatEvent) {
	if t == nil || ev.MessageID == "" {
		return
	}

	steps := t.steps[ev.MessageID]
	for i := range steps {
		if steps[i].Kind != AgentStepTool || steps[i].Tool.ID != ev.ToolCallID {
			continue
		}
		steps[i].Tool.ToolID = firstNonEmpty(ev.ToolName, steps[i].Tool.ToolID)
		steps[i].Tool.Arguments = ev.Arguments
		steps[i].Tool.Status = firstNonEmpty(ev.ToolStatus, "running")
		t.steps[ev.MessageID] = steps
		return
	}

	steps = append(steps, AgentStep{
		Kind: AgentStepTool,
		Tool: contract.ChatToolCall{
			ID:        ev.ToolCallID,
			ToolID:    ev.ToolName,
			Arguments: ev.Arguments,
			Status:    firstNonEmpty(ev.ToolStatus, "running"),
		},
	})
	t.steps[ev.MessageID] = steps
	t.open[ev.MessageID] = append(t.open[ev.MessageID], newBool(false))
}

// ApplyToolResult 回填工具执行结果与最终状态。
func (t *Timeline) ApplyToolResult(ev contract.ChatEvent) {
	if t == nil || ev.MessageID == "" {
		return
	}

	steps := t.steps[ev.MessageID]
	for i := range steps {
		if steps[i].Kind != AgentStepTool || steps[i].Tool.ID != ev.ToolCallID {
			continue
		}
		steps[i].Tool.Result = ev.Result
		steps[i].Tool.Error = ev.Error
		steps[i].Tool.Status = firstNonEmpty(ev.ToolStatus, "ok")
		t.steps[ev.MessageID] = steps
		return
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func newBool(value bool) *bool {
	return &value
}
