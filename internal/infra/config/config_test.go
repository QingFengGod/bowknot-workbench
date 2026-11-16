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
	if got.Model.Provider != "openai-compatible" {
		t.Fatalf("Provider = %q", got.Model.Provider)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "\"apiKey\": \"\"") {
		t.Fatalf("default config missing apiKey field: %s", data)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := Config{
		LogLevel: "debug",
		Model: Model{
			Provider:     "openai-compatible",
			BaseURL:      "https://api.example.com/",
			APIKey:       "sk-test-secret-key",
			DefaultModel: "demo-model",
		},
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
	if got.Model.BaseURL != "https://api.example.com" {
		t.Fatalf("BaseURL = %q", got.Model.BaseURL)
	}
	if got.Model.APIKey != want.Model.APIKey {
		t.Fatalf("APIKey = %q", got.Model.APIKey)
	}
	if got.Model.DefaultModel != want.Model.DefaultModel {
		t.Fatalf("DefaultModel = %q", got.Model.DefaultModel)
	}
}

func TestValidateRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "bad log level",
			cfg:  Config{LogLevel: "verbose", Model: Model{Provider: "openai-compatible"}},
		},
		{
			name: "empty provider",
			cfg:  Config{LogLevel: "info"},
		},
		{
			name: "bad base url",
			cfg: Config{
				LogLevel: "info",
				Model:    Model{Provider: "openai-compatible", BaseURL: "api.example.com"},
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
		Model:    Model{Provider: "openai-compatible", APIKey: "sk-1234567890abcdef"},
	}

	redacted := cfg.Redacted()
	if redacted.Model.APIKey == cfg.Model.APIKey {
		t.Fatal("Redacted() did not mask APIKey")
	}
	if cfg.Model.APIKey != "sk-1234567890abcdef" {
		t.Fatal("Redacted() mutated the original config")
	}
}
