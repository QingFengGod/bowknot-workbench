package pkgs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bowknot-workbench/internal/platform/id"
)

// previewTimeout 限制远程预览的下载与解包时间，避免 UI 长时间等待。
const previewTimeout = 2 * time.Minute

// Preview 是安装前的包预览结果。
type Preview struct {
	Kind        string
	Manifest    Manifest
	SourceType  string
	SourceValue string
	// Size 是解包后包内容的字节数。
	Size int64
}

// Preview 解析本地目录、本地 tar 或远程 tar 的 manifest。
// 复用 Install 的下载、解包与校验逻辑，但不写数据库、不移动包目录，
// 临时目录在返回前删除。
func (i *Installer) Preview(ctx context.Context, kind, sourceType, sourceValue string) (Preview, error) {
	if err := i.ensureDirs(); err != nil {
		return Preview{}, err
	}
	staging := filepath.Join(i.opts.TempRoot, "preview-"+id.New())
	defer os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return Preview{}, fmt.Errorf("创建预览目录失败：%w", err)
	}
	if err := i.stage(ctx, sourceType, sourceValue, staging); err != nil {
		return Preview{}, err
	}

	data, err := os.ReadFile(filepath.Join(staging, "manifest.json"))
	if err != nil {
		return Preview{}, fmt.Errorf("包内缺少 manifest.json：%w", err)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return Preview{}, err
	}
	size, err := MeasureDir(staging)
	if err != nil {
		return Preview{}, fmt.Errorf("统计包体积失败：%w", err)
	}
	return Preview{
		Kind:        kind,
		Manifest:    manifest,
		SourceType:  sourceType,
		SourceValue: sourceValue,
		Size:        size,
	}, nil
}

// stage 把来源内容解包到 staging，预览与安装共用同一套解包规则。
func (i *Installer) stage(ctx context.Context, sourceType, sourceValue, staging string) error {
	switch sourceType {
	case SourceTypeLocal:
		info, err := os.Stat(sourceValue)
		if err != nil {
			return fmt.Errorf("读取安装来源失败：%w", err)
		}
		if info.IsDir() {
			return CopyTree(sourceValue, staging, i.opts.MaxSize)
		}
		file, err := os.Open(sourceValue)
		if err != nil {
			return fmt.Errorf("打开 tar 包失败：%w", err)
		}
		defer file.Close()
		return ExtractTar(file, staging, i.opts.MaxSize)

	case SourceTypeRemote:
		runCtx, cancel := context.WithTimeout(ctx, previewTimeout)
		defer cancel()
		downloadPath := filepath.Join(i.opts.TempRoot, "preview-"+id.New()+".tar")
		defer os.Remove(downloadPath)
		if err := Download(runCtx, sourceValue, downloadPath, "", i.opts.MaxSize, i.opts.HTTPClient); err != nil {
			return err
		}
		file, err := os.Open(downloadPath)
		if err != nil {
			return fmt.Errorf("读取下载结果失败：%w", err)
		}
		defer file.Close()
		return ExtractTar(file, staging, i.opts.MaxSize)

	default:
		return fmt.Errorf("未知的安装来源类型：%s", sourceType)
	}
}
