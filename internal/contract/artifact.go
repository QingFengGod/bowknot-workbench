package contract

// ArtifactKind 是产物的预览类型。
type ArtifactKind string

const (
	ArtifactMarkdown ArtifactKind = "markdown"
	ArtifactText     ArtifactKind = "text"
	ArtifactCode     ArtifactKind = "code"
	ArtifactHTML     ArtifactKind = "html"
	ArtifactImage    ArtifactKind = "image"
	ArtifactLink     ArtifactKind = "link"
	// ArtifactFile 是无法原生预览的兜底类型，交给系统应用打开。
	ArtifactFile ArtifactKind = "file"
)

// ArtifactBrief 是消息与任务卡片使用的产物摘要。
type ArtifactBrief struct {
	ID    string       `json:"id"`
	Title string       `json:"title"`
	Kind  ArtifactKind `json:"kind"`
	MIME  string       `json:"mime,omitempty"`
	// Filename 是落盘文件名，正文里的文件引用按它匹配。
	Filename  string `json:"filename,omitempty"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"createdAt"`
	// Available 为 false 表示文件已被移动或删除。
	Available bool `json:"available"`
}

// ArtifactDetail 是预览抽屉与产物页使用的完整信息。
type ArtifactDetail struct {
	ArtifactBrief
	ConversationID string `json:"conversationId"`
	TaskID         string `json:"taskId,omitempty"`
	MessageID      string `json:"messageId,omitempty"`
	Filename       string `json:"filename,omitempty"`
	// Path 是文件产物的绝对路径；链接为空。
	Path string `json:"path,omitempty"`
	URI  string `json:"uri,omitempty"`
	// External 表示产物只登记了外部文件，删除时不会删除原文件。
	External bool `json:"external,omitempty"`
	// Summary 是一句话说明，可空。
	Summary string `json:"summary,omitempty"`
	// SourceKind / SourceName 标识产物来源（main / agent / group）。
	SourceKind string `json:"sourceKind,omitempty"`
	SourceName string `json:"sourceName,omitempty"`
}

// ArtifactFilter 描述产物页的筛选条件。
type ArtifactFilter struct {
	Kind ArtifactKind
	// Kinds 非空时按多个类型过滤，优先于 Kind。
	Kinds []ArtifactKind
	// OwnerID 是会话归属：主助手为 main，员工为 agentID。
	OwnerID string
	TaskID  string
	Search  string
	Limit   int
}

// ArtifactService 是消息、任务卡片与产物页依赖的只读能力。
type ArtifactService interface {
	Artifacts(filter ArtifactFilter) []ArtifactDetail
	Artifact(id string) ArtifactDetail
	DeleteArtifact(id string) error
	// ArtifactRevision 在产物创建或删除时递增，UI 据此刷新。
	ArtifactRevision() uint64
}
