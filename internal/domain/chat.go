// Package domain 定义后端共享的实体与值对象，不依赖 UI 或存储实现。
package domain

// ConversationKind 区分助手、Agent 与群聊会话。
type ConversationKind string

const (
	KindAssistant ConversationKind = "assistant"
	KindAgent     ConversationKind = "agent"
	KindGroup     ConversationKind = "group"
)

// Conversation 是一段独立会话的元信息。
type Conversation struct {
	ID        string
	Kind      ConversationKind
	OwnerID   string
	Title     string
	Summary   string
	Pinned    bool
	CreatedAt int64
	UpdatedAt int64
}

// Role 是消息发送方。
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// MessageStatus 是消息的生成状态。
type MessageStatus string

const (
	MessageStreaming MessageStatus = "streaming"
	MessageDone      MessageStatus = "done"
	MessageStopped   MessageStatus = "stopped"
	MessageFailed    MessageStatus = "failed"
)

// 发送者类型与消息类型：role 只有 user/assistant，身份与用途走独立列。
const (
	SenderUser  = "user"
	SenderMain  = "main"
	SenderAgent = "agent"

	MessageTypeChat         = "chat"
	MessageTypeTaskDispatch = "task_dispatch"
	MessageTypeTaskReport   = "task_report"

	VisibilityUser     = "user"
	VisibilityInternal = "internal"
)

// Message 是一条会话消息。
type Message struct {
	ID             string
	ConversationID string
	Seq            int64
	Role           Role
	Content        string
	// Reasoning 是模型返回的思维链，独立于正文保存。
	Reasoning     string
	Status        MessageStatus
	Error         string
	SenderName    string
	SenderAvatar  string
	Emotion       string
	MemoryWritten bool
	CreatedAt     int64
	// SenderKind / SenderID 标识发送者身份；MessageType 区分普通消息与任务消息。
	SenderKind  string
	SenderID    string
	MessageType string
	TaskID      string
	Visibility  string
}

// RunStatus 是一次生成任务的状态。
type RunStatus string

const (
	RunRunning RunStatus = "running"
	RunDone    RunStatus = "done"
	RunStopped RunStatus = "stopped"
	RunFailed  RunStatus = "failed"
)

// Run 记录一次生成任务，用于启动时收敛中断状态。
type Run struct {
	ID             string
	ConversationID string
	MessageID      string
	Status         RunStatus
	StartedAt      int64
	FinishedAt     int64
}

// ToolCallStatus 是一次工具调用的执行状态。
type ToolCallStatus string

const (
	ToolCallRunning  ToolCallStatus = "running"
	ToolCallOK       ToolCallStatus = "ok"
	ToolCallFailed   ToolCallStatus = "failed"
	ToolCallRejected ToolCallStatus = "rejected"
)

// ToolCall 是一次工具调用的持久化记录，与消息、Run 关联。
type ToolCall struct {
	ID             string
	ConversationID string
	MessageID      string
	RunID          string
	ToolID         string
	AgentID        string
	Arguments      string
	Result         string
	Status         ToolCallStatus
	Error          string
	CreatedAt      int64
	FinishedAt     int64
}
