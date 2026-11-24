package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/platform/id"
	"bowknot-workbench/internal/store"
)

const (
	// maxAttachmentsPerMessage 限制单条消息的附件数量，避免请求体过大。
	maxAttachmentsPerMessage = 4
	// maxAttachmentImageSize 是单张图片的上限；base64 后约为 1.33 倍。
	maxAttachmentImageSize = 5 << 20
	// maxAttachmentFileSize 是非图片附件的上限。
	maxAttachmentFileSize = 20 << 20
	// maxInlineAttachmentBytes 是文本类附件内联进请求的上限。
	maxInlineAttachmentBytes = 16 << 10
	// maxHistoryImageMessages / maxHistoryImages 控制历史图片重发范围。
	maxHistoryImageMessages = 2
	maxHistoryImages        = 4
)

// attachmentSource 是校验通过、尚未复制的附件。
type attachmentSource struct {
	path string
	name string
	size int64
	kind domain.ArtifactKind
}

// validateAttachments 校验附件数量、路径、类型与大小，返回可复制的来源。
func validateAttachments(inputs []contract.AttachmentInput) ([]attachmentSource, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if len(inputs) > maxAttachmentsPerMessage {
		return nil, fmt.Errorf("单条消息最多添加 %d 个附件", maxAttachmentsPerMessage)
	}

	out := make([]attachmentSource, 0, len(inputs))
	for _, input := range inputs {
		path := strings.TrimSpace(input.Path)
		if path == "" {
			return nil, errors.New("附件路径不能为空")
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, errors.New("附件路径无效")
		}
		info, err := os.Stat(absolute)
		if err != nil || info.IsDir() {
			return nil, fmt.Errorf("附件不存在：%s", filepath.Base(path))
		}
		name := filepath.Base(absolute)
		kind := attachmentKind(name)
		if kind == domain.ArtifactImage {
			if info.Size() > maxAttachmentImageSize {
				return nil, fmt.Errorf("图片 %s 超过 %d MB", name, maxAttachmentImageSize>>20)
			}
		} else if info.Size() > maxAttachmentFileSize {
			return nil, fmt.Errorf("文件 %s 超过 %d MB", name, maxAttachmentFileSize>>20)
		}
		out = append(out, attachmentSource{path: absolute, name: name, size: info.Size(), kind: kind})
	}
	return out, nil
}

// attachmentKind 按扩展名判断附件类型；无法识别的扩展名一律按普通文件处理，
// 避免把二进制内容内联成文本。
func attachmentKind(name string) domain.ArtifactKind {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return domain.ArtifactImage
	case "":
		return domain.ArtifactFile
	default:
		return normalizeArtifactKind("", name, "", "")
	}
}

// StageAttachments 把用户选中的文件复制进产物目录，返回待入库的记录。
// 文件复制完成后调用方在事务内写记录；任一步失败用 DiscardStaged 清理。
func (s *ArtifactService) StageAttachments(conversationID, messageID string, inputs []contract.AttachmentInput) ([]domain.Artifact, error) {
	sources, err := validateAttachments(inputs)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, nil
	}

	now := time.Now().UnixMilli()
	staged := make([]domain.Artifact, 0, len(sources))
	for _, source := range sources {
		artifactID := "art-" + id.New()
		filename := artifactFilename(source.kind, source.name, "")
		limit := int64(maxAttachmentFileSize)
		if source.kind == domain.ArtifactImage {
			limit = maxAttachmentImageSize
			if !allowedImageExt(source.name) {
				s.DiscardStaged(staged)
				return nil, errors.New("图片格式仅支持 jpg、png、gif、webp")
			}
		}
		relativePath, size, mimeType, err := s.copySource(artifactID, filename, source.path, limit)
		if err != nil {
			s.DiscardStaged(staged)
			return nil, err
		}
		if source.kind == domain.ArtifactImage && !strings.HasPrefix(mimeType, "image/") {
			_ = os.RemoveAll(filepath.Join(s.dir, artifactID))
			s.DiscardStaged(staged)
			return nil, fmt.Errorf("%s 不是有效的图片文件", source.name)
		}
		staged = append(staged, domain.Artifact{
			ID:             artifactID,
			ConversationID: conversationID,
			MessageID:      messageID,
			Title:          truncateRunes(source.name, 120),
			Kind:           source.kind,
			MIME:           mimeType,
			Filename:       filename,
			RelativePath:   relativePath,
			Size:           size,
			Summary:        "用户上传",
			CreatedAt:      now,
		})
	}
	return staged, nil
}

// DiscardStaged 删除已复制但尚未入库的附件目录。
func (s *ArtifactService) DiscardStaged(staged []domain.Artifact) {
	for _, item := range staged {
		if strings.ContainsAny(item.ID, `/\`) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(s.dir, item.ID))
	}
}

// TouchRevision 在附件入库后递增产物版本，让文件库与会话文件重新加载。
func (s *ArtifactService) TouchRevision() {
	s.revision.Add(1)
}

// AttachmentPath 返回产物文件的绝对路径；仅托管产物有值。
func (s *ArtifactService) AttachmentPath(item domain.Artifact) string {
	if item.RelativePath == "" {
		return ""
	}
	return filepath.Join(s.dir, filepath.FromSlash(item.RelativePath))
}

// DeleteByMessage 删除一条消息关联的附件记录与托管文件。
func (s *ArtifactService) DeleteByMessage(ctx context.Context, messageID string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	items, err := s.store.Artifacts.ListByMessageIDs(ctx, []string{messageID})
	if err != nil {
		return fmt.Errorf("读取附件失败：%w", err)
	}
	if len(items) == 0 {
		return nil
	}

	removed := false
	for _, item := range items {
		// 外部登记的文件只删记录，不动原文件。
		if item.SourcePath == "" && item.RelativePath != "" && !strings.ContainsAny(item.ID, `/\`) {
			if err := os.RemoveAll(filepath.Join(s.dir, item.ID)); err != nil {
				return fmt.Errorf("删除附件失败：%w", err)
			}
		}
		if err := s.store.Artifacts.Delete(ctx, item.ID); err != nil {
			return fmt.Errorf("删除附件记录失败：%w", err)
		}
		removed = true
	}
	if removed {
		s.revision.Add(1)
	}
	return nil
}

// chatModelByID 在启动时缓存的模型列表里查找指定 key。
func chatModelByID(models []contract.ChatModel, id string) (contract.ChatModel, bool) {
	for _, model := range models {
		if model.ID == id {
			return model, true
		}
	}
	return contract.ChatModel{}, false
}

// modelMaxTokens 返回模型配置的最大输出 token；未配置时为 0。
func modelMaxTokens(models []contract.ChatModel, id string) int {
	model, ok := chatModelByID(models, id)
	if !ok {
		return 0
	}
	return model.MaxOutputTokens
}

// attachmentVisionError 判断本轮附件能否交给所选模型：
// 带图片时要求模型支持图片理解；模型未选择时按演示模式放行。
func attachmentVisionError(staged []domain.Artifact, model contract.ChatModel, modelKnown bool, hasModels bool) error {
	if !containsImage(staged) {
		return nil
	}
	if !modelKnown {
		if hasModels {
			return errors.New("请先选择支持图片理解的模型")
		}
		return nil
	}
	if !model.Vision {
		return errors.New("当前模型不支持图片理解，请切换支持图片的模型")
	}
	return nil
}

func containsImage(items []domain.Artifact) bool {
	for _, item := range items {
		if item.Kind == domain.ArtifactImage {
			return true
		}
	}
	return false
}

// buildChatParts 为历史消息构建多模态内容：图片按最近两条带图消息重发，
// 文本类附件只在本轮内联，其余附件只给文件名提示。
// currentMessageID 为空（例如重答）时所有图片都降级为文字提示。
func buildChatParts(ctx context.Context, st *store.Store, artifacts *ArtifactService, items []domain.Message, model contract.ChatModel, currentMessageID string) map[string][]llm.ContentPart {
	if artifacts == nil || len(items) == 0 {
		return nil
	}

	userIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.Role == domain.RoleUser {
			userIDs = append(userIDs, item.ID)
		}
	}
	if len(userIDs) == 0 {
		return nil
	}
	attachments, err := st.Artifacts.ListByMessageIDs(ctx, userIDs)
	if err != nil || len(attachments) == 0 {
		return nil
	}
	byMessage := make(map[string][]domain.Artifact)
	for _, item := range attachments {
		byMessage[item.MessageID] = append(byMessage[item.MessageID], item)
	}

	// 从最新消息往前数，优先保证最新图片进入请求，避免旧图占满预算。
	allowedImages := make(map[string]bool)
	budget := maxHistoryImages
	messages := 0
	for i := len(items) - 1; i >= 0 && budget > 0; i-- {
		item := items[i]
		if item.Role != domain.RoleUser || !model.Vision || messages >= maxHistoryImageMessages {
			continue
		}
		list := byMessage[item.ID]
		if !hasImage(list) {
			continue
		}
		messages++
		for j := len(list) - 1; j >= 0 && budget > 0; j-- {
			if list[j].Kind != domain.ArtifactImage {
				continue
			}
			allowedImages[list[j].ID] = true
			budget--
		}
	}

	out := make(map[string][]llm.ContentPart, len(byMessage))
	for _, item := range items {
		list := byMessage[item.ID]
		if len(list) == 0 {
			continue
		}
		parts := make([]llm.ContentPart, 0, len(list))
		notes := make([]string, 0, len(list))
		for _, attachment := range list {
			if attachment.Kind == domain.ArtifactImage {
				if !allowedImages[attachment.ID] {
					notes = append(notes, "[图片] "+attachment.Title)
					continue
				}
				url, err := imageDataURL(artifacts, attachment)
				if err != nil {
					notes = append(notes, "[图片] "+attachment.Title)
					continue
				}
				parts = append(parts, llm.ContentPart{
					Type:     llm.ContentPartImage,
					ImageURL: llm.ImageURL{URL: url},
				})
				continue
			}
			if item.ID == currentMessageID && isInlineTextKind(attachment.Kind) {
				content := readAttachmentText(artifacts.AttachmentPath(attachment))
				if content != "" {
					parts = append(parts, llm.ContentPart{
						Type: llm.ContentPartText,
						Text: fmt.Sprintf("附件 %s：\n%s", attachment.Title, content),
					})
					continue
				}
			}
			notes = append(notes, "[附件] "+attachment.Title)
		}
		if len(notes) > 0 {
			parts = append([]llm.ContentPart{{
				Type: llm.ContentPartText,
				Text: strings.Join(notes, "\n"),
			}}, parts...)
		}
		if len(parts) > 0 {
			out[item.ID] = parts
		}
	}
	return out
}

func hasImage(items []domain.Artifact) bool {
	for _, item := range items {
		if item.Kind == domain.ArtifactImage {
			return true
		}
	}
	return false
}

func isInlineTextKind(kind domain.ArtifactKind) bool {
	switch kind {
	case domain.ArtifactText, domain.ArtifactCode, domain.ArtifactMarkdown:
		return true
	default:
		return false
	}
}

// imageDataURL 把托管图片编码成 data URL；MIME 以落盘时探测的结果为准。
func imageDataURL(artifacts *ArtifactService, item domain.Artifact) (string, error) {
	path := artifacts.AttachmentPath(item)
	if path == "" {
		return "", errors.New("图片不存在")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	mimeType := item.MIME
	if !strings.HasPrefix(mimeType, "image/") {
		mimeType = "image/png"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// readAttachmentText 读取文本类附件内容并按 UTF-8 边界截断。
func readAttachmentText(path string) string {
	if path == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxInlineAttachmentBytes+1))
	if err != nil {
		return ""
	}
	truncated := len(data) > maxInlineAttachmentBytes
	if truncated {
		data = data[:maxInlineAttachmentBytes]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	text := strings.ToValidUTF8(string(data), "")
	if truncated {
		text += "\n…（内容过长，已截断）"
	}
	return strings.TrimSpace(text)
}
