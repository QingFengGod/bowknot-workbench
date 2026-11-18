package contract

import (
	"errors"
	"fmt"
	"strings"
)

// 外观主题取值。
const (
	ThemeSystem = "system"
	ThemeDark   = "dark"
	ThemeLight  = "light"
)

// TaskSettings 是后台任务调度的运行参数。
type TaskSettings struct {
	MaxConcurrent      int   `json:"maxConcurrent"`
	TimeoutMinutes     int   `json:"timeoutMinutes"`
	MaxAttempts        int   `json:"maxAttempts"`
	RetryDelaysSeconds []int `json:"retryDelaysSeconds"`
}

// GroupSettings 是群聊与工具调用的运行参数。
type GroupSettings struct {
	MaxRounds     int `json:"maxRounds"`
	MaxToolRounds int `json:"maxToolRounds"`
}

// MemorySettings 是长期记忆的运行参数。
type MemorySettings struct {
	InjectItems         int `json:"injectItems"`
	InjectBudgetRunes   int `json:"injectBudgetRunes"`
	RecentDays          int `json:"recentDays"`
	DigestIntervalHours int `json:"digestIntervalHours"`
	ContextTaskLimit    int `json:"contextTaskLimit"`
}

// PromptSettings 是追加到所有 Agent 系统提示词的自定义内容。
type PromptSettings struct {
	Global string `json:"global"`
}

// AppearanceSettings 是外观设置。
type AppearanceSettings struct {
	Theme string `json:"theme"`
}

// Settings 是一次性写入 app_meta 的运行设置。
type Settings struct {
	Task       TaskSettings       `json:"task"`
	Group      GroupSettings      `json:"group"`
	Memory     MemorySettings     `json:"memory"`
	Prompt     PromptSettings     `json:"prompt"`
	Appearance AppearanceSettings `json:"appearance"`
}

// DefaultSettings 返回出厂默认值，供初始化与「恢复默认」复用。
func DefaultSettings() Settings {
	return Settings{
		Task: TaskSettings{
			MaxConcurrent:      4,
			TimeoutMinutes:     10,
			MaxAttempts:        3,
			RetryDelaysSeconds: []int{5, 15},
		},
		Group: GroupSettings{
			MaxRounds:     3,
			MaxToolRounds: 5,
		},
		Memory: MemorySettings{
			InjectItems:         12,
			InjectBudgetRunes:   2000,
			RecentDays:          30,
			DigestIntervalHours: 6,
			ContextTaskLimit:    8,
		},
		Prompt:     PromptSettings{Global: ""},
		Appearance: AppearanceSettings{Theme: ThemeSystem},
	}
}

// Clone 返回可安全修改的副本。
func (s Settings) Clone() Settings {
	s.Task.RetryDelaysSeconds = append([]int(nil), s.Task.RetryDelaysSeconds...)
	return s
}

// Normalize 裁剪空白、规整主题取值，并把重试间隔长度对齐到尝试次数。
// 越界数值不在这一步修正，交给 Validate 给出明确报错。
func (s *Settings) Normalize() {
	s.Prompt.Global = strings.TrimSpace(s.Prompt.Global)
	s.Appearance.Theme = strings.ToLower(strings.TrimSpace(s.Appearance.Theme))
	if s.Appearance.Theme == "" {
		s.Appearance.Theme = ThemeSystem
	}

	if s.Task.MaxAttempts >= 1 {
		want := s.Task.MaxAttempts - 1
		defaults := DefaultSettings().Task.RetryDelaysSeconds
		delays := make([]int, 0, want)
		for i := 0; i < want; i++ {
			if i < len(s.Task.RetryDelaysSeconds) {
				delays = append(delays, s.Task.RetryDelaysSeconds[i])
				continue
			}
			delays = append(delays, defaults[i%len(defaults)])
		}
		s.Task.RetryDelaysSeconds = delays
	}
}

// Validate 拒绝越界与非法取值，错误信息直接展示给用户。
func (s Settings) Validate() error {
	if s.Task.MaxConcurrent < 1 || s.Task.MaxConcurrent > 16 {
		return errors.New("任务并发数需在 1 到 16 之间")
	}
	if s.Task.TimeoutMinutes < 1 || s.Task.TimeoutMinutes > 120 {
		return errors.New("任务超时需在 1 到 120 分钟之间")
	}
	if s.Task.MaxAttempts < 1 || s.Task.MaxAttempts > 5 {
		return errors.New("任务尝试次数需在 1 到 5 之间")
	}
	for _, delay := range s.Task.RetryDelaysSeconds {
		if delay < 0 || delay > 300 {
			return errors.New("任务重试间隔需在 0 到 300 秒之间")
		}
	}

	if s.Group.MaxRounds < 1 || s.Group.MaxRounds > 10 {
		return errors.New("群聊轮次需在 1 到 10 之间")
	}
	if s.Group.MaxToolRounds < 1 || s.Group.MaxToolRounds > 20 {
		return errors.New("工具调用轮次需在 1 到 20 之间")
	}

	if s.Memory.InjectItems < 1 || s.Memory.InjectItems > 50 {
		return errors.New("记忆注入条数需在 1 到 50 之间")
	}
	if s.Memory.InjectBudgetRunes < 500 || s.Memory.InjectBudgetRunes > 8000 {
		return errors.New("记忆字数预算需在 500 到 8000 之间")
	}
	if s.Memory.RecentDays < 1 || s.Memory.RecentDays > 365 {
		return errors.New("记忆时间窗口需在 1 到 365 天之间")
	}
	if s.Memory.DigestIntervalHours < 1 || s.Memory.DigestIntervalHours > 168 {
		return errors.New("记忆摘要间隔需在 1 到 168 小时之间")
	}
	if s.Memory.ContextTaskLimit < 0 || s.Memory.ContextTaskLimit > 30 {
		return errors.New("直聊任务条数需在 0 到 30 之间")
	}

	if len([]rune(s.Prompt.Global)) > 4000 {
		return errors.New("全局提示词不能超过 4000 字")
	}

	switch s.Appearance.Theme {
	case ThemeSystem, ThemeDark, ThemeLight:
	default:
		return fmt.Errorf("外观主题取值无效：%s", s.Appearance.Theme)
	}
	return nil
}

// SettingsService 是运行设置的唯一入口。
type SettingsService interface {
	Settings() Settings
	Save(next Settings) error
	// Reset 只重置指定分类：task / group / memory / prompt / appearance。
	Reset(section string) error
}
