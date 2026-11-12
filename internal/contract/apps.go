package contract

// AppTool 是应用开放给助理的能力。
type AppTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Dangerous 表示执行前需要用户确认。
	Dangerous bool `json:"dangerous"`
	Enabled   bool `json:"enabled"`
}

// AppItem 描述一个已安装应用。
type AppItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// DisplayName 是本地显示名，可重命名。
	DisplayName string `json:"displayName"`
	Version     string `json:"version"`
	Description string `json:"description"`
	IconText    string `json:"iconText"`
	SourceType  string `json:"sourceType"`
	Source      string `json:"source"`
	InstalledAt int64  `json:"installedAt"`
	// LastUsedAt 为 0 表示从未使用。
	LastUsedAt int64     `json:"lastUsedAt"`
	DataSize   int64     `json:"dataSize"`
	Tools      []AppTool `json:"tools"`
}

// AppManifest 是应用包解析后的清单，用于安装前预览。
type AppManifest struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Tools       []AppTool `json:"tools"`
}

// AppPreview 是安装前的预览结果。
type AppPreview struct {
	Manifest AppManifest   `json:"manifest"`
	Source   InstallSource `json:"source"`
}

// AppCatalog 是 Apps 页面依赖的能力接口，由后端实现。
type AppCatalog interface {
	Apps() []AppItem
	Preview(source InstallSource) (AppPreview, error)
	Install(preview AppPreview) error
	Rename(id string, displayName string) error
	Uninstall(id string, keepData bool) error
	SetToolEnabled(id string, toolName string, enabled bool) error
	ClearData(id string) error
	MarkUsed(id string) error
}
