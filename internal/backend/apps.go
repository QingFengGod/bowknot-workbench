package backend

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"bowknot-workbench/internal/contract"
)

// appCatalog 是 Apps 能力的本地演示实现。
// 后续替换为应用包扫描与 SQLite 时，UI 不需要改动。
type appCatalog struct {
	apps []contract.AppItem
	seq  int
}

// NewAppCatalog 创建内存版应用目录，数据对齐 Web 端的演示数据。
func NewAppCatalog() contract.AppCatalog {
	now := time.Now().UnixMilli()
	const minute = int64(60 * 1000)
	const hour = 60 * minute
	const day = 24 * hour

	apps := []contract.AppItem{
		{
			ID:          "password-box",
			Name:        "密码箱",
			DisplayName: "密码箱",
			Version:     "1.0.2",
			Description: "本地保存账号密码，支持助理代填",
			IconText:    "密",
			SourceType:  "local",
			Source:      `D:\bowknot-apps\password-box`,
			InstalledAt: now - 12*day,
			LastUsedAt:  now - 2*minute,
			DataSize:    18 * 1024,
			Tools: []contract.AppTool{
				{Name: "addPassword", Description: "新增一条账号密码记录", Enabled: true},
				{Name: "listPasswords", Description: "列出已保存的账号，不返回明文密码", Enabled: true},
				{Name: "deletePassword", Description: "删除指定账号密码记录", Dangerous: true, Enabled: true},
			},
		},
		{
			ID:          "weekly-report",
			Name:        "weekly-report",
			DisplayName: "周报助手",
			Version:     "1.2.0",
			Description: "根据本周的工作记录自动生成周报",
			IconText:    "周",
			SourceType:  "remote",
			Source:      "https://apps.bowknot.dev/weekly-report",
			InstalledAt: now - 8*day,
			LastUsedAt:  now - hour,
			DataSize:    1638 * 1024,
			Tools: []contract.AppTool{
				{Name: "generateWeeklyReport", Description: "汇总本周工作并生成周报草稿", Enabled: true},
				{Name: "exportWeeklyReport", Description: "把周报导出为 Markdown 文件", Enabled: true},
			},
		},
		{
			ID:          "quick-note",
			Name:        "quick-note",
			DisplayName: "灵感速记",
			Version:     "0.9.1",
			Description: "随手记下一句话，空闲时再整理",
			IconText:    "灵",
			SourceType:  "local",
			Source:      `D:\bowknot-apps\quick-note`,
			InstalledAt: now - 3*day,
			DataSize:    4300,
			Tools: []contract.AppTool{
				{Name: "addNote", Description: "记录一条灵感或待办", Enabled: true},
				{Name: "searchNotes", Description: "按关键词搜索已记录的灵感", Enabled: true},
			},
		},
		{
			ID:          "data-board",
			Name:        "data-board",
			DisplayName: "数据看板",
			Version:     "2.0.0",
			Description: "把本地数据整理成可读的看板",
			IconText:    "数",
			SourceType:  "remote",
			Source:      "https://apps.bowknot.dev/data-board",
			InstalledAt: now - 20*day,
			LastUsedAt:  now - 5*day,
			DataSize:    8 * 1024 * 1024,
			Tools: []contract.AppTool{
				{Name: "queryData", Description: "按条件查询本地数据并汇总", Enabled: true},
				{Name: "clearCache", Description: "清除看板缓存与聚合结果", Dangerous: true, Enabled: false},
			},
		},
		{
			ID:          "file-organizer",
			Name:        "file-organizer",
			DisplayName: "文件整理",
			Version:     "1.4.3",
			Description: "批量重命名与归档本地文件",
			IconText:    "文",
			SourceType:  "local",
			Source:      `D:\bowknot-apps\file-organizer`,
			InstalledAt: now - 26*day,
			LastUsedAt:  now - 3*hour,
			DataSize:    2 * 1024 * 1024,
			Tools:       []contract.AppTool{},
		},
	}

	return &appCatalog{apps: apps}
}

func (s *appCatalog) Apps() []contract.AppItem {
	out := make([]contract.AppItem, len(s.apps))
	for i, app := range s.apps {
		out[i] = app
		out[i].Tools = append([]contract.AppTool{}, app.Tools...)
	}
	return out
}

func (s *appCatalog) Preview(source contract.InstallSource) (contract.AppPreview, error) {
	manifest, err := s.parse(source)
	if err != nil {
		return contract.AppPreview{}, err
	}
	if s.index(manifest.ID) >= 0 {
		return contract.AppPreview{}, errors.New("该应用已安装")
	}
	return contract.AppPreview{Manifest: manifest, Source: source}, nil
}

func (s *appCatalog) Install(preview contract.AppPreview) error {
	manifest := preview.Manifest
	if manifest.ID == "" {
		return errors.New("应用包信息不完整")
	}
	if s.index(manifest.ID) >= 0 {
		return errors.New("该应用已安装")
	}
	s.apps = append(s.apps, contract.AppItem{
		ID:          manifest.ID,
		Name:        manifest.Name,
		DisplayName: manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		IconText:    firstRune(manifest.Name),
		SourceType:  preview.Source.Type,
		Source:      strings.TrimSpace(preview.Source.Value),
		InstalledAt: time.Now().UnixMilli(),
		Tools:       manifest.Tools,
	})
	return nil
}

func (s *appCatalog) Rename(id string, displayName string) error {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return errors.New("应用显示名称不能为空")
	}
	if utf8.RuneCountInString(name) > 20 {
		return errors.New("应用显示名称不能超过 20 个字符")
	}
	i := s.index(id)
	if i < 0 {
		return fmt.Errorf("应用不存在：%s", id)
	}
	s.apps[i].DisplayName = name
	return nil
}

func (s *appCatalog) Uninstall(id string, keepData bool) error {
	i := s.index(id)
	if i < 0 {
		return fmt.Errorf("应用不存在：%s", id)
	}
	// 演示实现没有持久化数据，keepData 暂时只影响后续真实实现的清理策略。
	_ = keepData
	s.apps = append(s.apps[:i], s.apps[i+1:]...)
	return nil
}

func (s *appCatalog) SetToolEnabled(id string, toolName string, enabled bool) error {
	i := s.index(id)
	if i < 0 {
		return fmt.Errorf("应用不存在：%s", id)
	}
	for j := range s.apps[i].Tools {
		if s.apps[i].Tools[j].Name == toolName {
			s.apps[i].Tools[j].Enabled = enabled
			return nil
		}
	}
	return fmt.Errorf("能力不存在：%s", toolName)
}

func (s *appCatalog) ClearData(id string) error {
	i := s.index(id)
	if i < 0 {
		return fmt.Errorf("应用不存在：%s", id)
	}
	s.apps[i].DataSize = 0
	return nil
}

func (s *appCatalog) MarkUsed(id string) error {
	i := s.index(id)
	if i < 0 {
		return fmt.Errorf("应用不存在：%s", id)
	}
	s.apps[i].LastUsedAt = time.Now().UnixMilli()
	return nil
}

func (s *appCatalog) index(id string) int {
	for i, app := range s.apps {
		if app.ID == id {
			return i
		}
	}
	return -1
}

func (s *appCatalog) parse(source contract.InstallSource) (contract.AppManifest, error) {
	value := strings.TrimSpace(source.Value)
	if value == "" {
		if source.Type == "local" {
			return contract.AppManifest{}, errors.New("请输入应用包路径")
		}
		return contract.AppManifest{}, errors.New("请输入远程应用包地址")
	}
	if source.Type == "remote" && !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return contract.AppManifest{}, errors.New("请输入以 http:// 或 https:// 开头的地址")
	}
	if strings.Contains(strings.ToLower(value), "invalid") {
		return contract.AppManifest{}, errors.New("应用包缺少有效的 manifest.json")
	}

	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "pomodoro") || strings.Contains(value, "番茄"):
		return contract.AppManifest{
			ID:          "pomodoro",
			Name:        "番茄钟",
			Version:     "1.0.0",
			Description: "专注计时与休息提醒，助理可以直接帮你开始一轮专注",
			Tools: []contract.AppTool{
				{Name: "startTimer", Description: "开始一轮专注计时", Enabled: true},
				{Name: "stopTimer", Description: "结束当前专注计时", Enabled: true},
				{Name: "resetTimer", Description: "重置计时与历史记录", Dangerous: true, Enabled: true},
			},
		}, nil
	case strings.Contains(lower, "reading") || strings.Contains(value, "阅读"):
		return contract.AppManifest{
			ID:          "reading-list",
			Name:        "阅读清单",
			Version:     "0.8.2",
			Description: "收集想读的内容并记录阅读进度",
			Tools: []contract.AppTool{
				{Name: "addBookmark", Description: "添加一条待读记录", Enabled: true},
				{Name: "markAsRead", Description: "把一条记录标记为已读", Enabled: true},
				{Name: "removeBookmark", Description: "删除一条阅读记录", Dangerous: true, Enabled: true},
			},
		}, nil
	}

	s.seq++
	return contract.AppManifest{
		ID:          fmt.Sprintf("app-demo-%d", s.seq),
		Name:        "示例应用",
		Version:     "1.0.0",
		Description: "从应用包安装的应用，能力由应用包定义",
		Tools: []contract.AppTool{
			{Name: "runTask", Description: "执行应用定义的一项任务", Enabled: true},
			{Name: "resetData", Description: "重置应用数据", Dangerous: true, Enabled: true},
		},
	}, nil
}
