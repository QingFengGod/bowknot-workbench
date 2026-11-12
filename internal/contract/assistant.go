package contract

// ChatRole 是消息的发送方。
type ChatRole string

const (
	RoleUser      ChatRole = "user"
	RoleAssistant ChatRole = "assistant"
)

// MessageStatus 是消息的生成状态。
type MessageStatus string

const (
	MessageStreaming MessageStatus = "streaming"
	MessageDone      MessageStatus = "done"
	MessageStopped   MessageStatus = "stopped"
	MessageFailed    MessageStatus = "failed"
)

// ChatMessage 是一条会话消息。
type ChatMessage struct {
	ID             string        `json:"id"`
	ConversationID string        `json:"conversationId"`
	Role           ChatRole      `json:"role"`
	Content        string        `json:"content"`
	Status         MessageStatus `json:"status"`
	CreatedAt      int64         `json:"createdAt"`
	// Error 只在生成失败时展示。
	Error string `json:"error"`
	// MemoryWritten 标记该回复已写入长期记忆。
	MemoryWritten bool `json:"memoryWritten"`
	// Emotion 是助手回复携带的情绪标记，空字符串表示没有。
	Emotion string `json:"emotion"`
	// SenderName 与 SenderAvatar 只在群聊回复里设置，标识发言人。
	SenderName   string `json:"senderName,omitempty"`
	SenderAvatar string `json:"senderAvatar,omitempty"`
}

// ChatSession 是一个助手会话。
type ChatSession struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	// Pinned 标记置顶会话。
	Pinned    bool  `json:"pinned"`
	UpdatedAt int64 `json:"updatedAt"`
}

// ChatModel 是可选模型。
type ChatModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ChatEvent 是流式回复的一次快照。
// Content 是当前完整内容，UI 直接覆盖最后一条消息，避免增量重复。
type ChatEvent struct {
	MessageID string
	Content   string
	Status    MessageStatus
	Error     string
	Done      bool
}

// AssistantService 是助手页依赖的能力接口，由后端实现。
type AssistantService interface {
	Sessions() []ChatSession
	Messages(sessionID string) []ChatMessage
	Models() []ChatModel

	CreateSession() ChatSession
	RenameSession(id, title string) error
	DeleteSession(id string) error
	PinSession(id string, pinned bool) error

	// Send 追加用户消息并开始生成回复，事件通道按顺序给出流式快照。
	Send(sessionID, text, modelID string) (messageID string, events <-chan ChatEvent, err error)
	// AutoReply 在会话还没有助手回复时开始一次演示回复，已播放过则不再重复。
	AutoReply(sessionID string) (messageID string, events <-chan ChatEvent, started bool)
	// Regenerate 重新生成指定助手消息，保留消息在列表中的位置。
	Regenerate(sessionID, messageID string) (events <-chan ChatEvent, err error)
	// DeleteMessage 删除一条消息；正在生成的消息会先停止。
	DeleteMessage(sessionID, messageID string) error
	// Stop 停止指定会话的生成，保留已生成内容。
	Stop(sessionID string)
}
