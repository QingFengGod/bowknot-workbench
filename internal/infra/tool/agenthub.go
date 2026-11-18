package tool

import (
	"context"
)

// 内置 Agent 工具的 ID，与模型函数名保持一致，避免冒号与斜杠。
const (
	CreateAgentToolID = "builtin_create_agent"
	ListAgentsToolID  = "builtin_list_agents"
	RunAgentToolID    = "builtin_run_agent"
)

var agentHubToolIDs = map[string]bool{
	CreateAgentToolID: true,
	ListAgentsToolID:  true,
	RunAgentToolID:    true,
}

// IsAgentHubTool 判断工具是否属于 Agent 管理能力。
// 这些工具只开放给主助手，避免 Agent 之间互相委派形成递归。
func IsAgentHubTool(id string) bool {
	return agentHubToolIDs[id]
}

// CreateAgentInput 是创建一个专业 Agent 的参数，字段与主助手人设保持一致。
type CreateAgentInput struct {
	Name        string `json:"name"`
	Gender      string `json:"gender"`
	Age         string `json:"age"`
	Personality string `json:"personality"`
	Role        string `json:"role"`
	UserName    string `json:"userName"`
	Custom      string `json:"custom"`
}

// AgentRef 是委派与列举时使用的 Agent 摘要。
type AgentRef struct {
	ID          string
	Name        string
	Description string
	Persona     string
}

// RunAgentInput 是一次任务委派。
type RunAgentInput struct {
	AgentID string `json:"agentId"`
	Task    string `json:"task"`
	// ConversationID 是发起委派的主对话，由宿主注入，不接受模型传值。
	ConversationID string `json:"-"`
}

// AgentHub 由 app 层实现，负责真正的 Agent 读写与任务派发。
// 定义在 infra 侧是为了避免工具层反向依赖业务层。
type AgentHub interface {
	CreateAgent(ctx context.Context, in CreateAgentInput) (AgentRef, error)
	ListAgents(ctx context.Context, query string) ([]AgentRef, error)
	RunAgent(ctx context.Context, in RunAgentInput) (string, error)
	ListTasks(ctx context.Context, in ListTasksInput) ([]TaskRef, error)
	CancelTask(ctx context.Context, taskID string) (string, error)
}

// AgentHubToolSpecs 声明内置的 Agent 管理与派发工具。
func AgentHubToolSpecs() []ToolSpec {
	return []ToolSpec{
		{
			ID:          CreateAgentToolID,
			Name:        CreateAgentToolID,
			Description: "当用户明确要求创建一个新的专业 Agent（例如「帮我创建一个写文案的 agent」）时调用。人设字段包括姓名、性别、年龄、性格、角色或职责、对用户的称呼、其它要求；role 同时作为一句话简介。各字段要描述这个 Agent 自己的身份与工作方式，不要复述主助手的人设或身份。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "Agent 姓名，简短明确"},
    "gender": {"type": "string", "description": "性别"},
    "age": {"type": "string", "description": "年龄"},
    "personality": {"type": "string", "description": "性格"},
    "role": {"type": "string", "description": "角色或职责，同时作为一句话简介"},
    "userName": {"type": "string", "description": "该 Agent 对用户的称呼"},
    "custom": {"type": "string", "description": "其它要求，例如表达习惯或行为边界"}
  },
  "required": ["name", "role"]
}`,
		},
		{
			ID:          ListAgentsToolID,
			Name:        ListAgentsToolID,
			Description: "列出当前可用的专业 Agent 及其擅长领域。当用户的任务需要特定专业能力时，先调用本工具挑选最合适的 Agent，再用 builtin_run_agent 委派。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "可选的关键词，用于按名称或擅长领域过滤"}
  }
}`,
		},
		{
			ID:          RunAgentToolID,
			Name:        RunAgentToolID,
			Description: "把任务交给一个专业 Agent 后台执行，调用后立即返回任务 ID，完成时会另行汇报。调用前应先用 builtin_list_agents 找到合适的 agentId。task 只写任务本身，不要写称呼、身份设定或「称呼用户为某某」这类指示。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "agentId": {"type": "string", "description": "目标 Agent 的 ID，来自 builtin_list_agents"},
    "task": {"type": "string", "description": "要交给该 Agent 完成的完整任务描述"}
  },
  "required": ["agentId", "task"]
}`,
		},
	}
}
