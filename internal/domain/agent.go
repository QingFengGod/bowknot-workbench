package domain

// Agent 是用户创建的人设 Agent。
type Agent struct {
	ID          string
	Name        string
	Description string
	AvatarText  string
	Persona     string
	ModelID     string
	Pinned      bool
	CreatedAt   int64
	UpdatedAt   int64
}

// Group 是多 Agent 群聊，成员不含主 Agent。
type Group struct {
	ID        string
	Name      string
	MemberIDs []string
	Pinned    bool
	CreatedAt int64
}
