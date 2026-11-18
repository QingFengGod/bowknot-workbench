package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCreatesDefaultFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want info", got.LogLevel)
	}
	if len(got.Providers) != 0 || len(got.Models) != 0 {
		t.Fatalf("默认配置不应包含服务商或模型：%+v", got)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "\"logLevel\": \"info\"") {
		t.Fatalf("default config missing logLevel: %s", data)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := Config{
		LogLevel: "debug",
		Providers: []Provider{{
			ID:      "p1",
			Name:    "示例服务",
			Type:    ProviderOpenAICompatible,
			BaseURL: "https://api.example.com/",
			APIKey:  "sk-test-secret-key",
			Enabled: true,
		}},
		Models: []Model{{
			Key:             "p1/demo-model",
			ProviderID:      "p1",
			ModelID:         "demo-model",
			Name:            "演示模型",
			Vision:          true,
			ContextTokens:   128000,
			MaxOutputTokens: 16384,
			Enabled:         true,
		}},
		DefaultModelKey: "p1/demo-model",
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.LogLevel != want.LogLevel {
		t.Fatalf("LogLevel = %q", got.LogLevel)
	}
	if len(got.Providers) != 1 || got.Providers[0].BaseURL != "https://api.example.com" {
		t.Fatalf("Providers = %+v", got.Providers)
	}
	if got.Providers[0].APIKey != want.Providers[0].APIKey {
		t.Fatalf("APIKey = %q", got.Providers[0].APIKey)
	}
	if len(got.Models) != 1 || got.Models[0].Key != want.Models[0].Key {
		t.Fatalf("Models = %+v", got.Models)
	}
	if !got.Models[0].Vision || got.Models[0].ContextTokens != 128000 {
		t.Fatalf("模型能力字段丢失：%+v", got.Models[0])
	}
	if got.DefaultModelKey != want.DefaultModelKey {
		t.Fatalf("DefaultModelKey = %q", got.DefaultModelKey)
	}
}

func TestLoadMigratesLegacyModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{
  "logLevel": "info",
  "model": {
    "provider": "openai-compatible",
    "baseUrl": "https://api.example.com/v1/",
    "apiKey": "sk-legacy-key",
    "defaultModel": "legacy-model"
  }
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Providers) != 1 || got.Providers[0].ID != DefaultProviderID {
		t.Fatalf("迁移后的服务商 = %+v", got.Providers)
	}
	if got.Providers[0].BaseURL != "https://api.example.com/v1" {
		t.Fatalf("迁移后的 BaseURL = %q", got.Providers[0].BaseURL)
	}
	if len(got.Models) != 1 || got.Models[0].Key != "default/legacy-model" {
		t.Fatalf("迁移后的模型 = %+v", got.Models)
	}
	if got.DefaultModelKey != "default/legacy-model" {
		t.Fatalf("迁移后的默认模型 = %q", got.DefaultModelKey)
	}

	// 再次保存后旧字段应消失，读取结果保持一致。
	if err := Save(path, got); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(data), "\"model\"") {
		t.Fatalf("保存后仍包含旧 model 字段：%s", data)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("二次 Load() error = %v", err)
	}
	if reloaded.DefaultModelKey != got.DefaultModelKey {
		t.Fatalf("二次读取默认模型 = %q", reloaded.DefaultModelKey)
	}
}

func TestValidateRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "bad log level",
			cfg:  Config{LogLevel: "verbose"},
		},
		{
			name: "empty provider id",
			cfg: Config{LogLevel: "info", Providers: []Provider{{
				ID: "", Name: "x", Type: ProviderOpenAICompatible,
			}}},
		},
		{
			name: "duplicate provider id",
			cfg: Config{LogLevel: "info", Providers: []Provider{
				{ID: "p1", Name: "a", Type: ProviderOpenAICompatible},
				{ID: "p1", Name: "b", Type: ProviderOpenAICompatible},
			}},
		},
		{
			name: "bad base url",
			cfg: Config{LogLevel: "info", Providers: []Provider{{
				ID: "p1", Name: "a", Type: ProviderOpenAICompatible, BaseURL: "api.example.com",
			}}},
		},
		{
			name: "model without provider",
			cfg: Config{LogLevel: "info", Models: []Model{{
				Key: "p1/m", ProviderID: "p1", ModelID: "m", Name: "m",
			}}},
		},
		{
			name: "model key mismatch",
			cfg: Config{
				LogLevel:  "info",
				Providers: []Provider{{ID: "p1", Name: "a", Type: ProviderOpenAICompatible}},
				Models: []Model{{
					Key: "other/m", ProviderID: "p1", ModelID: "m", Name: "m",
				}},
			},
		},
		{
			name: "default model disabled",
			cfg: Config{
				LogLevel:        "info",
				Providers:       []Provider{{ID: "p1", Name: "a", Type: ProviderOpenAICompatible, Enabled: true}},
				Models:          []Model{{Key: "p1/m", ProviderID: "p1", ModelID: "m", Name: "m", Enabled: false}},
				DefaultModelKey: "p1/m",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
		})
	}
}

func TestDefaultModelFallsBackToFirstEnabled(t *testing.T) {
	cfg := Config{
		LogLevel: "info",
		Providers: []Provider{{
			ID: "p1", Name: "a", Type: ProviderOpenAICompatible, BaseURL: "https://example.com", Enabled: true,
		}},
		Models: []Model{
			{Key: "p1/a", ProviderID: "p1", ModelID: "a", Name: "a", Enabled: false},
			{Key: "p1/b", ProviderID: "p1", ModelID: "b", Name: "b", Enabled: true},
		},
	}
	model, ok := cfg.DefaultModel()
	if !ok || model.Key != "p1/b" {
		t.Fatalf("DefaultModel() = %+v, %v", model, ok)
	}
	if got := len(cfg.EnabledModels()); got != 1 {
		t.Fatalf("EnabledModels() = %d, want 1", got)
	}
}

func TestMaskKey(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"short":               "****",
		"12345678":            "****",
		"sk-1234567890abcdef": "sk-1****cdef",
	}
	for input, want := range cases {
		if got := MaskKey(input); got != want {
			t.Fatalf("MaskKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRedactedHidesKey(t *testing.T) {
	cfg := Config{
		LogLevel: "info",
		Providers: []Provider{
			{ID: "p1", Name: "a", APIKey: "sk-1234567890abcdef"},
			{ID: "p2", Name: "b", APIKey: "sk-abcdef1234567890"},
		},
	}

	redacted := cfg.Redacted()
	if redacted.Providers[0].APIKey == cfg.Providers[0].APIKey {
		t.Fatal("Redacted() did not mask APIKey")
	}
	if cfg.Providers[0].APIKey != "sk-1234567890abcdef" {
		t.Fatal("Redacted() mutated the original config")
	}
}
