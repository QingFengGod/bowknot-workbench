package domain

// ArtifactKind 是产物的预览类型。
type ArtifactKind string

const (
	ArtifactMarkdown ArtifactKind = "markdown"
	ArtifactText     ArtifactKind = "text"
	ArtifactCode     ArtifactKind = "code"
	ArtifactHTML     ArtifactKind = "html"
	ArtifactImage    ArtifactKind = "image"
	ArtifactLink     ArtifactKind = "link"
	// ArtifactFile 是 PDF、Office、压缩包等无法原生预览的兜底类型。
	ArtifactFile ArtifactKind = "file"
)

// Artifact 是 Agent 产出的一份可复用内容。
type Artifact struct {
	ID             string
	ConversationID string
	TaskID         string
	MessageID      string
	Title          string
	Kind           ArtifactKind
	MIME           string
	Filename       string
	// RelativePath 相对 artifacts 根目录；链接类型为空。
	RelativePath string
	// SourcePath 是用户目录里的原始文件路径；非空时不复制文件，只登记引用。
	SourcePath string
	// URI 只用于链接类型。
	URI       string
	Size      int64
	Summary   string
	CreatedAt int64
}
