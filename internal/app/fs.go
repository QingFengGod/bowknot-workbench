package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"bowknot-workbench/internal/infra/tool"
)

const (
	fsMaxReadBytes   = 512 * 1024
	fsMaxWriteBytes  = 1024 * 1024
	fsMaxListLines   = 500
	fsMaxGlobResults = 500
	fsMaxSearchHits  = 300
	fsMaxSearchFile  = 1024 * 1024
)

// FSService 执行内置文件系统工具：所有路径都限制在工作区根目录内，
// 模型使用相对路径即可，避免把数据目录、用户主目录混进对话。
type FSService struct {
	root string
}

func NewFSService(root string) *FSService {
	return &FSService{root: filepath.Clean(root)}
}

// Root 返回工作区根目录，供 UI 或测试展示。
func (s *FSService) Root() string {
	return s.root
}

// Execute 执行一次文件系统工具；返回的第二个值是本次写入或编辑的绝对路径，
// 供调用方登记到文件库，其它工具返回空字符串。
func (s *FSService) Execute(ctx context.Context, call tool.Call) (tool.Result, string, error) {
	if s == nil || s.root == "" {
		return tool.Result{}, "", errors.New("工作区未配置")
	}
	_ = ctx

	switch call.Tool.ID {
	case tool.ListDirToolID:
		var in tool.FSListDirInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, "", err
		}
		text, err := s.listDir(in)
		return tool.Result{Content: text}, "", err

	case tool.MakeDirToolID:
		var in tool.FSMakeDirInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, "", err
		}
		text, err := s.makeDir(in)
		return tool.Result{Content: text}, "", err

	case tool.ReadFileToolID:
		var in tool.FSReadFileInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, "", err
		}
		text, err := s.readFile(in)
		return tool.Result{Content: text}, "", err

	case tool.WriteFileToolID:
		var in tool.FSWriteFileInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, "", err
		}
		text, path, err := s.writeFile(in)
		return tool.Result{Content: text}, path, err

	case tool.EditFileToolID:
		var in tool.FSEditFileInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, "", err
		}
		text, path, err := s.editFile(in)
		return tool.Result{Content: text}, path, err

	case tool.DeleteFileToolID:
		var in tool.FSDeleteFileInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, "", err
		}
		text, err := s.deleteFile(in)
		return tool.Result{Content: text}, "", err

	case tool.GlobToolID:
		var in tool.FSGlobInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, "", err
		}
		text, err := s.glob(in)
		return tool.Result{Content: text}, "", err

	case tool.SearchFilesToolID:
		var in tool.FSSearchInput
		if err := decodeArguments(call.Arguments, &in); err != nil {
			return tool.Result{}, "", err
		}
		text, err := s.search(in)
		return tool.Result{Content: text}, "", err

	default:
		return tool.Result{}, "", fmt.Errorf("未实现的文件工具：%s", call.Tool.ID)
	}
}

func (s *FSService) listDir(in tool.FSListDirInput) (string, error) {
	dir, err := s.resolve(in.Path, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("读取目录失败：%w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s 不是目录，请使用 readFile", s.display(dir))
	}
	depth := in.Depth
	if depth <= 1 {
		depth = 1
	}
	if depth > 4 {
		depth = 4
	}

	lines := []string{"目录：" + s.display(dir)}
	lines, overflow := s.appendEntries(lines, dir, depth, 0)
	if overflow {
		lines = append(lines, "…结果过多，已省略其余条目")
	}
	return strings.Join(lines, "\n"), nil
}

// appendEntries 递归列出目录内容，目录在前、名称升序，超过上限时提前结束。
func (s *FSService) appendEntries(lines []string, dir string, depth, level int) ([]string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return append(lines, strings.Repeat("  ", level)+"（读取失败："+err.Error()+"）"), false
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if len(lines) >= fsMaxListLines {
			return lines, true
		}
		indent := strings.Repeat("  ", level)
		if entry.IsDir() {
			lines = append(lines, indent+"[目录] "+entry.Name())
			if level+1 < depth {
				lines, _ = s.appendEntries(lines, filepath.Join(dir, entry.Name()), depth, level+1)
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			lines = append(lines, indent+"[文件] "+entry.Name())
			continue
		}
		lines = append(lines, fmt.Sprintf("%s[文件] %s（%d 字节）", indent, entry.Name(), info.Size()))
	}
	return lines, len(lines) >= fsMaxListLines
}

func (s *FSService) makeDir(in tool.FSMakeDirInput) (string, error) {
	dir, err := s.resolve(in.Path, false)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建目录失败：%w", err)
	}
	return "已创建目录 " + s.display(dir), nil
}

func (s *FSService) readFile(in tool.FSReadFileInput) (string, error) {
	target, err := s.resolve(in.Path, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("读取文件失败：%w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s 是目录，请使用 listDir", s.display(target))
	}
	if info.Size() > fsMaxReadBytes {
		return "", fmt.Errorf("文件超过 %d KB 上限，请用 searchFiles 定位片段", fsMaxReadBytes/1024)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("读取文件失败：%w", err)
	}
	if !utf8.Valid(data) {
		return "", errors.New("文件不是 UTF-8 文本，暂不支持读取二进制内容")
	}
	return string(data), nil
}

func (s *FSService) writeFile(in tool.FSWriteFileInput) (string, string, error) {
	if len(in.Content) > fsMaxWriteBytes {
		return "", "", fmt.Errorf("内容超过 %d KB 上限", fsMaxWriteBytes/1024)
	}
	target, err := s.resolve(in.Path, false)
	if err != nil {
		return "", "", err
	}
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return "", "", fmt.Errorf("%s 是目录，无法写入", s.display(target))
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", "", fmt.Errorf("创建目录失败：%w", err)
	}
	if err := os.WriteFile(target, []byte(in.Content), 0o644); err != nil {
		return "", "", fmt.Errorf("写入文件失败：%w", err)
	}
	return fmt.Sprintf("已写入 %s（%d 字节）", s.display(target), len(in.Content)), target, nil
}

// editFile 用精确文本替换修改文件；匹配不唯一时要求更多上下文，
// 避免模型误改同名的其它片段。
func (s *FSService) editFile(in tool.FSEditFileInput) (string, string, error) {
	if strings.TrimSpace(in.OldText) == "" {
		return "", "", errors.New("oldText 不能为空，请先读取文件再指定要替换的内容")
	}
	target, err := s.resolve(in.Path, true)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", "", fmt.Errorf("读取文件失败：%w", err)
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("%s 是目录，无法编辑", s.display(target))
	}
	if info.Size() > fsMaxReadBytes {
		return "", "", fmt.Errorf("文件超过 %d KB 上限，请分段处理", fsMaxReadBytes/1024)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", "", fmt.Errorf("读取文件失败：%w", err)
	}
	if !utf8.Valid(data) {
		return "", "", errors.New("文件不是 UTF-8 文本，暂不支持编辑二进制内容")
	}

	content := string(data)
	count := strings.Count(content, in.OldText)
	if count == 0 {
		return "", "", errors.New("未找到要替换的内容，请确认 oldText 与文件内容完全一致（包括缩进与换行）")
	}
	if count > 1 && !in.ReplaceAll {
		return "", "", fmt.Errorf("oldText 在文件中出现 %d 次，请补充上下文使其唯一，或设置 replaceAll 为 true", count)
	}

	next := strings.ReplaceAll(content, in.OldText, in.NewText)
	if len(next) > fsMaxWriteBytes {
		return "", "", fmt.Errorf("编辑后内容超过 %d KB 上限", fsMaxWriteBytes/1024)
	}
	if err := os.WriteFile(target, []byte(next), info.Mode().Perm()); err != nil {
		return "", "", fmt.Errorf("写入文件失败：%w", err)
	}
	return fmt.Sprintf("已编辑 %s（替换 %d 处，%d 字节 → %d 字节）",
		s.display(target), count, len(content), len(next)), target, nil
}

func (s *FSService) deleteFile(in tool.FSDeleteFileInput) (string, error) {
	target, err := s.resolve(in.Path, true)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("读取文件失败：%w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s 是目录，deleteFile 只能删除文件", s.display(target))
	}
	if err := os.Remove(target); err != nil {
		return "", fmt.Errorf("删除文件失败：%w", err)
	}
	return fmt.Sprintf("已删除 %s（%d 字节）", s.display(target), info.Size()), nil
}

// glob 支持 *、? 与 **，按路径段递归匹配，不依赖第三方库。
func (s *FSService) glob(in tool.FSGlobInput) (string, error) {
	pattern := filepath.ToSlash(strings.TrimSpace(in.Pattern))
	if pattern == "" {
		return "", errors.New("pattern 不能为空")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > fsMaxGlobResults {
		limit = fsMaxGlobResults
	}

	segments := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	matches := make([]string, 0, 16)
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == s.root {
			return nil
		}
		if entry.IsDir() {
			if skipFSNames[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil
		}
		if matchFSPath(segments, strings.Split(filepath.ToSlash(rel), "/")) {
			matches = append(matches, filepath.ToSlash(rel))
		}
		if len(matches) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("查找文件失败：%w", err)
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return "没有匹配的文件。", nil
	}
	lines := make([]string, 0, len(matches)+1)
	lines = append(lines, fmt.Sprintf("匹配到 %d 个文件：", len(matches)))
	lines = append(lines, matches...)
	return strings.Join(lines, "\n"), nil
}

func (s *FSService) search(in tool.FSSearchInput) (string, error) {
	raw := strings.TrimSpace(in.Pattern)
	if raw == "" {
		return "", errors.New("pattern 不能为空")
	}
	re, err := regexp.Compile("(?i)" + raw)
	if err != nil {
		return "", fmt.Errorf("正则表达式无效：%w", err)
	}
	start, err := s.resolve(in.Path, false)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > fsMaxSearchHits {
		limit = fsMaxSearchHits
	}

	globPattern := strings.TrimSpace(in.Glob)
	hits := make([]string, 0, 16)
	truncated := false
	walkErr := filepath.WalkDir(start, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != start && skipFSNames[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if globPattern != "" {
			ok, matchErr := filepath.Match(globPattern, entry.Name())
			if matchErr != nil || !ok {
				return nil
			}
		}
		info, err := entry.Info()
		if err != nil || info.Size() > fsMaxSearchFile {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || !utf8.Valid(data) {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		for index, line := range strings.Split(string(data), "\n") {
			if !re.MatchString(line) {
				continue
			}
			hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, index+1, truncateRunes(strings.TrimSpace(line), 200)))
			if len(hits) >= limit {
				truncated = true
				return fs.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("搜索失败：%w", walkErr)
	}
	if len(hits) == 0 {
		return "没有匹配的内容。", nil
	}
	lines := make([]string, 0, len(hits)+1)
	lines = append(lines, fmt.Sprintf("找到 %d 处匹配：", len(hits)))
	lines = append(lines, hits...)
	if truncated {
		lines = append(lines, "…结果过多，已截断")
	}
	return strings.Join(lines, "\n"), nil
}

// skipFSNames 是遍历时忽略的目录，避免搜索与 glob 被依赖和版本库淹没。
var skipFSNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	".idea":        true,
	".vscode":      true,
}

// resolve 把模型给的路径解析为工作区内的绝对路径。
// 相对路径以工作区为基准；绝对路径必须落在工作区内。
func (s *FSService) resolve(raw string, mustExist bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "."
	}
	raw = filepath.FromSlash(raw)
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(s.root, raw)
	}
	abs := filepath.Clean(raw)
	if !withinRoot(s.root, abs) {
		return "", fmt.Errorf("路径 %s 超出工作区范围，请使用工作区相对路径", abs)
	}
	if err := s.checkSymlink(abs); err != nil {
		return "", err
	}
	if mustExist {
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("文件不存在：%s", s.display(abs))
		}
	}
	return abs, nil
}

// checkSymlink 解析最近的已存在父目录，防止工作区内的符号链接指向外面。
func (s *FSService) checkSymlink(abs string) error {
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return fmt.Errorf("解析工作区失败：%w", err)
	}
	probe := abs
	rest := []string{}
	for {
		if _, err := os.Lstat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		rest = append([]string{filepath.Base(probe)}, rest...)
		probe = parent
	}
	real, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return nil
	}
	full := filepath.Join(append([]string{real}, rest...)...)
	if !withinRoot(root, full) {
		return errors.New("路径通过符号链接指向工作区外，已拒绝")
	}
	return nil
}

// display 把绝对路径转成工作区相对路径，便于模型复用。
func (s *FSService) display(abs string) string {
	rel, err := filepath.Rel(s.root, abs)
	if err != nil || rel == "." {
		return "/"
	}
	if strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

func withinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// matchFSPath 按路径段匹配通配符，** 可跨任意层目录。
func matchFSPath(pattern, parts []string) bool {
	if len(pattern) == 0 {
		return len(parts) == 0
	}
	if pattern[0] == "**" {
		if matchFSPath(pattern[1:], parts) {
			return true
		}
		for i := 0; i < len(parts); i++ {
			if matchFSPath(pattern[1:], parts[i+1:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	ok, err := filepath.Match(pattern[0], parts[0])
	if err != nil || !ok {
		return false
	}
	return matchFSPath(pattern[1:], parts[1:])
}
