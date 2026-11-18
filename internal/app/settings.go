package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/store"
)

// settingsMetaKey 是运行设置在 app_meta 中的键，整份 JSON 一次写入。
const settingsMetaKey = "settings.v1"

// SettingsService 负责运行设置的读取、校验与持久化。
// 保存后立即更新内存快照，行为参数无需重启即可生效。
type SettingsService struct {
	store  *store.Store
	logger *slog.Logger

	mu      sync.RWMutex
	current contract.Settings
}

func NewSettingsService(st *store.Store, logger *slog.Logger) *SettingsService {
	if logger == nil {
		logger = slog.Default()
	}
	s := &SettingsService{store: st, logger: logger}
	s.current = s.load()
	return s
}

// load 读取持久化设置；不存在或损坏时回退默认值，不阻塞启动。
func (s *SettingsService) load() contract.Settings {
	next := contract.DefaultSettings()
	if s.store == nil {
		return next
	}

	raw, err := s.store.Meta.Get(context.Background(), settingsMetaKey)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.logger.Warn("读取运行设置失败，使用默认值", "error", err)
		}
		return next
	}

	next = contract.DefaultSettings()
	if err := json.Unmarshal([]byte(raw), &next); err != nil {
		s.logger.Warn("运行设置解析失败，使用默认值", "error", err)
		return contract.DefaultSettings()
	}
	next.Normalize()
	if err := next.Validate(); err != nil {
		s.logger.Warn("运行设置取值无效，使用默认值", "error", err)
		return contract.DefaultSettings()
	}
	return next
}

func (s *SettingsService) Settings() contract.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Clone()
}

func (s *SettingsService) Save(next contract.Settings) error {
	next.Normalize()
	if err := next.Validate(); err != nil {
		return err
	}
	if s.store == nil {
		return errors.New("设置存储不可用")
	}

	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化运行设置失败：%w", err)
	}
	if err := s.store.Meta.Set(context.Background(), settingsMetaKey, string(data)); err != nil {
		return fmt.Errorf("保存运行设置失败：%w", err)
	}

	s.mu.Lock()
	s.current = next.Clone()
	s.mu.Unlock()
	return nil
}

func (s *SettingsService) Reset(section string) error {
	next := s.Settings()
	defaults := contract.DefaultSettings()

	switch section {
	case "task":
		next.Task = defaults.Task
	case "group":
		next.Group = defaults.Group
	case "memory":
		next.Memory = defaults.Memory
	case "prompt":
		next.Prompt = defaults.Prompt
	case "appearance":
		next.Appearance = defaults.Appearance
	default:
		return fmt.Errorf("未知的设置分类：%s", section)
	}
	return s.Save(next)
}

// 以下只读快照方法供同包服务使用。

func (s *SettingsService) Task() contract.TaskSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := s.current.Task
	snapshot.RetryDelaysSeconds = append([]int(nil), s.current.Task.RetryDelaysSeconds...)
	return snapshot
}

func (s *SettingsService) Group() contract.GroupSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Group
}

func (s *SettingsService) Memory() contract.MemorySettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Memory
}

func (s *SettingsService) PromptText() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Prompt.Global
}

func (s *SettingsService) ThemeMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Appearance.Theme
}

// promptWithGlobal 在基础提示词后追加全局提示词，并始终以记忆检查清单收尾。
// 全局提示词为空时不留空行，避免稀释人设与规则。
func promptWithGlobal(base string, settings *SettingsService) string {
	parts := []string{strings.TrimSpace(base)}
	if settings != nil {
		if extra := strings.TrimSpace(settings.PromptText()); extra != "" {
			parts = append(parts, extra)
		}
	}
	parts = append(parts, memoryCheckGuide)
	return strings.Join(parts, "\n\n")
}

// toolRoundLimit 返回单次生成的工具调用轮次上限。
func toolRoundLimit(settings *SettingsService) int {
	if settings == nil {
		return maxToolRounds
	}
	limit := settings.Group().MaxToolRounds
	if limit <= 0 {
		return maxToolRounds
	}
	return limit
}
