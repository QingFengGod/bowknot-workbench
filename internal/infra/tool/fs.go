package tool

// 文件系统工具 ID。全部为内置工具，执行范围限制在工作区目录内，
// 模型只需使用工作区相对路径，不必关心数据目录的绝对位置。
const (
	ListDirToolID     = "builtin_list_dir"
	MakeDirToolID     = "builtin_make_dir"
	ReadFileToolID    = "builtin_read_file"
	WriteFileToolID   = "builtin_write_file"
	EditFileToolID    = "builtin_edit_file"
	DeleteFileToolID  = "builtin_delete_file"
	GlobToolID        = "builtin_glob"
	SearchFilesToolID = "builtin_search_files"
)

var fsToolIDs = map[string]bool{
	ListDirToolID:     true,
	MakeDirToolID:     true,
	ReadFileToolID:    true,
	WriteFileToolID:   true,
	EditFileToolID:    true,
	DeleteFileToolID:  true,
	GlobToolID:        true,
	SearchFilesToolID: true,
}

// IsFSTool 判断工具是否属于内置文件系统工具。
func IsFSTool(id string) bool {
	return fsToolIDs[id]
}

// FSListDirInput 是列目录的参数；depth 为 1 时只列直接子项。
type FSListDirInput struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
}

// FSMakeDirInput 是创建目录的参数。
type FSMakeDirInput struct {
	Path string `json:"path"`
}

// FSReadFileInput 是读取文件的参数。
type FSReadFileInput struct {
	Path string `json:"path"`
}

// FSWriteFileInput 是写入文件的参数，content 为完整内容。
type FSWriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FSEditFileInput 是精确替换的参数；replaceAll 为 false 时要求匹配唯一。
type FSEditFileInput struct {
	Path       string `json:"path"`
	OldText    string `json:"oldText"`
	NewText    string `json:"newText"`
	ReplaceAll bool   `json:"replaceAll"`
}

// FSDeleteFileInput 是删除文件的参数。
type FSDeleteFileInput struct {
	Path string `json:"path"`
}

// FSGlobInput 按通配符匹配工作区内的文件路径，支持 ** 跨目录。
type FSGlobInput struct {
	Pattern string `json:"pattern"`
	Limit   int    `json:"limit"`
}

// FSSearchInput 在工作区内按正则搜索文件内容。
type FSSearchInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
	Limit   int    `json:"limit"`
}

// FSToolSpecs 返回八个内置文件系统工具的声明。
func FSToolSpecs() []ToolSpec {
	return []ToolSpec{
		{
			ID:          ListDirToolID,
			Name:        ListDirToolID,
			Description: "列出工作区内某个目录的文件与子目录。path 使用工作区相对路径，省略表示工作区根目录；depth 为 1 时只列直接子项，默认 1。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "工作区相对路径，例如 docs 或 项目A/src；省略表示根目录"},
    "depth": {"type": "integer", "description": "递归层数，1 表示只列直接子项，默认 1，最多 4"}
  }
}`,
		},
		{
			ID:          MakeDirToolID,
			Name:        MakeDirToolID,
			Description: "在工作区内创建目录，已存在时不报错。需要新建项目目录时使用。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "要创建的目录，使用工作区相对路径，例如 项目A/设计稿"}
  },
  "required": ["path"]
}`,
		},
		{
			ID:          ReadFileToolID,
			Name:        ReadFileToolID,
			Description: "读取工作区内 UTF-8 文本文件的完整内容，单个文件上限 512KB。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "要读取的文件，使用工作区相对路径"}
  },
  "required": ["path"]
}`,
		},
		{
			ID:          WriteFileToolID,
			Name:        WriteFileToolID,
			Description: "在工作区内写入文本文件，父目录不存在时自动创建，内容上限 1MB。生成报告、代码、文档时优先使用相对路径（例如 docs/report.md），写入后文件会自动出现在文件库。",
			Dangerous:   true,
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "目标文件，使用工作区相对路径，可带子目录"},
    "content": {"type": "string", "description": "完整文件内容"}
  },
  "required": ["path", "content"]
}`,
		},
		{
			ID:          EditFileToolID,
			Name:        EditFileToolID,
			Description: "把工作区内文件中的指定文本精确替换为新文本。oldText 必须与原文完全一致（含缩进换行），默认要求唯一匹配。",
			Dangerous:   true,
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "要编辑的文件，使用工作区相对路径"},
    "oldText": {"type": "string", "description": "要被替换的原文"},
    "newText": {"type": "string", "description": "替换后的新文本，空字符串表示删除该段"},
    "replaceAll": {"type": "boolean", "description": "为 true 时替换全部匹配，默认 false"}
  },
  "required": ["path", "oldText", "newText"]
}`,
		},
		{
			ID:          DeleteFileToolID,
			Name:        DeleteFileToolID,
			Description: "永久删除工作区内的一个文件，不可恢复。删除目录请先清理目录内的文件。",
			Dangerous:   true,
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "要删除的文件，使用工作区相对路径"}
  },
  "required": ["path"]
}`,
		},
		{
			ID:          GlobToolID,
			Name:        GlobToolID,
			Description: "按通配符查找工作区内的文件路径，支持 *、? 与 **（跨目录）。不确定文件在哪时先 glob 再读取。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "路径通配符，例如 **/*.go 或 docs/**/*.md"},
    "limit": {"type": "integer", "description": "最多返回条数，默认 200，最多 500"}
  },
  "required": ["pattern"]
}`,
		},
		{
			ID:          SearchFilesToolID,
			Name:        SearchFilesToolID,
			Description: "在工作区内按正则搜索文件内容，返回文件路径与行号。适合查找关键字出现在哪个文件。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "正则表达式，大小写不敏感"},
    "path": {"type": "string", "description": "搜索的子目录，工作区相对路径，默认根目录"},
    "glob": {"type": "string", "description": "只搜索匹配的文件，例如 *.md，默认全部文本文件"},
    "limit": {"type": "integer", "description": "最多返回条数，默认 100，最多 300"}
  },
  "required": ["pattern"]
}`,
		},
	}
}
