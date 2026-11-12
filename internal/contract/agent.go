package contract

// AgentInfo 是一个动态 Agent 的人设信息。
type AgentInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// AvatarText 是头像展示文字，创建时默认取名称首字。
	AvatarText string `json:"avatarText"`
	// Persona 是系统人设提示词。
	Persona   string `json:"persona"`
	ModelName string `json:"modelName"`
	Pinned    bool   `json:"pinned"`
}

// AgentGroup 是一个多 Agent 群聊，成员不含主 Agent。
type AgentGroup struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	MemberIDs []string `json:"memberIds"`
	Pinned    bool     `json:"pinned"`
}

// AgentSession 是动态 Agent 独立的会话；群聊没有会话列表。
type AgentSession struct {
	ID        string `json:"id"`
	AgentID   string `json:"agentId"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	Pinned    bool   `json:"pinned"`
	UpdatedAt int64  `json:"updatedAt"`
}

// ItemKind 区分置顶操作的对象类型。
const (
	ItemKindAgent = "agent"
	ItemKindGroup = "group"
)

// AgentService 是 agents 页依赖的能力接口，由后端实现。
// 群聊的会话标识固定为 "group:" + 群 ID，与 Agent 会话 ID 共用消息存储。
type AgentService interface {
	Agents() []AgentInfo
	Groups() []AgentGroup
	Sessions(agentID string) []AgentSession
	Messages(conversationID string) []ChatMessage
	Models() []ChatModel

	CreateAgent(name, description, persona string) AgentInfo
	UpdateAgent(id, name, description, persona string) error
	RemoveAgent(id string) error

	CreateGroup(name string, memberIDs []string) AgentGroup
	UpdateGroup(id, name string, memberIDs []string) error
	RemoveGroup(id string) error

	// Pin 置顶或取消置顶一个 Agent 或群聊。
	Pin(kind, id string, pinned bool) error

	CreateSession(agentID string) AgentSession
	RenameSession(id, title string) error
	DeleteSession(id string) error
	PinSession(id string, pinned bool) error

	// Send 追加用户消息并开始生成回复；群聊回复带发送者信息。
	Send(conversationID, text, modelID string) (messageID string, events <-chan ChatEvent, err error)
	// Regenerate 重新生成指定助手消息，仅演示剧本覆盖的会话支持。
	Regenerate(conversationID, messageID string) (events <-chan ChatEvent, err error)
	// DeleteMessage 删除一条消息；正在生成的消息会先停止。
	DeleteMessage(conversationID, messageID string) error
	// Stop 停止指定会话的生成，保留已生成内容。
	Stop(conversationID string)
}
