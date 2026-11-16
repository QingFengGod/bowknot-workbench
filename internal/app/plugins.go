package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	pkgs "bowknot-workbench/internal/infra/package"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/store"
)

// mainAgentID 是系统内置主 Agent 的固定标识，插件授权与种子数据共用该 ID。
const mainAgentID = tool.MainAssistantAgentID

// PluginService 是插件页的持久化实现：插件、工具与授权关系来自 SQLite，
// 安装与卸载统一交给包安装器。
type PluginService struct {
	store     *store.Store
	installer *pkgs.Installer
	tools     *tool.Service
	logger    *slog.Logger
}

func NewPluginService(st *store.Store, installer *pkgs.Installer, tools *tool.Service, logger *slog.Logger) *PluginService {
	if logger == nil {
		logger = slog.Default()
	}
	return &PluginService{
		store:     st,
		installer: installer,
		tools:     tools,
		logger:    logger,
	}
}

// Plugins 读取已安装插件；contract 接口没有 error，读取失败时记录日志并返回空列表。
func (s *PluginService) Plugins() []contract.Plugin {
	items, err := s.store.Plugins.List(context.Background())
	if err != nil {
		s.logger.Error("读取插件列表失败", "error", err)
		return nil
	}
	out := make([]contract.Plugin, 0, len(items))
	for _, item := range items {
		out = append(out, toPlugin(item))
	}
	return out
}

// Agents 返回内置主 Agent 与用户创建的 Agent，供插件授权使用。
func (s *PluginService) Agents() []contract.PluginAgent {
	out := []contract.PluginAgent{{
		ID:          mainAgentID,
		Name:        "助理",
		Description: "系统内置主 Agent，负责日常对话与群聊任务派发",
		AvatarText:  "助",
		IsMain:      true,
	}}

	items, err := s.store.Agents.List(context.Background())
	if err != nil {
		s.logger.Error("读取 Agent 列表失败", "error", err)
		return out
	}
	for _, item := range items {
		out = append(out, contract.PluginAgent{
			ID:          item.ID,
			Name:        item.Name,
			Description: item.Description,
			AvatarText:  item.AvatarText,
		})
	}
	return out
}

func (s *PluginService) Install(source contract.InstallSource) error {
	normalized, err := normalizeSource(source, "插件")
	if err != nil {
		return err
	}
	if _, err := s.installer.Install(context.Background(), pkgs.KindPlugin, normalized.Type, normalized.Value); err != nil {
		return installError(err, "插件")
	}
	s.syncTools()
	return nil
}

func (s *PluginService) Uninstall(id string) error {
	if err := s.installer.Uninstall(context.Background(), pkgs.KindPlugin, id, false); err != nil {
		return installError(err, "插件")
	}
	s.syncTools()
	return nil
}

// SetAuthorizedAgents 覆盖插件的授权 Agent 列表；未知 Agent 会被忽略，
// 写入成功后同步工具注册表。
func (s *PluginService) SetAuthorizedAgents(id string, agentIDs []string) error {
	known := make(map[string]bool)
	for _, agent := range s.Agents() {
		known[agent.ID] = true
	}
	next := make([]string, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		if known[agentID] {
			next = append(next, agentID)
		}
	}

	if err := s.store.Plugins.SetGrants(context.Background(), id, next); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("插件不存在：%s", id)
		}
		return fmt.Errorf("更新插件授权失败：%w", err)
	}
	s.syncTools()
	return nil
}

func (s *PluginService) syncTools() {
	if s.tools == nil {
		return
	}
	if err := s.tools.SyncPlugins(context.Background()); err != nil {
		s.logger.Error("同步插件工具失败", "error", err)
	}
}

func toPlugin(item domain.Plugin) contract.Plugin {
	tools := make([]contract.PluginTool, 0, len(item.Tools))
	for _, t := range item.Tools {
		tools = append(tools, contract.PluginTool{
			Name:        t.Name,
			Description: t.Description,
			Dangerous:   t.Dangerous,
		})
	}
	return contract.Plugin{
		ID:          item.ID,
		Name:        item.Name,
		Version:     item.Version,
		Description: item.Description,
		IconText:    item.IconText,
		SourceType:  item.SourceType,
		Source:      item.SourceValue,
		InstalledAt: item.InstalledAt,
		Tools:       tools,
		AgentIDs:    append([]string{}, item.AgentIDs...),
	}
}
