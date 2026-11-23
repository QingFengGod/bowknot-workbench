// Package paths 负责解析与创建工作台的数据目录。
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	appDirName       = ".bowknot-workbench"
	databaseName     = "bowknot.db"
	configName       = "config.json"
	appsDirName      = "apps"
	pluginsDirName   = "plugins"
	logsDirName      = "logs"
	tempDirName      = "tmp"
	workspaceDirName = "workspace"
	artifactsDirName = "artifacts"
)

// Paths 是一次解析结果，所有字段都是绝对路径。
type Paths struct {
	Root     string
	Database string
	Config   string
	Apps     string
	Plugins  string
	Logs     string
	Temp     string
	// Workspace 是 Agent 产出的统一根目录，所有文件读写都限制在它下面。
	Workspace string
	// Artifacts 是工具托管文件的落盘目录，位于 Workspace 内。
	Artifacts string
}

// Resolve 按 BOWKNOT_HOME、用户目录的顺序确定数据根目录。
func Resolve() (Paths, error) {
	root := strings.TrimSpace(os.Getenv("BOWKNOT_HOME"))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("无法定位用户目录：%w", err)
		}
		root = filepath.Join(home, appDirName)
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, fmt.Errorf("解析数据目录失败：%w", err)
	}

	return Paths{
		Root:      abs,
		Database:  filepath.Join(abs, databaseName),
		Config:    filepath.Join(abs, configName),
		Apps:      filepath.Join(abs, appsDirName),
		Plugins:   filepath.Join(abs, pluginsDirName),
		Logs:      filepath.Join(abs, logsDirName),
		Temp:      filepath.Join(abs, tempDirName),
		Workspace: filepath.Join(abs, workspaceDirName),
		Artifacts: filepath.Join(abs, workspaceDirName, artifactsDirName),
	}, nil
}

// Prepare 创建运行所需目录，已存在时保持原样。
func (p Paths) Prepare() error {
	dirs := []string{p.Root, p.Logs, p.Apps, p.Plugins, p.Temp, p.Workspace}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败：%w", dir, err)
		}
	}
	// 先迁移旧目录再创建目标目录，否则目标已存在会导致迁移被跳过。
	p.migrateLegacyArtifacts()
	if err := os.MkdirAll(p.Artifacts, 0o755); err != nil {
		return fmt.Errorf("创建目录 %s 失败：%w", p.Artifacts, err)
	}
	return nil
}

// migrateLegacyArtifacts 把旧版数据根下的 artifacts 目录搬进 workspace，
// 避免升级后历史托管文件从文件库里消失。失败时不阻塞启动，原目录保持不动。
func (p Paths) migrateLegacyArtifacts() {
	legacy := filepath.Join(p.Root, artifactsDirName)
	if _, err := os.Stat(legacy); err != nil {
		return
	}
	if _, err := os.Stat(p.Artifacts); err == nil {
		return
	}
	_ = os.Rename(legacy, p.Artifacts)
}

// CheckWritable 启动自检：确认数据根与日志目录可写，提前暴露权限问题
// （杀毒软件、漫游配置、磁盘满等），避免运行中途才失败。
func (p Paths) CheckWritable() error {
	dirs := []string{p.Root, p.Logs, p.Temp, p.Workspace}
	for _, dir := range dirs {
		probe := filepath.Join(dir, ".write-probe")
		if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
			return fmt.Errorf("目录 %s 不可写：%w", dir, err)
		}
		if err := os.Remove(probe); err != nil {
			return fmt.Errorf("目录 %s 自检探针清理失败：%w", dir, err)
		}
	}
	return nil
}
