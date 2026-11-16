package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	pkgs "bowknot-workbench/internal/infra/package"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/store"
)

// AppCatalog 是 Apps 页的持久化实现：列表来自 SQLite，
// 安装、卸载与数据清理统一交给包安装器。
type AppCatalog struct {
	store     *store.Store
	installer *pkgs.Installer
	tools     *tool.Service
	logger    *slog.Logger
}

func NewAppCatalog(st *store.Store, installer *pkgs.Installer, tools *tool.Service, logger *slog.Logger) *AppCatalog {
	if logger == nil {
		logger = slog.Default()
	}
	return &AppCatalog{
		store:     st,
		installer: installer,
		tools:     tools,
		logger:    logger,
	}
}

// Apps 读取已安装应用；contract 接口没有 error，读取失败时记录日志并返回空列表。
func (s *AppCatalog) Apps() []contract.AppItem {
	items, err := s.store.Apps.List(context.Background())
	if err != nil {
		s.logger.Error("读取应用列表失败", "error", err)
		return nil
	}
	out := make([]contract.AppItem, 0, len(items))
	for _, item := range items {
		out = append(out, toAppItem(item))
	}
	return out
}

func (s *AppCatalog) Preview(source contract.InstallSource) (contract.AppPreview, error) {
	normalized, err := normalizeSource(source, "应用")
	if err != nil {
		return contract.AppPreview{}, err
	}
	preview, err := s.installer.Preview(context.Background(), pkgs.KindApp, normalized.Type, normalized.Value)
	if err != nil {
		return contract.AppPreview{}, err
	}
	return contract.AppPreview{
		Manifest: toAppManifest(preview.Manifest),
		Source:   normalized,
	}, nil
}

// Install 安装预览过的应用：安装前重新解析来源，防止预览后包内容被替换；
// 安装成功后同步工具注册表。
func (s *AppCatalog) Install(preview contract.AppPreview) error {
	if strings.TrimSpace(preview.Manifest.ID) == "" {
		return errors.New("应用包信息不完整，请重新解析后再安装")
	}

	ctx := context.Background()
	current, err := s.installer.Preview(ctx, pkgs.KindApp, preview.Source.Type, preview.Source.Value)
	if err != nil {
		return err
	}
	if current.Manifest.ID != preview.Manifest.ID || current.Manifest.Version != preview.Manifest.Version {
		return errors.New("应用包内容已变化，请重新解析后再安装")
	}

	if _, err := s.installer.Install(ctx, pkgs.KindApp, preview.Source.Type, preview.Source.Value); err != nil {
		return installError(err, "应用")
	}
	s.syncTools()
	return nil
}

func (s *AppCatalog) Uninstall(id string, keepData bool) error {
	if err := s.installer.Uninstall(context.Background(), pkgs.KindApp, id, keepData); err != nil {
		return installError(err, "应用")
	}
	s.syncTools()
	return nil
}

func (s *AppCatalog) SetToolEnabled(id string, toolName string, enabled bool) error {
	if err := s.store.Apps.SetToolEnabled(context.Background(), id, toolName, enabled); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("能力不存在：%s", toolName)
		}
		return fmt.Errorf("更新工具开关失败：%w", err)
	}
	s.syncTools()
	return nil
}

func (s *AppCatalog) ClearData(id string) error {
	if err := s.installer.ClearData(context.Background(), pkgs.KindApp, id); err != nil {
		return installError(err, "应用")
	}
	return nil
}

func (s *AppCatalog) MarkUsed(id string) error {
	if err := s.store.Apps.MarkUsed(context.Background(), id, time.Now().UnixMilli()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("应用不存在：%s", id)
		}
		return fmt.Errorf("更新应用使用时间失败：%w", err)
	}
	return nil
}

func (s *AppCatalog) syncTools() {
	if s.tools == nil {
		return
	}
	if err := s.tools.SyncApps(context.Background()); err != nil {
		s.logger.Error("同步应用工具失败", "error", err)
	}
}

func toAppItem(item domain.App) contract.AppItem {
	tools := make([]contract.AppTool, 0, len(item.Tools))
	for _, t := range item.Tools {
		tools = append(tools, contract.AppTool{
			Name:        t.Name,
			Description: t.Description,
			Dangerous:   t.Dangerous,
			Enabled:     t.Enabled,
		})
	}
	return contract.AppItem{
		ID:          item.ID,
		Name:        item.Name,
		DisplayName: item.DisplayName,
		Version:     item.Version,
		Description: item.Description,
		IconText:    item.IconText,
		SourceType:  item.SourceType,
		Source:      item.SourceValue,
		Entry:       item.Entry,
		InstallDir:  item.InstallDir,
		InstalledAt: item.InstalledAt,
		LastUsedAt:  item.LastUsedAt,
		DataSize:    item.DataSize,
		Tools:       tools,
	}
}

func toAppManifest(manifest pkgs.Manifest) contract.AppManifest {
	tools := make([]contract.AppTool, 0, len(manifest.Tools))
	for _, t := range manifest.Tools {
		tools = append(tools, contract.AppTool{
			Name:        t.Name,
			Description: t.Description,
			Dangerous:   t.Dangerous,
			Enabled:     true,
		})
	}
	return contract.AppManifest{
		ID:          manifest.ID,
		Name:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		Tools:       tools,
	}
}

// normalizeSource 校验安装来源并去掉首尾空白。
func normalizeSource(source contract.InstallSource, kindName string) (contract.InstallSource, error) {
	value := strings.TrimSpace(source.Value)
	if value == "" {
		if source.Type == pkgs.SourceTypeRemote {
			return contract.InstallSource{}, fmt.Errorf("请输入远程%s包地址", kindName)
		}
		return contract.InstallSource{}, fmt.Errorf("请输入%s包路径", kindName)
	}
	switch source.Type {
	case pkgs.SourceTypeLocal, pkgs.SourceTypeRemote:
	default:
		return contract.InstallSource{}, fmt.Errorf("未知的安装来源类型：%s", source.Type)
	}
	if source.Type == pkgs.SourceTypeRemote &&
		!strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return contract.InstallSource{}, errors.New("请输入以 http:// 或 https:// 开头的地址")
	}
	return contract.InstallSource{Type: source.Type, Value: value}, nil
}

// installError 把安装器的内部错误转换为面向用户的中文提示。
func installError(err error, kindName string) error {
	if errors.Is(err, pkgs.ErrAlreadyInstalled) {
		return fmt.Errorf("该%s已安装相同版本", kindName)
	}
	return err
}
