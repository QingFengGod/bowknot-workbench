package app

import (
	"path/filepath"
	"strings"
	"testing"

	"bowknot-workbench/internal/contract"
)

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
