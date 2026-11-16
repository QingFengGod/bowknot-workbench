// Package builtin 携带随包内置的应用与插件 tar 包，
// 正式版首次启动时通过 package.Installer 安装，开发版的演示数据由 store 种子提供。
package builtin

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	pkgs "bowknot-workbench/internal/infra/package"
	"bowknot-workbench/internal/store"
)

//go:embed all:apps all:plugins
var embedded embed.FS

// markerKey 与 markerValue 标记内置包已安装，后续启动不重复导入。
const (
	markerKey   = "builtin.seed_version"
	markerValue = "builtin.v1"
)

// Files 返回内置包文件系统，根目录含 apps/ 与 plugins/。
func Files() fs.FS {
	return embedded
}

// EnsureInstalled 安装 embed 的内置包；没有内置包或已完成首装时直接返回。
// 安装失败的包会记录日志但不阻塞启动，下次启动重试（标记只在全部成功后写入）。
func EnsureInstalled(ctx context.Context, st *store.Store, installer *pkgs.Installer, logger *slog.Logger) error {
	return ensureFrom(ctx, st, installer, logger, embedded)
}

func ensureFrom(ctx context.Context, st *store.Store, installer *pkgs.Installer, logger *slog.Logger, files fs.FS) error {
	if logger == nil {
		logger = slog.Default()
	}
	if value, err := st.Meta.Get(ctx, markerKey); err == nil && value == markerValue {
		return nil
	}

	total := 0
	installed := 0
	for _, spec := range []struct {
		dir  string
		kind string
	}{
		{dir: "apps", kind: pkgs.KindApp},
		{dir: "plugins", kind: pkgs.KindPlugin},
	} {
		entries, err := fs.ReadDir(files, spec.dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			// 只把 *.tar 视为内置包，忽略目录与占位文件。
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar") {
				continue
			}
			total++
			file, err := files.Open(spec.dir + "/" + entry.Name())
			if err != nil {
				logger.Warn("打开内置包失败", "package", entry.Name(), "error", err)
				continue
			}
			_, installErr := installer.InstallArchive(ctx, spec.kind, pkgs.SourceTypeBuiltin, entry.Name(), file)
			file.Close()
			if installErr != nil {
				// 已安装相同版本属于正常重入，其余失败记录日志等待重试。
				if !errors.Is(installErr, pkgs.ErrAlreadyInstalled) {
					logger.Warn("安装内置包失败", "package", entry.Name(), "error", installErr)
					continue
				}
			}
			installed++
		}
	}

	if total == 0 {
		return nil
	}
	if installed < total {
		return fmt.Errorf("内置包安装未完成：%d/%d", installed, total)
	}
	if err := st.Meta.Set(ctx, markerKey, markerValue); err != nil {
		return fmt.Errorf("写入内置包标记失败：%w", err)
	}
	logger.Info("内置包安装完成", "count", installed)
	return nil
}
