package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/store"
)

// ToolExecutor 接管需要宿主真实执行的内置工具，其余工具交给插件进程执行器。
type ToolExecutor struct {
	store   *store.Store
	plugins *tool.ProcessExecutor
	// agents 在启动装配完成后注入，避免与 AgentService 构造形成循环依赖。
	agents tool.AgentHub
}

func NewToolExecutor(st *store.Store, plugins *tool.ProcessExecutor) *ToolExecutor {
	return &ToolExecutor{store: st, plugins: plugins}
}

// SetAgentHub 注入 Agent 管理能力，由 main 在 AgentService 创建后调用。
func (e *ToolExecutor) SetAgentHub(hub tool.AgentHub) {
	e.agents = hub
}

func (e *ToolExecutor) Execute(ctx context.Context, call tool.Call) (tool.Result, error) {
	if call.Tool.ID == tool.PersonaToolID {
		return e.updatePersona(ctx, call.Arguments)
	}
	if tool.IsAgentHubTool(call.Tool.ID) {
		return e.runAgentHub(ctx, call)
	}
	if e.plugins != nil {
		return e.plugins.Execute(ctx, call)
	}
	return tool.InProcess{}.Execute(ctx, call)
}

// runAgentHub 分派创建 / 列举 / 委派三个内置工具。
func (e *ToolExecutor) runAgentHub(ctx context.Context, call tool.Call) (tool.Result, error) {
	if e.agents == nil {
		return tool.Result{}, fmt.Errorf("Agent 工具未启用")
	}

	switch call.Tool.ID {
	case tool.CreateAgentToolID:
		var in tool.CreateAgentInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, err
		}
		ref, err := e.agents.CreateAgent(ctx, in)
		if err != nil {
			return tool.Result{}, err
		}
		return tool.Result{Content: fmt.Sprintf("已创建 Agent「%s」（ID：%s）。现在可以把任务交给它了。", ref.Name, ref.ID)}, nil

	case tool.ListAgentsToolID:
		var in struct {
			Query string `json:"query"`
		}
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, err
		}
		refs, err := e.agents.ListAgents(ctx, in.Query)
		if err != nil {
			return tool.Result{}, err
		}
		if len(refs) == 0 {
			return tool.Result{Content: "当前没有可用的 Agent。如果用户需要，可以先调用 builtin_create_agent 创建一个。"}, nil
		}
		lines := make([]string, 0, len(refs)+1)
		lines = append(lines, "可用 Agent（格式：ID｜名称｜擅长）：")
		for _, ref := range refs {
			description := strings.TrimSpace(ref.Description)
			if description == "" {
				description = truncateRunes(ref.Persona, 40)
			}
			lines = append(lines, fmt.Sprintf("- %s｜%s｜%s", ref.ID, ref.Name, description))
		}
		return tool.Result{Content: strings.Join(lines, "\n")}, nil

	case tool.RunAgentToolID:
		var in tool.RunAgentInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, err
		}
		// 来源会话只信任宿主传递，不采用模型参数。
		in.ConversationID = call.ConversationID
		content, err := e.agents.RunAgent(ctx, in)
		if err != nil {
			return tool.Result{}, err
		}
		return tool.Result{Content: content}, nil

	default:
		return tool.Result{}, fmt.Errorf("未实现的 Agent 工具：%s", call.Tool.ID)
	}
}

// decodeArguments 解析工具参数，空文本按空对象处理。
func decodeArguments(raw string, target any) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("工具参数解析失败：%w", err)
	}
	return nil
}

// updatePersona 把人设工具的参数合并进当前人设，空字段保持原值。
func (e *ToolExecutor) updatePersona(ctx context.Context, arguments string) (tool.Result, error) {
	current, err := loadPersona(ctx, e.store)
	if err != nil {
		return tool.Result{}, err
	}

	var patch personaPatch
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &patch); err != nil {
			return tool.Result{}, fmt.Errorf("人设参数解析失败：%w", err)
		}
	}

	next := applyPersonaPatch(current, patch)
	if err := savePersona(ctx, e.store, next); err != nil {
		return tool.Result{}, err
	}
	return tool.Result{Content: "已更新助手人设：" + summarizePersona(next)}, nil
}

// summarizePersona 生成便于在对话里展示的一行人设摘要。
func summarizePersona(p contract.AssistantPersona) string {
	parts := make([]string, 0, 6)
	add := func(label, value string) {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, label+"「"+value+"」")
		}
	}
	add("姓名", p.Name)
	add("性别", p.Gender)
	add("年龄", p.Age)
	add("性格", p.Personality)
	add("角色", p.Role)
	add("对用户的称呼", p.UserName)
	add("其它要求", p.Custom)
	if len(parts) == 0 {
		return "暂无内容"
	}
	return strings.Join(parts, "，")
}
