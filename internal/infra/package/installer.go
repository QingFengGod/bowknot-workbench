package pkgs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/platform/id"
	"bowknot-workbench/internal/store"
)

// 包类型与来源类型。
const (
	KindApp    = "app"
	KindPlugin = "plugin"

	SourceTypeLocal   = "local"
	SourceTypeRemote  = "remote"
	SourceTypeBuiltin = "builtin"
)

// MaxPackageSize 是下载与解包的默认体积上限（200MB）。
const MaxPackageSize int64 = 200 << 20

// ErrAlreadyInstalled 表示相同 ID 与版本的包已安装。
var ErrAlreadyInstalled = errors.New("already installed")

// Options 控制 Installer 行为，零值字段使用默认值。
type Options struct {
	Store       *store.Store
	AppsRoot    string // <root>/apps，包内容在 apps/<id>/<version>/
	PluginsRoot string // <root>/plugins
	TempRoot    string // <root>/tmp，下载与解包临时目录
	Logger      *slog.Logger
	HTTPClient  *http.Client
	// MaxSize 限制下载与解包总体积，默认 200MB。
	MaxSize int64
}

// Installer 负责包的安装、升级、卸载与数据目录管理。
// 安装流程：暂存目录解包 → 校验 manifest → 原子移动到版本目录 →
// 同一事务写入记录；任一步失败都清理临时目录与半成品目录。
type Installer struct {
	opts Options
}

func NewInstaller(opts Options) *Installer {
	if opts.MaxSize <= 0 {
		opts.MaxSize = MaxPackageSize
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Installer{opts: opts}
}

// Install 安装本地目录、本地 tar 文件或远程 tar 包。
// sourceType 取 local / remote；local 的 value 是目录或 .tar 文件路径，
// remote 的 value 是下载 URL。
func (i *Installer) Install(ctx context.Context, kind, sourceType, value string) (Manifest, error) {
	switch sourceType {
	case SourceTypeLocal:
		info, err := os.Stat(value)
		if err != nil {
			return Manifest{}, fmt.Errorf("读取安装来源失败：%w", err)
		}
		if info.IsDir() {
			return i.installLocalDir(ctx, kind, value)
		}
		file, err := os.Open(value)
		if err != nil {
			return Manifest{}, fmt.Errorf("打开 tar 包失败：%w", err)
		}
		defer file.Close()
		return i.InstallArchive(ctx, kind, SourceTypeLocal, value, file)

	case SourceTypeRemote:
		downloadPath := filepath.Join(i.opts.TempRoot, "download-"+id.New()+".tar")
		if err := Download(ctx, value, downloadPath, "", i.opts.MaxSize, i.opts.HTTPClient); err != nil {
			return Manifest{}, err
		}
		file, err := os.Open(downloadPath)
		if err != nil {
			return Manifest{}, fmt.Errorf("读取下载结果失败：%w", err)
		}
		manifest, installErr := i.InstallArchive(ctx, kind, SourceTypeRemote, value, file)
		file.Close()
		os.Remove(downloadPath)
		return manifest, installErr

	default:
		return Manifest{}, fmt.Errorf("未知的安装来源类型：%s", sourceType)
	}
}

// installLocalDir 复制本地目录到暂存区后走统一安装流程。
func (i *Installer) installLocalDir(ctx context.Context, kind, dir string) (Manifest, error) {
	if err := i.ensureDirs(); err != nil {
		return Manifest{}, err
	}
	staging := filepath.Join(i.opts.TempRoot, "staging-"+id.New())
	defer os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("创建暂存目录失败：%w", err)
	}
	if err := CopyTree(dir, staging, i.opts.MaxSize); err != nil {
		return Manifest{}, err
	}
	manifest, err := i.finalize(ctx, kind, SourceTypeLocal, dir, staging)
	if err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// InstallArchive 从 tar 流安装一个包，内置包与本地/远程安装共用该入口。
func (i *Installer) InstallArchive(ctx context.Context, kind, sourceType, sourceValue string, r io.Reader) (Manifest, error) {
	if err := i.ensureDirs(); err != nil {
		return Manifest{}, err
	}
	staging := filepath.Join(i.opts.TempRoot, "staging-"+id.New())
	defer os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("创建暂存目录失败：%w", err)
	}
	if err := ExtractTar(r, staging, i.opts.MaxSize); err != nil {
		return Manifest{}, err
	}
	return i.finalize(ctx, kind, sourceType, sourceValue, staging)
}

// finalize 校验 manifest、处理版本冲突、原子移动并写入 store 记录。
func (i *Installer) finalize(ctx context.Context, kind, sourceType, sourceValue, staging string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(staging, "manifest.json"))
	if err != nil {
		return Manifest{}, fmt.Errorf("包内缺少 manifest.json：%w", err)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, err
	}

	// 冲突检查：同类型同版本拒绝；ID 被另一类型占用也拒绝。
	existing, err := i.getRecord(ctx, kind, manifest.ID)
	switch {
	case err == nil && existing.Version == manifest.Version:
		return Manifest{}, fmt.Errorf("%w：%s %s 已安装相同版本 %s", ErrAlreadyInstalled, kind, manifest.ID, manifest.Version)
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return Manifest{}, fmt.Errorf("查询已安装记录失败：%w", err)
	}
	// 插件升级时保留已有授权，首次安装保持空授权。
	var upgradeGrants []string
	otherKind := KindPlugin
	if kind == KindPlugin {
		otherKind = KindApp
		upgradeGrants = existing.AgentIDs
	}
	if _, err := i.getRecord(ctx, otherKind, manifest.ID); err == nil {
		return Manifest{}, fmt.Errorf("包 ID %s 已被%s占用", manifest.ID, kindName(otherKind))
	}

	target := i.versionDir(kind, manifest.ID, manifest.Version)
	if err := os.RemoveAll(target); err != nil {
		return Manifest{}, fmt.Errorf("清理旧版本目录失败：%w", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return Manifest{}, fmt.Errorf("创建安装目录失败：%w", err)
	}
	// 暂存目录与安装目录同在数据根下，rename 为原子操作。
	if err := os.Rename(staging, target); err != nil {
		return Manifest{}, fmt.Errorf("移动包目录失败：%w", err)
	}

	now := time.Now().UnixMilli()
	err = i.opts.Store.Tx(ctx, func(tx *store.Store) error {
		switch kind {
		case KindApp:
			item := domain.App{
				ID:          manifest.ID,
				Name:        manifest.ID,
				DisplayName: manifest.Name,
				Version:     manifest.Version,
				Description: manifest.Description,
				IconText:    firstRune(manifest.Name),
				SourceType:  sourceType,
				SourceValue: sourceValue,
				InstallDir:  target,
				Entry:       manifest.Entry,
				InstalledAt: now,
			}
			if err := tx.Apps.Upsert(ctx, item); err != nil {
				return err
			}
			return tx.Apps.ReplaceTools(ctx, manifest.ID, manifestTools(manifest.Tools, true))
		case KindPlugin:
			item := domain.Plugin{
				ID:          manifest.ID,
				Name:        manifest.Name,
				Version:     manifest.Version,
				Description: manifest.Description,
				IconText:    firstRune(manifest.Name),
				SourceType:  sourceType,
				SourceValue: sourceValue,
				InstallDir:  target,
				InstalledAt: now,
			}
			if err := tx.Plugins.Upsert(ctx, item); err != nil {
				return err
			}
			if err := tx.Plugins.ReplaceTools(ctx, manifest.ID, manifestTools(manifest.Tools, false)); err != nil {
				return err
			}
			return tx.Plugins.SetGrants(ctx, manifest.ID, upgradeGrants)
		default:
			return fmt.Errorf("未知的包类型：%s", kind)
		}
	})
	if err != nil {
		// 事务失败时回滚目录，避免留下无记录的半成品。
		os.RemoveAll(target)
		return Manifest{}, fmt.Errorf("写入安装记录失败：%w", err)
	}

	i.pruneVersions(kind, manifest.ID, manifest.Version)
	return manifest, nil
}

// Uninstall 删除安装记录后清理文件；文件删除失败只记日志，
// 不阻塞卸载，残留目录由下次启动的 SweepOldVersions 之外的清理兜底。
func (i *Installer) Uninstall(ctx context.Context, kind, id string, keepData bool) error {
	item, err := i.getRecord(ctx, kind, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%s %s 未安装", kindName(kind), id)
		}
		return fmt.Errorf("查询安装记录失败：%w", err)
	}

	err = i.opts.Store.Tx(ctx, func(tx *store.Store) error {
		if kind == KindApp {
			return tx.Apps.Delete(ctx, id)
		}
		return tx.Plugins.Delete(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("删除安装记录失败：%w", err)
	}

	// 数据库已删除，文件清理尽力而为；Windows 文件占用等失败下次清理。
	packageDir := i.packageDir(kind, id)
	if keepData && item.InstallDir != "" {
		packageDir = item.InstallDir
	}
	if err := os.RemoveAll(packageDir); err != nil {
		i.opts.Logger.Warn("清理包目录失败，将在后续清理中重试", "dir", packageDir, "error", err)
	}
	return nil
}

// ClearData 清空应用/插件的数据目录；应用记录的 DataSize 同步归零。
func (i *Installer) ClearData(ctx context.Context, kind, id string) error {
	if _, err := i.getRecord(ctx, kind, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%s %s 未安装", kindName(kind), id)
		}
		return fmt.Errorf("查询安装记录失败：%w", err)
	}

	dataDir := filepath.Join(i.packageDir(kind, id), "data")
	if err := os.RemoveAll(dataDir); err != nil {
		return fmt.Errorf("清理数据目录失败：%w", err)
	}
	if kind == KindApp {
		if err := i.opts.Store.Apps.SetDataSize(ctx, id, 0); err != nil {
			return fmt.Errorf("更新数据大小失败：%w", err)
		}
	}
	return nil
}

// CleanTemp 在启动时清空临时目录中的安装残留（暂存目录与下载文件）。
// 启动阶段没有并发安装，残留必然来自上次异常退出；删除失败只记日志。
func (i *Installer) CleanTemp() {
	entries, err := os.ReadDir(i.opts.TempRoot)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			i.opts.Logger.Warn("读取临时目录失败，跳过清理", "dir", i.opts.TempRoot, "error", err)
		}
		return
	}
	for _, entry := range entries {
		path := filepath.Join(i.opts.TempRoot, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			i.opts.Logger.Warn("清理安装包残留失败", "path", path, "error", err)
		}
	}
}

// SweepOldVersions 在启动时重试上次未完成的旧版本目录清理。
func (i *Installer) SweepOldVersions(ctx context.Context) {
	apps, err := i.opts.Store.Apps.List(ctx)
	if err != nil {
		i.opts.Logger.Warn("读取应用列表失败，跳过旧版本清理", "error", err)
		return
	}
	for _, app := range apps {
		i.pruneVersions(KindApp, app.ID, app.Version)
	}
	plugins, err := i.opts.Store.Plugins.List(ctx)
	if err != nil {
		i.opts.Logger.Warn("读取插件列表失败，跳过旧版本清理", "error", err)
		return
	}
	for _, plugin := range plugins {
		i.pruneVersions(KindPlugin, plugin.ID, plugin.Version)
	}
}

// pruneVersions 只保留当前版本与上一个版本，更早的版本目录在安装后清理；
// 删除失败不阻塞安装，下次启动重试。
func (i *Installer) pruneVersions(kind, id, current string) {
	entries, err := os.ReadDir(i.packageDir(kind, id))
	if err != nil {
		return
	}
	var versions []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "data" {
			versions = append(versions, entry.Name())
		}
	}
	sort.Slice(versions, func(a, b int) bool { return compareVersion(versions[a], versions[b]) > 0 })

	currentIndex := -1
	for idx, version := range versions {
		if version == current {
			currentIndex = idx
			break
		}
	}
	keep := currentIndex + 2 // 当前版本 + 上一个版本
	if keep < 2 {
		keep = 2
	}
	for idx := keep; idx < len(versions); idx++ {
		dir := i.versionDir(kind, id, versions[idx])
		if err := os.RemoveAll(dir); err != nil {
			i.opts.Logger.Warn("清理旧版本目录失败，将在下次启动重试", "dir", dir, "error", err)
		}
	}
}

func (i *Installer) ensureDirs() error {
	for _, dir := range []string{i.opts.AppsRoot, i.opts.PluginsRoot, i.opts.TempRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败：%w", dir, err)
		}
	}
	return nil
}

func (i *Installer) root(kind string) string {
	if kind == KindApp {
		return i.opts.AppsRoot
	}
	return i.opts.PluginsRoot
}

func (i *Installer) packageDir(kind, id string) string {
	return filepath.Join(i.root(kind), id)
}

func (i *Installer) versionDir(kind, id, version string) string {
	return filepath.Join(i.packageDir(kind, id), version)
}

func (i *Installer) getRecord(ctx context.Context, kind, id string) (record, error) {
	if kind == KindApp {
		item, err := i.opts.Store.Apps.Get(ctx, id)
		if err != nil {
			return record{}, err
		}
		return record{Version: item.Version, InstallDir: item.InstallDir, AgentIDs: nil}, nil
	}
	item, err := i.opts.Store.Plugins.Get(ctx, id)
	if err != nil {
		return record{}, err
	}
	return record{Version: item.Version, InstallDir: item.InstallDir, AgentIDs: item.AgentIDs}, nil
}

// record 是安装记录的最小视图，屏蔽应用与插件字段差异。
type record struct {
	Version    string
	InstallDir string
	AgentIDs   []string
}

// manifestTools 把 manifest 工具声明转换为 domain.Tool。
// 应用工具默认启用；插件工具没有全局开关，Enabled 由调用方决定。
func manifestTools(tools []ManifestTool, enabled bool) []domain.Tool {
	out := make([]domain.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, domain.Tool{
			Name:        t.Name,
			Description: t.Description,
			Dangerous:   t.Dangerous,
			Enabled:     enabled,
		})
	}
	return out
}

// compareVersion 比较语义化版本，返回 >0 / 0 / <0；无法解析的段按 0 处理。
func compareVersion(a, b string) int {
	pa, pb := splitVersion(a), splitVersion(b)
	for idx := 0; idx < 3; idx++ {
		if pa[idx] != pb[idx] {
			return pa[idx] - pb[idx]
		}
	}
	return 0
}

func splitVersion(version string) [3]int {
	version = strings.TrimPrefix(strings.TrimPrefix(version, "v"), "V")
	var out [3]int
	for idx, part := range strings.SplitN(version, ".", 3) {
		if idx >= 3 {
			break
		}
		if num, err := strconv.Atoi(part); err == nil {
			out[idx] = num
		}
	}
	return out
}

func firstRune(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return "?"
	}
	return string(runes[0])
}

func kindName(kind string) string {
	if kind == KindApp {
		return "应用"
	}
	return "插件"
}
