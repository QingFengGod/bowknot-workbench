// Package tool 统一应用与插件工具的注册、授权校验与执行外壳。
// 本阶段只提供 in-process 占位执行器，真实工具执行与进程级沙箱在后续阶段接入。
package tool

import (
	"context"
	"errors"
	"fmt"

	"bowknot-workbench/internal/domain"
)

// 工具来源类型。
const (
	SourceApp     = "app"
	SourcePlugin  = "plugin"
	SourceBuiltin = "builtin"
)

// TransportInProcess 是首版执行通道；后续可扩展子进程或独立沙箱。
const TransportInProcess = "in-process"

// PersonaToolID 是更新助手人设的内置工具。名字避开冒号与斜杠，
// 保证符合模型供应商对函数名的字符限制。
const PersonaToolID = "builtin_update_persona"

// MainAssistantAgentID 是内置助手使用的 Agent ID，与 app 层保持一致。
const MainAssistantAgentID = "main-agent"

// ToolSpec 是统一后的工具描述，ID 形如 app:<appID>/<name> 或 plugin:<pluginID>/<name>。
type ToolSpec struct {
	ID          string
	Name        string
	Description string
	Dangerous   bool
	Source      string // app / plugin / builtin
	SourceID    string
	// Parameters 是可选的 JSON Schema 参数声明；为空时使用自由对象。
	Parameters string
}

// PersonaToolSpec 声明内置的人设更新工具，参数只传需要修改的字段。
func PersonaToolSpec() ToolSpec {
	return ToolSpec{
		ID:          PersonaToolID,
		Name:        PersonaToolID,
		Description: "当用户要求修改你的姓名、性别、年龄、性格、角色职能、对用户的称呼或其它要求时调用。参数是 JSON 对象，可包含 name、gender、age、personality、role、userName、custom 字段，只传需要修改的字段。",
		Source:      SourceBuiltin,
		SourceID:    "builtin",
		Parameters: `{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "助手姓名"},
    "gender": {"type": "string", "description": "助手性别"},
    "age": {"type": "string", "description": "助手年龄"},
    "personality": {"type": "string", "description": "助手性格"},
    "role": {"type": "string", "description": "角色或职能"},
    "userName": {"type": "string", "description": "助手对用户的称呼"},
    "custom": {"type": "string", "description": "其它要求"}
  }
}`,
	}
}

// Call 是一次工具调用。
type Call struct {
	Tool ToolSpec
	// AgentID 是发起调用的 Agent，用于插件授权校验。
	AgentID string
	// Arguments 是 JSON 编码的调用参数，可为空。
	Arguments string
	// Transport 标记执行通道，由 Service 按配置注入。
	Transport string
}

// Result 是工具执行输出。
type Result struct {
	Content string
}

// Executor 是工具执行器接口。
type Executor interface {
	Execute(ctx context.Context, call Call) (Result, error)
}

// Approver 是危险操作的确认入口，实现方负责等待 UI 确认；
// 返回 false 或 ctx 取消均视为拒绝。为 nil 时危险工具一律拒绝。
type Approver func(ctx context.Context, call Call) bool

// 工具调用被拒绝的原因分类，上层按 errors.Is 映射文案。
var (
	// ErrNotFound 表示注册表或存储中不存在该工具。
	ErrNotFound = errors.New("tool not found")
	// ErrDisabled 表示应用工具的全局开关处于关闭状态。
	ErrDisabled = errors.New("tool disabled")
	// ErrNotAuthorized 表示 Agent 未获得插件工具的授权。
	ErrNotAuthorized = errors.New("agent not authorized")
	// ErrCanceled 表示用户拒绝确认或调用被取消。
	ErrCanceled = errors.New("canceled")
)

// AppSpec 把应用工具映射为统一 ToolSpec。
func AppSpec(appID string, t domain.Tool) ToolSpec {
	return ToolSpec{
		ID:          fmt.Sprintf("app:%s/%s", appID, t.Name),
		Name:        t.Name,
		Description: t.Description,
		Dangerous:   t.Dangerous,
		Source:      SourceApp,
		SourceID:    appID,
	}
}

// PluginSpec 把插件工具映射为统一 ToolSpec。
func PluginSpec(pluginID string, t domain.Tool) ToolSpec {
	return ToolSpec{
		ID:          fmt.Sprintf("plugin:%s/%s", pluginID, t.Name),
		Name:        t.Name,
		Description: t.Description,
		Dangerous:   t.Dangerous,
		Source:      SourcePlugin,
		SourceID:    pluginID,
	}
}
