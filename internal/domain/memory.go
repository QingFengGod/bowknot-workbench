package domain

// MemoryScope 区分全局用户记忆与 Agent 专属记忆。
type MemoryScope string

const (
	MemoryScopeUser  MemoryScope = "user"
	MemoryScopeAgent MemoryScope = "agent"
)

// MemoryKind 是记忆的分类。
type MemoryKind string

const (
	MemoryKindProfile    MemoryKind = "profile"
	MemoryKindPreference MemoryKind = "preference"
	MemoryKindFact       MemoryKind = "fact"
	MemoryKindDecision   MemoryKind = "decision"
	MemoryKindEvent      MemoryKind = "event"
)

// MemoryStatus 是记忆的生命周期状态。
type MemoryStatus string

const (
	MemoryActive     MemoryStatus = "active"
	MemorySuperseded MemoryStatus = "superseded"
	MemoryArchived   MemoryStatus = "archived"
)

// Memory 是一条原子长期记忆。
type Memory struct {
	ID                   string
	Scope                MemoryScope
	OwnerID              string
	Kind                 MemoryKind
	Content              string
	ContentHash          string
	Keywords             string
	OccurredAt           int64
	DayKey               string
	SourceConversationID string
	SourceMessageID      string
	SourceRole           string
	Confidence           float64
	Importance           int
	Pinned               bool
	Status               MemoryStatus
	SupersedesID         string
	CreatedAt            int64
	UpdatedAt            int64
	LastUsedAt           int64
}

// DigestLevel 是摘要的聚合粒度。
type DigestLevel string

const (
	DigestDay   DigestLevel = "day"
	DigestWeek  DigestLevel = "week"
	DigestMonth DigestLevel = "month"
)

// MemoryDigest 是某个周期内原子记忆的摘要索引。
type MemoryDigest struct {
	ID            string
	Scope         MemoryScope
	OwnerID       string
	Level         DigestLevel
	PeriodStart   string
	PeriodEnd     string
	Summary       string
	Keywords      string
	MemoryCount   int
	SourceVersion string
	Dirty         bool
	CreatedAt     int64
	UpdatedAt     int64
}
