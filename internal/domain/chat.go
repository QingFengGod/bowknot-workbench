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

// Message 是一条会话消息。
type Message struct {
	ID             string
	ConversationID string
	Seq            int64
	Role           Role
	Content        string
	Status         MessageStatus
	Error          string
	SenderName     string
	SenderAvatar   string
	Emotion        string
	MemoryWritten  bool
	CreatedAt      int64
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
