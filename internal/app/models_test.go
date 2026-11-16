package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/llm"
)

type failingProvider struct{}

func (failingProvider) Stream(ctx context.Context, req llm.ChatRequest) (<-chan llm.Chunk, error) {
	return nil, errors.New("unavailable")
}

func (failingProvider) Complete(ctx context.Context, req llm.ChatRequest) (string, error) {
	return "", errors.New("unavailable")
}

func (failingProvider) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	return nil, errors.New("unavailable")
}

func TestResolveModels(t *testing.T) {
	ctx := context.Background()

	fake := llm.NewFake(llm.FakeOptions{Models: []llm.ModelInfo{
		{ID: "model-a", Name: "模型甲"},
		{ID: "model-b"},
	}})
	models := ResolveModels(ctx, fake, "fallback", testLogger())
	if len(models) != 2 {
		t.Fatalf("模型数量 = %d, want 2", len(models))
	}
	if models[0].ID != "model-a" || models[0].Name != "模型甲" {
		t.Fatalf("第一个模型 = %+v", models[0])
	}
	// 供应商没给名称时用 ID 兜底，不生成演示名称。
	if models[1].ID != "model-b" || models[1].Name != "model-b" {
		t.Fatalf("第二个模型 = %+v", models[1])
	}

	fallback := ResolveModels(ctx, failingProvider{}, "fallback-model", testLogger())
	if len(fallback) != 1 || fallback[0].ID != "fallback-model" {
		t.Fatalf("回退模型 = %+v", fallback)
	}

	if empty := ResolveModels(ctx, failingProvider{}, "", testLogger()); len(empty) != 0 {
		t.Fatalf("无配置时模型列表 = %+v, want empty", empty)
	}
}

func TestAssistantRejectsUnknownModel(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newFastAssistant(st)
	svc.SetModels([]contract.ChatModel{{ID: "real-model", Name: "真实模型"}})

	if _, _, err := svc.Send("session-1", "你好", "bk-pro"); err == nil || !strings.Contains(err.Error(), "模型不可用") {
		t.Fatalf("未知模型错误 = %v, want 中文模型不可用提示", err)
	}

	// 缓存的模型列表为空时不做本地校验，交给网关处理默认模型。
	plain := newFastAssistant(st)
	if got := len(plain.Models()); got != 0 {
		t.Fatalf("未注入模型列表时 Models() = %d, want 0", got)
	}
}
