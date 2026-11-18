package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/store"
)

// ToolExecutor 接管需要宿主真实执行的内置工具，其余工具交给插件进程执行器。
type ToolExecutor struct {
	store   *store.Store
	plugins *tool.ProcessExecutor
	// agents 在启动装配完成后注入，避免与 AgentService 构造形成循环依赖。
	agents tool.AgentHub
	// memory 在启动装配完成后注入；为 nil 时记忆工具一律拒绝。
	memory *MemoryService
	// artifacts 在启动装配完成后注入；为 nil 时产物工具一律拒绝。
	artifacts tool.ArtifactSink
}

func NewToolExecutor(st *store.Store, plugins *tool.ProcessExecutor) *ToolExecutor {
	return &ToolExecutor{store: st, plugins: plugins}
}

// SetAgentHub 注入 Agent 管理能力，由 main 在 AgentService 创建后调用。
func (e *ToolExecutor) SetAgentHub(hub tool.AgentHub) {
	e.agents = hub
}

// SetMemory 注入长期记忆服务，由 main 在 MemoryService 创建后调用。
func (e *ToolExecutor) SetMemory(memory *MemoryService) {
	e.memory = memory
}

// SetArtifacts 注入产物服务，由 main 在 ArtifactService 创建后调用。
func (e *ToolExecutor) SetArtifacts(artifacts tool.ArtifactSink) {
	e.artifacts = artifacts
}

func (e *ToolExecutor) Execute(ctx context.Context, call tool.Call) (tool.Result, error) {
	if tool.IsArtifactTool(call.Tool.ID) {
		return e.runArtifactTool(ctx, call)
	}
	if tool.IsMemoryTool(call.Tool.ID) {
		return e.runMemoryTool(ctx, call)
	}
	if call.Tool.ID == tool.PersonaToolID {
		return e.updatePersona(ctx, call.Arguments)
	}
	if tool.IsAgentHubTool(call.Tool.ID) {
		return e.runAgentHub(ctx, call)
	}
	if tool.IsTaskTool(call.Tool.ID) {
		return e.runTaskTool(ctx, call)
	}
	if e.plugins != nil {
		return e.plugins.Execute(ctx, call)
	}
	return tool.InProcess{}.Execute(ctx, call)
}

// runMemoryTool 分派写入、检索、修改、删除四个记忆工具。
// 作用域与归属由宿主按调用方 Agent 推导，记忆服务内部还会再校验一次所有权。
func (e *ToolExecutor) runMemoryTool(ctx context.Context, call tool.Call) (tool.Result, error) {
	if e.memory == nil {
		return tool.Result{}, fmt.Errorf("长期记忆未启用")
	}

	switch call.Tool.ID {
	case tool.RememberToolID:
		var in tool.RememberInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, err
		}
		item, err := e.memory.Remember(ctx, call.AgentID, call.ConversationID, tool.MessageID(ctx),
			in.Content, in.Kind, parseMemoryTime(in.OccurredAt))
		if err != nil {
			return tool.Result{}, err
		}
		return tool.Result{Content: fmt.Sprintf("已记住（ID：%s）：%s", item.ID, item.Content)}, nil

	case tool.SearchMemoryToolID:
		var in tool.SearchMemoryInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, err
		}
		items := e.memory.Search(ctx, call.AgentID, in.Query, in.From, in.To, in.Limit)
		if len(items) == 0 {
			return tool.Result{Content: "没有找到相关记忆。"}, nil
		}
		lines := make([]string, 0, len(items)+1)
		lines = append(lines, "找到以下记忆（ID｜日期｜类型｜内容）：")
		for _, item := range items {
			lines = append(lines, fmt.Sprintf("- %s｜%s｜%s｜%s", item.ID, item.DayKey, memoryKindText(item.Kind), item.Content))
		}
		return tool.Result{Content: strings.Join(lines, "\n")}, nil

	case tool.UpdateMemoryToolID:
		var in tool.UpdateMemoryInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, err
		}
		if err := e.memory.UpdateMemory(ctx, call.AgentID, in.ID, in.Content); err != nil {
			return tool.Result{}, err
		}
		return tool.Result{Content: fmt.Sprintf("已更新记忆（ID：%s）", in.ID)}, nil

	case tool.ForgetMemoryToolID:
		var in tool.ForgetMemoryInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, err
		}
		if err := e.memory.DeleteMemory(ctx, call.AgentID, in.ID); err != nil {
			return tool.Result{}, err
		}
		return tool.Result{Content: fmt.Sprintf("已删除记忆（ID：%s）", in.ID)}, nil

	default:
		return tool.Result{}, fmt.Errorf("未实现的记忆工具：%s", call.Tool.ID)
	}
}

// memoryKindText 把记忆类型转成便于对话展示的中文。
func memoryKindText(kind string) string {
	switch kind {
	case "profile":
		return "背景"
	case "preference":
		return "偏好"
	case "fact":
		return "事实"
	case "decision":
		return "决策"
	case "event":
		return "事件"
	default:
		return "记忆"
	}
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

// runArtifactTool 保存可复用交付物，并返回便于模型引用的结果。
func (e *ToolExecutor) runArtifactTool(ctx context.Context, call tool.Call) (tool.Result, error) {
	if e.artifacts == nil {
		return tool.Result{}, fmt.Errorf("产物服务未启用")
	}

	var in tool.ArtifactInput
	if err := decodeArguments(call.Arguments, &in); err != nil {
		return tool.Result{}, err
	}
	ref, err := e.artifacts.SaveArtifact(ctx, call.ConversationID, in)
	if err != nil {
		return tool.Result{}, err
	}

	size := fmt.Sprintf("%d B", ref.Size)
	if ref.Size >= 1024 {
		size = fmt.Sprintf("%.1f KB", float64(ref.Size)/1024)
	}
	content := fmt.Sprintf("已保存产物「%s」（ID：%s，类型：%s，大小：%s）。", ref.Title, ref.ID, ref.Kind, size)
	if ref.Filename != "" {
		content = fmt.Sprintf("已保存产物「%s」（ID：%s，文件：%s，大小：%s）。", ref.Title, ref.ID, ref.Filename, size)
	}
	return tool.Result{Content: content}, nil
}

// runTaskTool 分派查询与取消任务两个内置工具。
func (e *ToolExecutor) runTaskTool(ctx context.Context, call tool.Call) (tool.Result, error) {
	if e.agents == nil {
		return tool.Result{}, fmt.Errorf("任务工具未启用")
	}

	switch call.Tool.ID {
	case tool.ListTasksToolID:
		var in tool.ListTasksInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, err
		}
		refs, err := e.agents.ListTasks(ctx, in)
		if err != nil {
			return tool.Result{}, err
		}
		if len(refs) == 0 {
			return tool.Result{Content: "没有符合条件的任务。"}, nil
		}
		lines := make([]string, 0, len(refs)+1)
		lines = append(lines, "任务列表（ID｜员工｜状态｜标题）：")
		for _, ref := range refs {
			line := fmt.Sprintf("- %s｜%s｜%s｜%s", ref.ID, ref.AssigneeName, taskStatusText(ref.Status), ref.Title)
			if summary := strings.TrimSpace(ref.ResultSummary); summary != "" {
				line += "｜结果：" + truncateRunes(summary, 80)
			}
			if ref.Error != "" && !strings.Contains(ref.Error, "已取消") {
				line += "（" + ref.Error + "）"
			}
			lines = append(lines, line)
		}
		return tool.Result{Content: strings.Join(lines, "\n")}, nil

	case tool.CancelTaskToolID:
		var in struct {
			TaskID string `json:"taskId"`
		}
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, err
		}
		content, err := e.agents.CancelTask(ctx, in.TaskID)
		if err != nil {
			return tool.Result{}, err
		}
		return tool.Result{Content: content}, nil

	default:
		return tool.Result{}, fmt.Errorf("未实现的任务工具：%s", call.Tool.ID)
	}
}

// taskStatusText 把任务状态转成便于对话展示的中文。
func taskStatusText(status string) string {
	return taskStatusLabel(domain.TaskStatus(status))
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
