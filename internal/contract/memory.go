package contract

// MemoryItem 是记忆管理界面展示的一条原子记忆。
type MemoryItem struct {
	ID         string `json:"id"`
	Scope      string `json:"scope"`
	OwnerID    string `json:"ownerId"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Keywords   string `json:"keywords"`
	DayKey     string `json:"dayKey"`
	OccurredAt int64  `json:"occurredAt"`
	Pinned     bool   `json:"pinned"`
}

// MemoryService 是记忆管理界面依赖的能力接口。
// ownerID 为空表示全局用户记忆；Agent 记忆的 ownerID 为 Agent ID。
type MemoryService interface {
	// Memories 返回指定作用域下的 active 记忆。
	Memories(scope, ownerID string) []MemoryItem
	SetPinned(id string, pinned bool) error
	UpdateMemory(id, content string) error
	DeleteMemory(id string) error
	ClearMemories() error

	// MemoryEnabled 查询全局或某个 Agent 是否允许长期记忆。
	MemoryEnabled(ownerID string) bool
	SetMemoryEnabled(ownerID string, enabled bool) error
}
