// Package config 负责加载与校验本地配置。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	// ProviderOpenAICompatible 是目前唯一支持的供应商协议。
	ProviderOpenAICompatible = "openai-compatible"
	// DefaultProviderID 是旧版单模型配置迁移后使用的供应商 ID。
	DefaultProviderID = "default"
)

// Provider 是一个模型服务商的连接配置。
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Enabled bool   `json:"enabled"`
}

// Model 是一个可用的模型配置，Key 为内部唯一标识。
type Model struct {
	Key             string `json:"key"`
	ProviderID      string `json:"providerId"`
	ModelID         string `json:"modelId"`
	Name            string `json:"name"`
	Vision          bool   `json:"vision"`
	ContextTokens   int    `json:"contextTokens"`
	MaxOutputTokens int    `json:"maxOutputTokens"`
	Enabled         bool   `json:"enabled"`
}

// LegacyModel 是旧版单模型配置，仅用于读取兼容。
type LegacyModel struct {
	Provider     string `json:"provider"`
	BaseURL      string `json:"baseUrl"`
	APIKey       string `json:"apiKey"`
	DefaultModel string `json:"defaultModel"`
}

type Config struct {
	LogLevel        string     `json:"logLevel"`
	Providers       []Provider `json:"providers,omitempty"`
	Models          []Model    `json:"models,omitempty"`
	DefaultModelKey string     `json:"defaultModelKey,omitempty"`
	// Legacy 只在读取旧配置时出现，保存时始终为空。
	Legacy *LegacyModel `json:"model,omitempty"`
}

// Default 返回首次启动使用的配置。
func Default() Config {
	return Config{LogLevel: "info"}
}

// Load 读取配置；文件不存在时写入默认配置并返回。
// 旧版单模型配置会在这里转换为「一个供应商 + 一个模型」。
func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := Save(path, cfg); err != nil {
			return Config{}, fmt.Errorf("创建默认配置失败：%w", err)
		}
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("读取配置失败：%w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("解析配置失败：%w", err)
	}

	cfg.migrateLegacy()
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save 以缩进 JSON 写入配置文件，并尽量收紧文件权限。
func Save(path string, cfg Config) error {
	cfg.Legacy = nil
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败：%w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败：%w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("写入配置失败：%w", err)
	}
	return nil
}

func (c Config) Validate() error {
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("日志级别无效：%s", c.LogLevel)
	}

	providers := make(map[string]bool, len(c.Providers))
	for _, item := range c.Providers {
		if strings.TrimSpace(item.ID) == "" {
			return errors.New("服务商 ID 不能为空")
		}
		if providers[item.ID] {
			return fmt.Errorf("服务商 ID 重复：%s", item.ID)
		}
		providers[item.ID] = true

		if strings.TrimSpace(item.Name) == "" {
			return fmt.Errorf("服务商名称不能为空：%s", item.ID)
		}
		if item.Type != "" && item.Type != ProviderOpenAICompatible {
			return fmt.Errorf("服务商协议不支持：%s", item.Type)
		}
		if base := strings.TrimSpace(item.BaseURL); base != "" {
			parsed, err := url.Parse(base)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("服务商 baseUrl 无效：%s", base)
			}
		}
	}

	models := make(map[string]bool, len(c.Models))
	for _, item := range c.Models {
		if strings.TrimSpace(item.Key) == "" {
			return errors.New("模型 key 不能为空")
		}
		if models[item.Key] {
			return fmt.Errorf("模型 key 重复：%s", item.Key)
		}
		models[item.Key] = true

		if !providers[item.ProviderID] {
			return fmt.Errorf("模型所属服务商不存在：%s", item.ProviderID)
		}
		if item.ModelID == "" {
			return fmt.Errorf("模型 ID 不能为空：%s", item.Key)
		}
		if strings.Contains(item.ModelID, "/") {
			return fmt.Errorf("模型 ID 不能包含斜杠：%s", item.ModelID)
		}
		if item.ProviderID+"/"+item.ModelID != item.Key {
			return fmt.Errorf("模型 key 与所属服务商不一致：%s", item.Key)
		}
		if item.ContextTokens != 0 && item.ContextTokens < 1000 {
			return fmt.Errorf("模型上下文长度过小：%s", item.Name)
		}
		if item.MaxOutputTokens < 0 {
			return fmt.Errorf("模型最大输出长度无效：%s", item.Name)
		}
		if item.ContextTokens > 0 && item.MaxOutputTokens > item.ContextTokens {
			return fmt.Errorf("模型最大输出不能大于上下文长度：%s", item.Name)
		}
	}

	if key := strings.TrimSpace(c.DefaultModelKey); key != "" {
		model, ok := c.ModelByKey(key)
		if !ok {
			return fmt.Errorf("默认模型不存在：%s", key)
		}
		if !model.Enabled {
			return fmt.Errorf("默认模型已停用：%s", key)
		}
		if provider, ok := c.ProviderByID(model.ProviderID); !ok || !provider.Enabled {
			return fmt.Errorf("默认模型所属服务商已停用：%s", key)
		}
	}
	return nil
}

// ProviderByID 按 ID 查找服务商。
func (c Config) ProviderByID(id string) (Provider, bool) {
	for _, item := range c.Providers {
		if item.ID == id {
			return item, true
		}
	}
	return Provider{}, false
}

// ModelByKey 按内部 key 查找模型。
func (c Config) ModelByKey(key string) (Model, bool) {
	for _, item := range c.Models {
		if item.Key == key {
			return item, true
		}
	}
	return Model{}, false
}

// EnabledModels 返回模型与服务商都启用、且服务商已配置 BaseURL 的模型。
func (c Config) EnabledModels() []Model {
	out := make([]Model, 0, len(c.Models))
	for _, item := range c.Models {
		if !item.Enabled {
			continue
		}
		provider, ok := c.ProviderByID(item.ProviderID)
		if !ok || !provider.Enabled || strings.TrimSpace(provider.BaseURL) == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

// DefaultModel 返回默认模型；未配置默认模型时回退到第一个启用模型。
func (c Config) DefaultModel() (Model, bool) {
	if model, ok := c.ModelByKey(strings.TrimSpace(c.DefaultModelKey)); ok && model.Enabled {
		return model, true
	}
	enabled := c.EnabledModels()
	if len(enabled) == 0 {
		return Model{}, false
	}
	return enabled[0], true
}

// Redacted 返回可安全写入日志的副本。
func (c Config) Redacted() Config {
	providers := make([]Provider, len(c.Providers))
	copy(providers, c.Providers)
	for i := range providers {
		providers[i].APIKey = MaskKey(providers[i].APIKey)
	}
	c.Providers = providers
	if c.Legacy != nil {
		legacy := *c.Legacy
		legacy.APIKey = MaskKey(legacy.APIKey)
		c.Legacy = &legacy
	}
	return c
}

// MaskKey 保留首尾各四位，短 Key 全部隐藏。
func MaskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}

	runes := []rune(key)
	if len(runes) <= 8 {
		return "****"
	}
	return string(runes[:4]) + "****" + string(runes[len(runes)-4:])
}

// migrateLegacy 把旧版单模型配置转换为多服务商结构。
// 旧配置没有 BaseURL（演示模式）时不生成任何记录，继续回退到 Fake 网关。
func (c *Config) migrateLegacy() {
	if c.Legacy == nil {
		return
	}
	legacy := *c.Legacy
	c.Legacy = nil

	if len(c.Providers) > 0 || len(c.Models) > 0 {
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(legacy.BaseURL), "/")
	if baseURL == "" {
		return
	}

	c.Providers = []Provider{{
		ID:      DefaultProviderID,
		Name:    "默认服务",
		Type:    normalizeProviderType(legacy.Provider),
		BaseURL: baseURL,
		APIKey:  strings.TrimSpace(legacy.APIKey),
		Enabled: true,
	}}
	if modelID := strings.TrimSpace(legacy.DefaultModel); modelID != "" {
		key := DefaultProviderID + "/" + modelID
		c.Models = []Model{{
			Key:        key,
			ProviderID: DefaultProviderID,
			ModelID:    modelID,
			Name:       modelID,
			Enabled:    true,
		}}
		c.DefaultModelKey = key
	}
}

func (c *Config) normalize() {
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}

	for i := range c.Providers {
		item := &c.Providers[i]
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		item.Type = normalizeProviderType(item.Type)
		item.BaseURL = strings.TrimRight(strings.TrimSpace(item.BaseURL), "/")
		item.APIKey = strings.TrimSpace(item.APIKey)
	}

	for i := range c.Models {
		item := &c.Models[i]
		item.Key = strings.TrimSpace(item.Key)
		item.ProviderID = strings.TrimSpace(item.ProviderID)
		item.ModelID = strings.TrimSpace(item.ModelID)
		item.Name = strings.TrimSpace(item.Name)
		if item.Name == "" {
			item.Name = item.ModelID
		}
		if item.Key == "" && item.ProviderID != "" && item.ModelID != "" {
			item.Key = item.ProviderID + "/" + item.ModelID
		}
		if item.ContextTokens < 0 {
			item.ContextTokens = 0
		}
		if item.MaxOutputTokens < 0 {
			item.MaxOutputTokens = 0
		}
	}
	c.DefaultModelKey = strings.TrimSpace(c.DefaultModelKey)
}

func normalizeProviderType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ProviderOpenAICompatible
	}
	return value
}
