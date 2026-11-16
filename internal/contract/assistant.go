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

// ChatToolCall 是一条已落库的工具调用记录，用于重启后回看。
type ChatToolCall struct {
	ID        string `json:"id"`
	ToolID    string `json:"toolId"`
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
	Status    string `json:"status"`
	Error     string `json:"error"`
}

// ChatMessage 是一条会话消息。
type ChatMessage struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationId"`
	// Seq 是消息在会话内的递增序号，分页时作为游标。
	Seq       int64          `json:"seq"`
	Role      ChatRole       `json:"role"`
	Content   string         `json:"content"`
	Status    MessageStatus  `json:"status"`
	CreatedAt int64          `json:"createdAt"`
	ToolCalls []ChatToolCall `json:"toolCalls,omitempty"`
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

// DefaultMessagePageSize 是消息分页的默认页大小。
const DefaultMessagePageSize = 50

// MessagePage 是一页按 seq 升序排列的消息。
type MessagePage struct {
	Items   []ChatMessage `json:"items"`
	HasMore bool          `json:"hasMore"`
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

// ChatEventType 标记流式事件类型；空值等价于普通文本事件。
type ChatEventType string

const (
	ChatEventText       ChatEventType = "text"
	ChatEventToolCall   ChatEventType = "tool_call"
	ChatEventToolResult ChatEventType = "tool_result"
	ChatEventApproval   ChatEventType = "approval"
	ChatEventDone       ChatEventType = "done"
)

// ChatEvent 是流式回复的一次快照。
// Content 是当前完整内容，UI 直接覆盖最后一条消息，避免增量重复。
type ChatEvent struct {
	Type      ChatEventType
	RunID     string
	MessageID string
	Content   string
	Status    MessageStatus
	Error     string
	Done      bool

	// 以下字段只在工具调用相关事件里使用。
	ToolCallID string
	ToolName   string
	Arguments  string
	Result     string
	Dangerous  bool
}

// ApprovalService 由支持工具审批的实现提供。
// UI 收到审批事件后调用 ResolveApproval 回传用户选择。
type ApprovalService interface {
	ResolveApproval(runID, callID string, approved bool)
}

// AssistantPersona 是主助手的人设配置，所有字段都可为空，Name 为空时按默认人设处理。
type AssistantPersona struct {
	Name        string `json:"name"`
	Gender      string `json:"gender"`
	Age         string `json:"age"`
	Personality string `json:"personality"`
	Role        string `json:"role"`
	// UserName 是助手对用户的称呼。
	UserName string `json:"userName"`
	// Custom 是其它要求，用于补充不属于以上字段的设定。
	Custom string `json:"custom"`
}

// DefaultAssistantPersona 返回未配置时使用的默认人设。
func DefaultAssistantPersona() AssistantPersona {
	return AssistantPersona{
		Name:        "Bowknot",
		Personality: "温柔、耐心、好奇",
		Role:        "你的小助手",
	}
}

// AssistantService 是助手页依赖的能力接口，由后端实现。
type AssistantService interface {
	Sessions() []ChatSession
	Messages(sessionID string) []ChatMessage
	// MessagesPage 读取一页消息：beforeSeq <= 0 从最新开始，limit <= 0 用默认页大小。
	MessagesPage(sessionID string, beforeSeq int64, limit int) MessagePage
	Models() []ChatModel
	// Persona 返回主助手的人设，未配置时返回默认人设。
	Persona() AssistantPersona
	// SetPersona 保存主助手的人设。
	SetPersona(persona AssistantPersona) error
	// Greeting 返回进入空对话时的主动问候：首次使用自我介绍，之后按最近记忆关心近况。
	Greeting() string
	// GreetOnLaunch 在每次应用启动后的首次进入时创建默认会话并流式输出问候；
	// 只触发一次，手动新建会话不会再次问候。
	GreetOnLaunch() (sessionID, messageID string, events <-chan ChatEvent, started bool)

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
