package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func newTestSettings(t *testing.T) (*SettingsService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st := openTestStore(t, path, false)
	return NewSettingsService(st, testLogger()), path
}

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	svc, path := newTestSettings(t)

	got := svc.Settings()
	if got.Task.MaxConcurrent != 4 || got.Task.MaxAttempts != 3 || got.Group.MaxRounds != 3 {
		t.Fatalf("默认设置 = %+v, want 任务并发 4 / 尝试 3 / 群聊 3", got)
	}

	next := got.Clone()
	next.Task.MaxConcurrent = 8
	next.Task.MaxAttempts = 4
	next.Task.RetryDelaysSeconds = []int{5}
	next.Prompt.Global = "  回答先给结论  "
	if err := svc.Save(next); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	saved := svc.Settings()
	if saved.Task.MaxConcurrent != 8 {
		t.Fatalf("MaxConcurrent = %d, want 8", saved.Task.MaxConcurrent)
	}
	if saved.Prompt.Global != "回答先给结论" {
		t.Fatalf("全局提示词 = %q, want 去除首尾空白", saved.Prompt.Global)
	}
	if len(saved.Task.RetryDelaysSeconds) != 3 {
		t.Fatalf("重试间隔 = %v, want 3 项补齐到尝试次数-1", saved.Task.RetryDelaysSeconds)
	}

	// 重新打开数据库后应读到持久化后的值。
	st := openTestStore(t, path, false)
	again := NewSettingsService(st, testLogger())
	if got := again.Settings().Task.MaxConcurrent; got != 8 {
		t.Fatalf("重启后 MaxConcurrent = %d, want 8", got)
	}
}

func TestSettingsRejectsOutOfRange(t *testing.T) {
	svc, _ := newTestSettings(t)

	next := svc.Settings()
	next.Task.MaxConcurrent = 0
	if err := svc.Save(next); err == nil || !strings.Contains(err.Error(), "并发") {
		t.Fatalf("Save() error = %v, want 中文并发范围提示", err)
	}

	next = svc.Settings()
	next.Prompt.Global = strings.Repeat("提", 4001)
	if err := svc.Save(next); err == nil || !strings.Contains(err.Error(), "4000") {
		t.Fatalf("Save() error = %v, want 提示词长度上限", err)
	}
}

func TestSettingsResetSection(t *testing.T) {
	svc, _ := newTestSettings(t)

	next := svc.Settings()
	next.Group.MaxRounds = 7
	next.Task.MaxConcurrent = 9
	if err := svc.Save(next); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := svc.Reset("group"); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	got := svc.Settings()
	if got.Group.MaxRounds != 3 {
		t.Fatalf("Reset 后群聊轮次 = %d, want 3", got.Group.MaxRounds)
	}
	if got.Task.MaxConcurrent != 9 {
		t.Fatalf("Reset 不应改动其它分类，MaxConcurrent = %d, want 9", got.Task.MaxConcurrent)
	}
}

func TestSettingsCorruptFallsBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st := openTestStore(t, path, false)
	if err := st.Meta.Set(context.Background(), settingsMetaKey, "{broken"); err != nil {
		t.Fatalf("Meta.Set() error = %v", err)
	}

	svc := NewSettingsService(st, testLogger())
	if got := svc.Settings().Task.MaxConcurrent; got != 4 {
		t.Fatalf("损坏配置回退 MaxConcurrent = %d, want 4", got)
	}
}
