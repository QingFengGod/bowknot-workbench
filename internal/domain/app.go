package domain

// Tool 是应用或插件开放的能力。
type Tool struct {
	Name        string
	Description string
	// Parameters 是 JSON Schema 参数声明，为空表示自由对象。
	Parameters string
	// TimeoutMS 是单次执行超时（毫秒），0 表示使用工具服务默认值。
	TimeoutMS int
	Dangerous bool
	Enabled   bool
}

// Runtime 描述插件的执行方式。
type Runtime struct {
	// Type 取 native 或 bun；为空表示旧包未声明，执行时会报错。
	Type string `json:"type"`
	// Entry 是 bun 插件入口文件，相对安装目录。
	Entry string `json:"entry"`
	// Commands 是 native 插件按 "GOOS-GOARCH" 声明的可执行文件相对路径。
	Commands map[string]string `json:"commands"`
}

// Permissions 是插件声明的能力边界，安装后随插件记录保存。
type Permissions struct {
	FS        []string `json:"fs"`
	Net       []string `json:"net"`
	Host      []string `json:"host"`
	TimeoutMS int      `json:"timeoutMs"`
}

// App 是一个已安装应用。
type App struct {
	ID          string
	Name        string
	DisplayName string
	Version     string
	Description string
	IconText    string
	SourceType  string
	SourceValue string
	InstallDir  string
	// Entry 是入口文件相对安装目录的路径，为空表示应用不可启动。
	Entry       string
	InstalledAt int64
	LastUsedAt  int64
	DataSize    int64
	Tools       []Tool
}

// Plugin 是一个已安装插件及其授权范围。
type Plugin struct {
	ID          string
	Name        string
	Version     string
	Description string
	IconText    string
	SourceType  string
	SourceValue string
	InstallDir  string
	InstalledAt int64
	Runtime     Runtime
	Permissions Permissions
	Tools       []Tool
	AgentIDs    []string
}
