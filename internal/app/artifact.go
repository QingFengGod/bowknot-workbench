package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/platform/id"
	"bowknot-workbench/internal/store"
)

const (
	maxTextArtifactSize  = 2 << 20
	maxImageArtifactSize = 20 << 20
	maxFileArtifactSize  = 50 << 20
	maxArtifactNameRunes = 100
)

// ArtifactService 负责文件校验、落盘、记录与查询。
type ArtifactService struct {
	store    *store.Store
	root     string
	dir      string
	logger   *slog.Logger
	revision atomic.Uint64
}

func NewArtifactService(st *store.Store, root, dir string, logger *slog.Logger) *ArtifactService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ArtifactService{store: st, root: root, dir: dir, logger: logger}
}

func (s *ArtifactService) ArtifactRevision() uint64 {
	return s.revision.Load()
}

// SaveArtifact 实现工具写入入口：校验类型与内容，落盘后写库。
func (s *ArtifactService) SaveArtifact(ctx context.Context, conversationID string, in tool.ArtifactInput) (tool.ArtifactRef, error) {
	title := truncateRunes(strings.TrimSpace(in.Title), 120)
	if title == "" {
		return tool.ArtifactRef{}, errors.New("文件标题不能为空")
	}

	sourcePath := ""
	if strings.TrimSpace(in.SourcePath) != "" {
		resolved, err := s.resolveSourcePath(in.SourcePath)
		if err != nil {
			return tool.ArtifactRef{}, err
		}
		sourcePath = resolved
	}
	kind := normalizeArtifactKind(in.Kind, in.Filename, sourcePath, in.Content)
	nameHint := firstNonEmpty(in.Filename, filepath.Base(sourcePath))
	// 二进制文件沿用源文件扩展名，避免被补成 .txt 后交给错误的系统应用。
	if (kind == domain.ArtifactImage || kind == domain.ArtifactFile) &&
		sourcePath != "" && filepath.Ext(nameHint) == "" {
		nameHint += filepath.Ext(sourcePath)
	}
	filename := artifactFilename(kind, nameHint, in.Language)
	if kind == domain.ArtifactLink {
		filename = ""
	}
	artifactID := "art-" + id.New()
	now := time.Now().UnixMilli()

	var (
		relativePath string
		uri          string
		mime         string
		size         int64
		err          error
	)
	switch kind {
	case domain.ArtifactLink:
		uri, err = normalizeArtifactLink(in.Content)
		if err != nil {
			return tool.ArtifactRef{}, err
		}
		mime = "text/uri-list"

	case domain.ArtifactImage, domain.ArtifactFile:
		if sourcePath == "" {
			return tool.ArtifactRef{}, errors.New("图片或文件需要提供 sourcePath")
		}
		limit := int64(maxFileArtifactSize)
		if kind == domain.ArtifactImage {
			if !allowedImageExt(filename) {
				return tool.ArtifactRef{}, errors.New("图片格式仅支持 jpg、png、gif、webp")
			}
			limit = maxImageArtifactSize
		}
		relativePath, size, mime, err = s.copySource(artifactID, filename, sourcePath, limit)
		if err != nil {
			return tool.ArtifactRef{}, err
		}
		if kind == domain.ArtifactImage && !strings.HasPrefix(mime, "image/") {
			_ = os.RemoveAll(filepath.Join(s.dir, artifactID))
			return tool.ArtifactRef{}, errors.New("sourcePath 不是有效的图片文件")
		}

	default:
		content := in.Content
		if strings.TrimSpace(content) == "" {
			return tool.ArtifactRef{}, errors.New("文本文件需要提供 content")
		}
		if int64(len(content)) > maxTextArtifactSize {
			return tool.ArtifactRef{}, fmt.Errorf("文本文件不能超过 %d KB", maxTextArtifactSize/1024)
		}
		mime = artifactMIME(kind)
		relativePath, size, err = s.writeContent(artifactID, filename, []byte(content))
		if err != nil {
			return tool.ArtifactRef{}, err
		}
	}

	conversation, taskID := s.contextOf(ctx, conversationID)
	record := domain.Artifact{
		ID:             artifactID,
		ConversationID: conversation,
		TaskID:         taskID,
		MessageID:      tool.MessageID(ctx),
		Title:          title,
		Kind:           kind,
		MIME:           mime,
		Filename:       filename,
		RelativePath:   relativePath,
		URI:            uri,
		Size:           size,
		Summary:        truncateRunes(strings.TrimSpace(in.Summary), 200),
		CreatedAt:      now,
	}
	if err := s.store.Artifacts.Create(ctx, record); err != nil {
		// 记录失败时清理已落盘的文件，避免留下孤儿目录。
		_ = os.RemoveAll(filepath.Join(s.dir, artifactID))
		return tool.ArtifactRef{}, fmt.Errorf("保存文件记录失败：%w", err)
	}

	s.revision.Add(1)
	return tool.ArtifactRef{
		ID:       record.ID,
		Title:    record.Title,
		Kind:     string(record.Kind),
		Filename: record.Filename,
		Size:     record.Size,
	}, nil
}

// RegisterFile 把工作区里的文件登记到文件库：只记录路径，不复制文件，
// 让内置文件工具写入的内容也能出现在对话与文件库，并始终指向原文件。
func (s *ArtifactService) RegisterFile(ctx context.Context, conversationID, path string) (tool.ArtifactRef, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return tool.ArtifactRef{}, fmt.Errorf("解析文件路径失败：%w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return tool.ArtifactRef{}, fmt.Errorf("读取文件失败：%w", err)
	}
	if info.IsDir() {
		return tool.ArtifactRef{}, fmt.Errorf("%s 是目录，不能加入文件库", abs)
	}
	if info.Size() > maxFileArtifactSize {
		return tool.ArtifactRef{}, fmt.Errorf("文件超过 %d MB，暂不能加入文件库", maxFileArtifactSize>>20)
	}

	messageID := strings.TrimSpace(tool.MessageID(ctx))
	// 同一条消息里重复写入同一个文件时复用已有登记，避免文件条刷屏。
	if messageID != "" {
		if items, listErr := s.store.Artifacts.ListByMessageIDs(ctx, []string{messageID}); listErr == nil {
			for _, item := range items {
				if item.SourcePath == abs {
					return tool.ArtifactRef{
						ID:       item.ID,
						Title:    item.Title,
						Kind:     string(item.Kind),
						Filename: item.Filename,
						Size:     item.Size,
					}, nil
				}
			}
		}
	}

	conversation, taskID := s.contextOf(ctx, conversationID)
	name := filepath.Base(abs)
	kind := normalizeArtifactKind("", name, abs, "")
	mimeType := mime.TypeByExtension(filepath.Ext(abs))
	if mimeType == "" {
		mimeType = artifactMIME(kind)
	}
	record := domain.Artifact{
		ID:             "art-" + id.New(),
		ConversationID: conversation,
		TaskID:         taskID,
		MessageID:      messageID,
		Title:          name,
		Kind:           kind,
		MIME:           mimeType,
		Filename:       name,
		SourcePath:     abs,
		Size:           info.Size(),
		Summary:        "本地文件",
		CreatedAt:      time.Now().UnixMilli(),
	}
	if err := s.store.Artifacts.Create(ctx, record); err != nil {
		return tool.ArtifactRef{}, fmt.Errorf("登记文件失败：%w", err)
	}

	s.revision.Add(1)
	return tool.ArtifactRef{
		ID:       record.ID,
		Title:    record.Title,
		Kind:     string(record.Kind),
		Filename: record.Filename,
		Size:     record.Size,
	}, nil
}

// Artifacts 返回文件列表，供文件库页面与筛选使用。
func (s *ArtifactService) Artifacts(filter contract.ArtifactFilter) []contract.ArtifactDetail {
	kinds := make([]domain.ArtifactKind, 0, len(filter.Kinds))
	for _, kind := range filter.Kinds {
		kinds = append(kinds, domain.ArtifactKind(kind))
	}
	items, err := s.store.Artifacts.List(context.Background(), store.ArtifactFilter{
		Kind:    domain.ArtifactKind(filter.Kind),
		Kinds:   kinds,
		OwnerID: filter.OwnerID,
		TaskID:  filter.TaskID,
		Search:  filter.Search,
		Limit:   filter.Limit,
	})
	if err != nil {
		s.logger.Error("读取文件列表失败", "error", err)
		return nil
	}
	out := make([]contract.ArtifactDetail, 0, len(items))
	for _, item := range items {
		out = append(out, s.detail(context.Background(), item))
	}
	return out
}

func (s *ArtifactService) Artifact(id string) contract.ArtifactDetail {
	item, err := s.store.Artifacts.Get(context.Background(), id)
	if err != nil {
		return contract.ArtifactDetail{}
	}
	return s.detail(context.Background(), item)
}

// DeleteArtifact 删除文件记录与托管目录；失败时保留记录并返回中文错误。
func (s *ArtifactService) DeleteArtifact(id string) error {
	ctx := context.Background()
	item, err := s.store.Artifacts.Get(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("文件不存在：%s", id)
		}
		return fmt.Errorf("读取文件失败：%w", err)
	}

	if strings.ContainsAny(item.ID, `/\`) {
		return errors.New("文件路径非法，已拒绝删除")
	}
	// 外部文件只移除登记记录，绝不删除工作区里的原文件。
	if item.SourcePath == "" {
		dir := filepath.Join(s.dir, item.ID)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("删除文件失败：%w", err)
		}
	}
	if err := s.store.Artifacts.Delete(ctx, id); err != nil {
		return fmt.Errorf("删除文件记录失败：%w", err)
	}
	s.revision.Add(1)
	return nil
}

func (s *ArtifactService) detail(ctx context.Context, item domain.Artifact) contract.ArtifactDetail {
	detail := contract.ArtifactDetail{
		ArtifactBrief:  s.brief(item),
		ConversationID: item.ConversationID,
		TaskID:         item.TaskID,
		MessageID:      item.MessageID,
		Filename:       item.Filename,
		URI:            item.URI,
		Summary:        item.Summary,
		External:       item.SourcePath != "",
	}

	if item.Kind != domain.ArtifactLink {
		switch {
		case item.RelativePath != "":
			detail.Path = filepath.Join(s.dir, filepath.FromSlash(item.RelativePath))
		case item.SourcePath != "":
			detail.Path = item.SourcePath
		}
	}

	if conversation, err := s.store.Conversations.Get(ctx, item.ConversationID); err == nil {
		switch conversation.Kind {
		case domain.KindAssistant:
			detail.SourceKind, detail.SourceName = "main", "主助手"
		case domain.KindAgent:
			detail.SourceKind, detail.SourceName = "agent", conversation.Title
			if agent, err := s.store.Agents.Get(ctx, conversation.OwnerID); err == nil && agent.Name != "" {
				detail.SourceName = agent.Name
			}
		case domain.KindGroup:
			detail.SourceKind, detail.SourceName = "group", conversation.Title
		}
	}
	return detail
}

// brief 生成文件摘要，并检查文件是否仍然存在。
func (s *ArtifactService) brief(item domain.Artifact) contract.ArtifactBrief {
	brief := contract.ArtifactBrief{
		ID:        item.ID,
		Title:     item.Title,
		Kind:      contract.ArtifactKind(item.Kind),
		MIME:      item.MIME,
		Filename:  item.Filename,
		Size:      item.Size,
		CreatedAt: item.CreatedAt,
	}
	switch {
	case item.Kind == domain.ArtifactLink:
		brief.Available = true
	case item.SourcePath != "":
		if _, err := os.Stat(item.SourcePath); err == nil {
			brief.Available = true
		}
	case item.RelativePath != "":
		if _, err := os.Stat(filepath.Join(s.dir, filepath.FromSlash(item.RelativePath))); err == nil {
			brief.Available = true
		}
	}
	return brief
}

// contextOf 从当前消息推导会话与任务；消息不存在时退回调用方传入的会话 ID。
func (s *ArtifactService) contextOf(ctx context.Context, conversationID string) (string, string) {
	conversation := strings.TrimSpace(conversationID)
	messageID := strings.TrimSpace(tool.MessageID(ctx))
	if messageID == "" {
		return conversation, ""
	}
	message, err := s.store.Messages.Get(ctx, messageID)
	if err != nil {
		return conversation, ""
	}
	if conversation == "" {
		conversation = message.ConversationID
	}
	return conversation, message.TaskID
}

func (s *ArtifactService) writeContent(artifactID, filename string, content []byte) (string, int64, error) {
	dir := filepath.Join(s.dir, artifactID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("创建文件目录失败：%w", err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", 0, fmt.Errorf("创建文件失败：%w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		_ = os.RemoveAll(dir)
		return "", 0, fmt.Errorf("写入文件失败：%w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		_ = os.RemoveAll(dir)
		return "", 0, fmt.Errorf("保存文件失败：%w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		_ = os.RemoveAll(dir)
		return "", 0, fmt.Errorf("保存文件失败：%w", err)
	}

	final := filepath.Join(dir, filename)
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		_ = os.RemoveAll(dir)
		return "", 0, fmt.Errorf("保存文件失败：%w", err)
	}
	return filepath.ToSlash(filepath.Join(artifactID, filename)), int64(len(content)), nil
}

func (s *ArtifactService) copySource(artifactID, filename, sourcePath string, limit int64) (string, int64, string, error) {
	info, err := os.Stat(sourcePath)
	if err != nil || info.IsDir() {
		return "", 0, "", errors.New("sourcePath 不是可读取的文件")
	}
	if info.Size() > limit {
		return "", 0, "", fmt.Errorf("文件不能超过 %d MB", limit/1024/1024)
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return "", 0, "", fmt.Errorf("读取源文件失败：%w", err)
	}
	defer source.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(source, head)
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return "", 0, "", fmt.Errorf("读取源文件失败：%w", err)
	}
	mime := http.DetectContentType(head[:n])

	dir := filepath.Join(s.dir, artifactID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, "", fmt.Errorf("创建文件目录失败：%w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", 0, "", fmt.Errorf("创建文件失败：%w", err)
	}
	tmpName := tmp.Name()
	written, copyErr := io.Copy(tmp, source)
	if copyErr == nil {
		copyErr = tmp.Sync()
	}
	closeErr := tmp.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(tmpName)
		_ = os.RemoveAll(dir)
		return "", 0, "", fmt.Errorf("保存文件失败：%w", copyErr)
	}

	final := filepath.Join(dir, filename)
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		_ = os.RemoveAll(dir)
		return "", 0, "", fmt.Errorf("保存文件失败：%w", err)
	}
	return filepath.ToSlash(filepath.Join(artifactID, filename)), written, mime, nil
}

// resolveSourcePath 只允许把数据目录内的文件复制进文件库。
func (s *ArtifactService) resolveSourcePath(raw string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("sourcePath 无效")
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", errors.New("数据目录无效")
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("sourcePath 只能位于应用数据目录内")
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return "", errors.New("sourcePath 不是可读取的文件")
	}
	return abs, nil
}

func normalizeArtifactKind(raw, filename, sourcePath, content string) domain.ArtifactKind {
	kind := domain.ArtifactKind(strings.ToLower(strings.TrimSpace(raw)))
	switch kind {
	case domain.ArtifactMarkdown, domain.ArtifactText, domain.ArtifactCode, domain.ArtifactHTML,
		domain.ArtifactImage, domain.ArtifactLink, domain.ArtifactFile:
		return kind
	}

	name := filename
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(sourcePath)
	}
	ext := strings.ToLower(filepath.Ext(name))
	// 没有文件名与来源文件时，http/https 内容按链接处理。
	if ext == "" && strings.TrimSpace(sourcePath) == "" {
		lower := strings.ToLower(strings.TrimSpace(content))
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			return domain.ArtifactLink
		}
	}
	switch ext {
	case ".md", ".markdown":
		return domain.ArtifactMarkdown
	case ".html", ".htm":
		return domain.ArtifactHTML
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return domain.ArtifactImage
	case ".txt":
		return domain.ArtifactText
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".c", ".cc", ".cpp", ".h",
		".rs", ".rb", ".php", ".sh", ".sql", ".yaml", ".yml", ".json", ".toml", ".xml",
		".css", ".vue", ".kt", ".swift":
		return domain.ArtifactCode
	case "":
		return domain.ArtifactText
	default:
		return domain.ArtifactFile
	}
}

func artifactFilename(kind domain.ArtifactKind, rawName, language string) string {
	name := sanitizeArtifactName(rawName)
	ext := strings.ToLower(filepath.Ext(name))

	if name == "" {
		name = "artifact" + defaultArtifactExt(kind, language)
		return name
	}
	if ext == "" {
		name += defaultArtifactExt(kind, language)
	}
	return name
}

func sanitizeArtifactName(raw string) string {
	name := filepath.Base(strings.TrimSpace(raw))
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-", "?", "-",
		"\"", "-", "<", "-", ">", "-", "|", "-",
	)
	name = replacer.Replace(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 {
			return '-'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	runes := []rune(name)
	if len(runes) > maxArtifactNameRunes {
		name = string(runes[:maxArtifactNameRunes])
	}
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

func defaultArtifactExt(kind domain.ArtifactKind, language string) string {
	switch kind {
	case domain.ArtifactMarkdown:
		return ".md"
	case domain.ArtifactHTML:
		return ".html"
	case domain.ArtifactCode:
		return codeExt(language)
	case domain.ArtifactImage:
		return ".png"
	case domain.ArtifactLink, domain.ArtifactFile:
		return ""
	default:
		return ".txt"
	}
}

func codeExt(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "go", "golang":
		return ".go"
	case "python", "py":
		return ".py"
	case "javascript", "js":
		return ".js"
	case "typescript", "ts":
		return ".ts"
	case "java":
		return ".java"
	case "rust":
		return ".rs"
	case "c":
		return ".c"
	case "cpp", "c++":
		return ".cpp"
	case "shell", "bash", "sh":
		return ".sh"
	case "sql":
		return ".sql"
	case "json":
		return ".json"
	case "yaml", "yml":
		return ".yml"
	case "html":
		return ".html"
	case "css":
		return ".css"
	default:
		return ".txt"
	}
}

func allowedImageExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func artifactMIME(kind domain.ArtifactKind) string {
	switch kind {
	case domain.ArtifactMarkdown:
		return "text/markdown; charset=utf-8"
	case domain.ArtifactHTML:
		return "text/html; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}

func normalizeArtifactLink(content string) (string, error) {
	link := strings.TrimSpace(content)
	parsed, err := url.Parse(link)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("链接必须是 http 或 https 地址")
	}
	return link, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
