package contract

// ProviderConfig 是设置页展示的服务商配置。
// APIKey 始终为脱敏值；APIKeyInput 只在保存时使用，不会回传。
type ProviderConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	BaseURL     string `json:"baseUrl"`
	APIKey      string `json:"apiKey"`
	APIKeyInput string `json:"-"`
	HasKey      bool   `json:"hasKey"`
	ClearKey    bool   `json:"-"`
	Enabled     bool   `json:"enabled"`
	ModelCount  int    `json:"modelCount"`
}

// ModelConfig 是设置页展示的模型配置。
type ModelConfig struct {
	Key             string `json:"key"`
	ProviderID      string `json:"providerId"`
	ModelID         string `json:"modelId"`
	Name            string `json:"name"`
	Vision          bool   `json:"vision"`
	ContextTokens   int    `json:"contextTokens"`
	MaxOutputTokens int    `json:"maxOutputTokens"`
	Enabled         bool   `json:"enabled"`
}

// TTSSettings 是设置页展示的语音合成配置。
// APIKey 始终为脱敏值；APIKeyInput 只在保存时使用，不会回传。
type TTSSettings struct {
	Enabled     bool   `json:"enabled"`
	BaseURL     string `json:"baseUrl"`
	APIKey      string `json:"apiKey"`
	APIKeyInput string `json:"-"`
	HasKey      bool   `json:"hasKey"`
	ClearKey    bool   `json:"-"`
	ResourceID  string `json:"resourceId"`
	Speaker     string `json:"speaker"`
	Format      string `json:"format"`
	SampleRate  int    `json:"sampleRate"`
	SpeechRate  int    `json:"speechRate"`
}

// ModelConfigService 是模型服务与模型的配置入口。
// 所有修改都写入 config.json，保存后需要重启才会重建网关。
type ModelConfigService interface {
	Providers() []ProviderConfig
	// SaveProvider 新增或更新服务商；draft.APIKeyInput 为空表示保持原密钥，
	// draft.ClearKey 为 true 时清除密钥。返回服务商 ID。
	SaveProvider(draft ProviderConfig) (string, error)
	DeleteProvider(id string) error
	// TestProvider 用草稿参数发起一次模型列表请求，不落盘。
	TestProvider(draft ProviderConfig) (string, error)
	// FetchModels 用已保存的凭据拉取上游模型列表。
	FetchModels(providerID string) ([]ChatModel, error)

	// TTS 返回语音合成配置快照，密钥已脱敏。
	TTS() TTSSettings
	// SaveTTS 保存语音合成配置；APIKeyInput 为空表示保持原密钥，
	// ClearKey 为 true 时清除密钥。保存后下一次播报即生效。
	SaveTTS(draft TTSSettings) error

	Models() []ModelConfig
	SaveModel(draft ModelConfig) error
	DeleteModel(key string) error
	DefaultModel() string
	SetDefaultModel(key string) error
	// ChatModels 返回启用且服务商可用的模型，供各页面模型选择器使用。
	ChatModels() []ChatModel
	// LogLevel 返回 config.json 中的日志级别，供设置页只读展示。
	LogLevel() string
}
