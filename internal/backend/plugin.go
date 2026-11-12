package backend

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"bowknot-workbench/internal/contract"
)

// pluginService 是插件能力的本地演示实现。
// 后续替换为 SQLite + 插件目录扫描时，UI 不需要改动。
type pluginService struct {
	plugins []contract.Plugin
	agents  []contract.PluginAgent
	seq     int
}

// NewPluginService 创建内存版插件服务，数据对齐 Web 端的演示数据。
func NewPluginService() contract.PluginService {
	now := time.Now().UnixMilli()
	const day = int64(24 * 60 * 60 * 1000)

	mainAgent := contract.PluginAgent{
		ID:          "main-agent",
		Name:        "助理",
		Description: "系统内置主 Agent，负责日常对话与群聊任务派发",
		AvatarText:  "助",
		IsMain:      true,
	}

	agents := []contract.PluginAgent{
		mainAgent,
		{ID: "writing", Name: "写作助手", Description: "负责长文撰写与润色", AvatarText: "写"},
		{ID: "research", Name: "研究助手", Description: "搜集资料与竞品调研", AvatarText: "研"},
		{ID: "coding", Name: "代码搭档", Description: "读代码、改 bug、写测试", AvatarText: "码"},
		{ID: "schedule", Name: "日程管家", Description: "管理日程与提醒", AvatarText: "程"},
	}

	plugins := []contract.Plugin{
		{
			ID:          "web-search",
			Name:        "网页搜索",
			Version:     "1.2.0",
			Description: "联网检索资料并整理成可引用的摘要",
			IconText:    "搜",
			SourceType:  "remote",
			Source:      "https://plugins.bowknot.dev/web-search",
			InstalledAt: now - 9*day,
			AgentIDs:    []string{"main-agent", "research"},
			Tools: []contract.PluginTool{
				{Name: "searchWeb", Description: "按关键词搜索网页并返回结果摘要"},
				{Name: "readPage", Description: "读取指定网页的正文内容"},
			},
		},
		{
			ID:          "file-system",
			Name:        "文件系统",
			Version:     "0.8.4",
			Description: "读取、写入与整理本地文件",
			IconText:    "文",
			SourceType:  "local",
			Source:      `D:\bowknot-plugins\file-system`,
			InstalledAt: now - 4*day,
			AgentIDs:    []string{"main-agent", "coding"},
			Tools: []contract.PluginTool{
				{Name: "listFiles", Description: "列出指定目录下的文件与子目录"},
				{Name: "readFile", Description: "读取文本文件内容"},
				{Name: "writeFile", Description: "写入或覆盖文本文件", Dangerous: true},
			},
		},
		{
			ID:          "code-runner",
			Name:        "代码执行",
			Version:     "1.0.1",
			Description: "在受限环境中运行脚本与命令",
			IconText:    "码",
			SourceType:  "local",
			Source:      `D:\bowknot-plugins\code-runner`,
			InstalledAt: now - 2*day,
			AgentIDs:    []string{"coding"},
			Tools: []contract.PluginTool{
				{Name: "runScript", Description: "运行一段脚本并返回输出"},
				{Name: "runCommand", Description: "执行一条本地命令", Dangerous: true},
			},
		},
		{
			ID:          "image-gen",
			Name:        "图像生成",
			Version:     "2.1.0",
			Description: "根据描述生成或编辑图片",
			IconText:    "图",
			SourceType:  "remote",
			Source:      "https://plugins.bowknot.dev/image-gen",
			InstalledAt: now - 16*day,
			AgentIDs:    []string{},
			Tools: []contract.PluginTool{
				{Name: "generateImage", Description: "根据文字描述生成图片"},
				{Name: "editImage", Description: "按指令修改已有图片", Dangerous: true},
			},
		},
		{
			ID:          "calendar",
			Name:        "日历",
			Version:     "1.0.0",
			Description: "查看日程、创建提醒与处理时间冲突",
			IconText:    "历",
			SourceType:  "local",
			Source:      `D:\bowknot-plugins\calendar`,
			InstalledAt: now - 1*day,
			AgentIDs:    []string{"main-agent", "schedule"},
			Tools: []contract.PluginTool{
				{Name: "listEvents", Description: "列出指定时间范围内的日程"},
				{Name: "createEvent", Description: "创建一条日程或提醒"},
				{Name: "deleteEvent", Description: "删除指定日程", Dangerous: true},
			},
		},
	}

	return &pluginService{plugins: plugins, agents: agents}
}

func (s *pluginService) Plugins() []contract.Plugin {
	out := make([]contract.Plugin, len(s.plugins))
	for i, p := range s.plugins {
		out[i] = p
		out[i].Tools = append([]contract.PluginTool(nil), p.Tools...)
		out[i].AgentIDs = append([]string{}, p.AgentIDs...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InstalledAt > out[j].InstalledAt })
	return out
}

func (s *pluginService) Agents() []contract.PluginAgent {
	return append([]contract.PluginAgent(nil), s.agents...)
}

func (s *pluginService) Install(source contract.InstallSource) error {
	value := strings.TrimSpace(source.Value)
	if value == "" {
		if source.Type == "local" {
			return errors.New("请输入插件包路径")
		}
		return errors.New("请输入远程插件包地址")
	}
	if source.Type == "remote" && !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return errors.New("请输入以 http:// 或 https:// 开头的地址")
	}
	if strings.Contains(strings.ToLower(value), "invalid") {
		return errors.New("插件包缺少有效的 manifest.json")
	}

	manifest := s.manifestFor(value)
	for _, p := range s.plugins {
		if p.ID == manifest.ID {
			return errors.New("该插件已安装")
		}
	}

	s.plugins = append(s.plugins, contract.Plugin{
		ID:          manifest.ID,
		Name:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		IconText:    firstRune(manifest.Name),
		SourceType:  source.Type,
		Source:      value,
		InstalledAt: time.Now().UnixMilli(),
		Tools:       manifest.Tools,
		AgentIDs:    []string{},
	})
	return nil
}

func (s *pluginService) Uninstall(id string) error {
	for i, p := range s.plugins {
		if p.ID == id {
			s.plugins = append(s.plugins[:i], s.plugins[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("插件不存在：%s", id)
}

func (s *pluginService) SetAuthorizedAgents(id string, agentIDs []string) error {
	for i, p := range s.plugins {
		if p.ID != id {
			continue
		}
		known := make(map[string]bool, len(s.agents))
		for _, agent := range s.agents {
			known[agent.ID] = true
		}
		next := make([]string, 0, len(agentIDs))
		for _, agentID := range agentIDs {
			if known[agentID] {
				next = append(next, agentID)
			}
		}
		s.plugins[i].AgentIDs = next
		return nil
	}
	return fmt.Errorf("插件不存在：%s", id)
}

type pluginManifest struct {
	ID          string
	Name        string
	Version     string
	Description string
	Tools       []contract.PluginTool
}

func (s *pluginService) manifestFor(value string) pluginManifest {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "translator") || strings.Contains(value, "翻译"):
		return pluginManifest{
			ID:          "translator",
			Name:        "翻译助手",
			Version:     "1.0.0",
			Description: "在多语言之间翻译文本，并保留原文语气",
			Tools: []contract.PluginTool{
				{Name: "translateText", Description: "把文本翻译为目标语言"},
				{Name: "detectLanguage", Description: "识别一段文本的语言"},
			},
		}
	case strings.Contains(lower, "browser") || strings.Contains(value, "浏览器"):
		return pluginManifest{
			ID:          "browser-automation",
			Name:        "浏览器自动化",
			Version:     "0.6.3",
			Description: "打开网页、点击元素并截取页面截图",
			Tools: []contract.PluginTool{
				{Name: "openPage", Description: "打开指定网页"},
				{Name: "clickElement", Description: "点击页面中的指定元素"},
				{Name: "screenshot", Description: "截取当前页面截图并返回文件", Dangerous: true},
			},
		}
	}
	s.seq++
	return pluginManifest{
		ID:          fmt.Sprintf("plugin-demo-%d", s.seq),
		Name:        "示例插件",
		Version:     "1.0.0",
		Description: "从插件包安装的插件，能力由插件包定义",
		Tools: []contract.PluginTool{
			{Name: "runTask", Description: "执行插件定义的一项任务"},
			{Name: "resetData", Description: "重置插件维护的数据", Dangerous: true},
		},
	}
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return "?"
}
