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

type Model struct {
	Provider     string `json:"provider"`
	BaseURL      string `json:"baseUrl"`
	APIKey       string `json:"apiKey"`
	DefaultModel string `json:"defaultModel"`
}

type Config struct {
	LogLevel string `json:"logLevel"`
	Model    Model  `json:"model"`
}

// Default 返回首次启动使用的配置。
func Default() Config {
	return Config{
		LogLevel: "info",
		Model: Model{
			Provider: "openai-compatible",
		},
	}
}

// Load 读取配置；文件不存在时写入默认配置并返回。
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

	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save 以缩进 JSON 写入配置文件，并尽量收紧文件权限。
func Save(path string, cfg Config) error {
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

	if c.Model.Provider == "" {
		return errors.New("模型 provider 不能为空")
	}
	if c.Model.BaseURL == "" {
		return nil
	}

	parsed, err := url.Parse(c.Model.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("模型 baseUrl 无效：%s", c.Model.BaseURL)
	}
	return nil
}

// Redacted 返回可安全写入日志的副本。
func (c Config) Redacted() Config {
	c.Model.APIKey = MaskKey(c.Model.APIKey)
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

func (c *Config) normalize() {
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}

	c.Model.Provider = strings.TrimSpace(c.Model.Provider)
	if c.Model.Provider == "" {
		c.Model.Provider = "openai-compatible"
	}
	c.Model.BaseURL = strings.TrimRight(strings.TrimSpace(c.Model.BaseURL), "/")
	c.Model.APIKey = strings.TrimSpace(c.Model.APIKey)
	c.Model.DefaultModel = strings.TrimSpace(c.Model.DefaultModel)
}
