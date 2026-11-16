package domain

// Tool 是应用或插件开放的能力。
type Tool struct {
	Name        string
	Description string
	Dangerous   bool
	Enabled     bool
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
	Tools       []Tool
	AgentIDs    []string
}
