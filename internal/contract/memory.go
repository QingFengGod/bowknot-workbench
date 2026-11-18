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
// 用户只查看与整体清空，单条记忆的增删改由模型通过记忆工具完成。
// ownerID 为空表示全局用户记忆；Agent 记忆的 ownerID 为 Agent ID。
type MemoryService interface {
	// Memories 返回指定作用域下的 active 记忆。
	Memories(scope, ownerID string) []MemoryItem
	ClearMemories() error

	// MemoryEnabled 查询全局或某个 Agent 是否允许长期记忆。
	MemoryEnabled(ownerID string) bool
	SetMemoryEnabled(ownerID string, enabled bool) error
}
