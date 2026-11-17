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
	platformPattern = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)
	// 单次工具执行超时上限，避免 manifest 写出半小时的长任务。
	maxToolTimeoutMS = 600000
)

// ManifestTool 是 manifest.json 里的一个工具声明。
type ManifestTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Dangerous   bool   `json:"dangerous"`
	// Parameters 是可选的 JSON Schema 参数声明，为空时按自由对象传给模型。
	Parameters json.RawMessage `json:"parameters"`
	// TimeoutMS 是单次执行超时（毫秒），0 表示使用工具服务默认值。
	TimeoutMS int `json:"timeoutMs"`
}

// ManifestRuntime 声明插件的执行方式：
//   - native：按平台提供可执行文件，宿主以 stdio JSON-RPC 启动；
//   - bun：提供 JS 入口，由内置 Bun 宿主加载。
type ManifestRuntime struct {
	Type string `json:"type"`
	// Entry 是 bun 插件的入口文件，相对包根目录。
	Entry string `json:"entry"`
	// Commands 是 native 插件按 "GOOS-GOARCH" 声明的可执行文件。
	Commands map[string]string `json:"commands"`
}

// ManifestPermissions 是插件声明的能力边界，安装后随记录保存。
type ManifestPermissions struct {
	FS        []string `json:"fs"`
	Net       []string `json:"net"`
	Host      []string `json:"host"`
	TimeoutMS int      `json:"timeoutMs"`
}

// Manifest 是应用与插件共用的包清单。
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	// Entry 是应用入口文件，插件可为空。
	Entry       string              `json:"entry"`
	Runtime     ManifestRuntime     `json:"runtime"`
	Permissions ManifestPermissions `json:"permissions"`
	Tools       []ManifestTool      `json:"tools"`
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
		if tool.TimeoutMS < 0 || tool.TimeoutMS > maxToolTimeoutMS {
			return fmt.Errorf("包 %s 的工具 %s 超时必须在 0-%d 毫秒之间", m.ID, tool.Name, maxToolTimeoutMS)
		}
		if len(tool.Parameters) > 0 {
			var schema map[string]any
			if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
				return fmt.Errorf("包 %s 的工具 %s 参数不是合法的 JSON Schema：%w", m.ID, tool.Name, err)
			}
		}
	}

	if err := m.Runtime.validate(m.ID, m.Entry); err != nil {
		return err
	}
	if m.Permissions.TimeoutMS < 0 || m.Permissions.TimeoutMS > maxToolTimeoutMS {
		return fmt.Errorf("包 %s 的 permissions.timeoutMs 必须在 0-%d 之间", m.ID, maxToolTimeoutMS)
	}
	return nil
}

// validate 校验运行方式声明。空 runtime 视为旧包，允许安装，
// 但执行时会因缺少运行方式而失败。
func (r ManifestRuntime) validate(id, entry string) error {
	switch r.Type {
	case "":
		return nil
	case "native":
		if len(r.Commands) == 0 {
			return fmt.Errorf("包 %s 的 native 运行方式必须声明 commands", id)
		}
		for platform, command := range r.Commands {
			if !platformPattern.MatchString(platform) {
				return fmt.Errorf("包 %s 的平台标识无效：%q（示例：windows-amd64）", id, platform)
			}
			if err := validateRelativePath(id, "runtime.commands."+platform, command); err != nil {
				return err
			}
		}
		return nil
	case "bun":
		if strings.TrimSpace(r.Entry) == "" {
			return fmt.Errorf("包 %s 的 bun 运行方式必须声明 runtime.entry", id)
		}
		return validateRelativePath(id, "runtime.entry", r.Entry)
	default:
		return fmt.Errorf("包 %s 的运行方式未知：%q（只支持 native / bun）", id, r.Type)
	}
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
