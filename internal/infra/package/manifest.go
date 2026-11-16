// Package pkgs 实现应用与插件包的 manifest 解析、安全解包、下载与安装卸载。
// 远程包为 tar 归档，manifest.json 位于归档根目录；解包只接受普通文件与目录。
package pkgs

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// 包 ID、语义化版本与工具名的校验规则，见基础设施计划 5.5。
var (
	idPattern       = regexp.MustCompile(`^[a-z0-9.-]{1,64}$`)
	versionPattern  = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	toolNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
)

// ManifestTool 是 manifest.json 里的一个工具声明。
type ManifestTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Dangerous   bool   `json:"dangerous"`
}

// Manifest 是应用与插件共用的包清单。
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	// Entry 是应用入口文件，插件可为空。
	Entry string         `json:"entry"`
	Tools []ManifestTool `json:"tools"`
}

// ParseManifest 解析并校验 manifest.json 内容。
func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("解析 manifest.json 失败：%w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate 校验清单字段，规则见基础设施计划 5.5。
func (m Manifest) Validate() error {
	if !idPattern.MatchString(m.ID) || m.ID == "." || m.ID == ".." {
		return fmt.Errorf("包 ID 无效（只允许小写字母、数字、.、-，长度 1-64）：%q", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("包 %s 的 name 不能为空", m.ID)
	}
	if !versionPattern.MatchString(strings.TrimPrefix(m.Version, "v")) {
		return fmt.Errorf("包 %s 的版本号不是语义化版本：%q", m.ID, m.Version)
	}
	if m.Entry != "" {
		if err := validateRelativePath(m.ID, "entry", m.Entry); err != nil {
			return err
		}
	}

	seen := make(map[string]bool, len(m.Tools))
	for i, tool := range m.Tools {
		if !toolNamePattern.MatchString(tool.Name) {
			return fmt.Errorf("包 %s 第 %d 个工具名不符合标识符规则：%q", m.ID, i+1, tool.Name)
		}
		if seen[tool.Name] {
			return fmt.Errorf("包 %s 的工具名重复：%s", m.ID, tool.Name)
		}
		seen[tool.Name] = true
	}
	return nil
}

// validateRelativePath 拒绝绝对路径与路径穿越，用于 manifest 内的文件字段。
func validateRelativePath(id, field, value string) error {
	if strings.HasPrefix(value, "/") || filepath.IsAbs(value) || strings.Contains(value, "\\") {
		return fmt.Errorf("包 %s 的 %s 必须是相对路径：%q", id, field, value)
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return fmt.Errorf("包 %s 的 %s 不允许包含 ..：%q", id, field, value)
		}
	}
	return nil
}
