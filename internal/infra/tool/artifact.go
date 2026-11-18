package tool

import "context"

// SaveArtifactToolID 是保存产物工具的 ID。
const SaveArtifactToolID = "builtin_save_artifact"

// IsArtifactTool 判断工具是否属于产物能力。
func IsArtifactTool(id string) bool {
	return id == SaveArtifactToolID
}

// ArtifactInput 是保存产物的工具入参。
type ArtifactInput struct {
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Filename   string `json:"filename"`
	Content    string `json:"content"`
	SourcePath string `json:"sourcePath"`
	Language   string `json:"language"`
	Summary    string `json:"summary"`
}

// ArtifactRef 是保存成功后返回给模型的摘要。
type ArtifactRef struct {
	ID       string
	Title    string
	Kind     string
	Filename string
	Size     int64
}

// ArtifactSink 由 app 层实现，负责校验、落盘与记录。
type ArtifactSink interface {
	SaveArtifact(ctx context.Context, conversationID string, in ArtifactInput) (ArtifactRef, error)
}

// ArtifactToolSpec 声明保存产物工具。
func ArtifactToolSpec() ToolSpec {
	return ToolSpec{
		ID:          SaveArtifactToolID,
		Name:        SaveArtifactToolID,
		Description: "把可复用的交付物保存为产物（文档、代码、图片、HTML、链接等）。当用户要求生成文件、报告、代码文件、图片或可下载内容时调用；保存后回复正文只写简要说明，不要重复整段内容。文本类用 content 传入完整内容；图片或已有文件用 sourcePath 指向数据目录内的文件；链接用 content 传入 http/https 地址。",
		Source:      SourceBuiltin,
		SourceID:    "builtin",
		Parameters: `{
  "type": "object",
  "properties": {
    "title": {"type": "string", "description": "产物标题，便于用户识别"},
    "kind": {"type": "string", "description": "markdown / text / code / html / image / link / file；省略时按 filename 推断"},
    "filename": {"type": "string", "description": "文件名，可省略"},
    "content": {"type": "string", "description": "文本内容；link 类型填 URL"},
    "sourcePath": {"type": "string", "description": "已有文件的绝对路径，仅允许数据目录内；image/file 使用"},
    "language": {"type": "string", "description": "代码语言，例如 go、python、javascript；用于选择扩展名"},
    "summary": {"type": "string", "description": "一句话说明，可省略"}
  },
  "required": ["title"]
}`,
	}
}
