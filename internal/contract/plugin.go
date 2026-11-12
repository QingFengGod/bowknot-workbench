package contract

// PluginTool 描述插件提供的一个工具。
type PluginTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Dangerous 表示执行前需要用户确认。
	Dangerous bool `json:"dangerous"`
}

// InstallSource 是插件包或应用包的安装来源。
type InstallSource struct {
	// Type 取 local 或 remote。
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Plugin 描述一个已安装插件。
type Plugin struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description"`
	IconText    string       `json:"iconText"`
	SourceType  string       `json:"sourceType"`
	Source      string       `json:"source"`
	InstalledAt int64        `json:"installedAt"`
	Tools       []PluginTool `json:"tools"`
	AgentIDs    []string     `json:"agentIds"`
}

// PluginAgent 是可以被授权使用插件的 Agent。
type PluginAgent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	AvatarText  string `json:"avatarText"`
	// IsMain 标记系统内置的主 Agent。
	IsMain bool `json:"isMain"`
}

// PluginService 是插件页面依赖的能力接口，由后端实现。
// UI 只依赖该接口，不感知数据来自本地服务还是演示数据。
type PluginService interface {
	Plugins() []Plugin
	Agents() []PluginAgent
	Install(source InstallSource) error
	Uninstall(id string) error
	SetAuthorizedAgents(id string, agentIDs []string) error
}
