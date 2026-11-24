package app

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/llm"
)

// onePixelPNG 是 1x1 的合法 PNG，用于走真实的图片类型探测。
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func writeTestPNG(t *testing.T) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(onePixelPNG)
	if err != nil {
		t.Fatalf("解码测试图片失败：%v", err)
	}
	path := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入测试图片失败：%v", err)
	}
	return path
}

// newAttachmentFixture 装配带产物服务的助手，并返回最近一次模型请求。
func newAttachmentFixture(t *testing.T) (*AssistantService, *ArtifactService, func() llm.ChatRequest) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st := openTestStore(t, path, false)
	root := t.TempDir()
	artifacts := NewArtifactService(st, root, filepath.Join(root, "artifacts"), testLogger())
	if err := os.MkdirAll(artifacts.dir, 0o755); err != nil {
		t.Fatalf("创建产物目录失败：%v", err)
	}

	var captured llm.ChatRequest
	fake := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 1024,
		Reply: func(_ int, req llm.ChatRequest) llm.FakeReply {
			captured = req
			return llm.FakeReply{Content: "收到"}
		},
	})
	svc := NewAssistantService(st, testLogger(), fake, nil, nil)
	svc.SetArtifacts(artifacts)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	return svc, artifacts, func() llm.ChatRequest { return captured }
}

func TestValidateAttachmentsRejectsOverLimit(t *testing.T) {
	inputs := make([]contract.AttachmentInput, 0, maxAttachmentsPerMessage+1)
	for i := 0; i <= maxAttachmentsPerMessage; i++ {
		inputs = append(inputs, contract.AttachmentInput{Path: "a.txt"})
	}
	if _, err := validateAttachments(inputs); err == nil || !strings.Contains(err.Error(), "最多") {
		t.Fatalf("超过数量上限应报错，got=%v", err)
	}

	bigImage := filepath.Join(t.TempDir(), "big.png")
	if err := os.WriteFile(bigImage, make([]byte, maxAttachmentImageSize+1), 0o644); err != nil {
		t.Fatalf("写入大图失败：%v", err)
	}
	if _, err := validateAttachments([]contract.AttachmentInput{{Path: bigImage}}); err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("超大图片应报错，got=%v", err)
	}

	if _, err := validateAttachments([]contract.AttachmentInput{{Path: filepath.Join(t.TempDir(), "missing.png")}}); err == nil {
		t.Fatal("不存在的附件应报错")
	}
}

func TestSendAttachmentStoresArtifactAndBuildsParts(t *testing.T) {
	svc, artifacts, capturedRequest := newAttachmentFixture(t)
	svc.SetModels([]contract.ChatModel{{ID: "vision-model", Vision: true, MaxOutputTokens: 640}})
	st := artifacts.store

	session := svc.CreateSession()
	image := writeTestPNG(t)
	_, events, err := svc.Send(session.ID, "", "vision-model", []contract.AttachmentInput{{Path: image}})
	if err != nil {
		t.Fatalf("发送带附件消息失败：%v", err)
	}
	drainEvents(t, events)

	captured := capturedRequest()
	if captured.MaxTokens != 640 {
		t.Fatalf("max_tokens = %d, want 640", captured.MaxTokens)
	}
	if len(captured.Messages) == 0 {
		t.Fatal("请求消息为空")
	}
	last := captured.Messages[len(captured.Messages)-1]
	if len(last.Parts) == 0 {
		t.Fatal("带图片的消息应包含多模态分段")
	}
	var hasImage bool
	for _, part := range last.Parts {
		if part.Type == llm.ContentPartImage && strings.HasPrefix(part.ImageURL.URL, "data:image/png;base64,") {
			hasImage = true
		}
	}
	if !hasImage {
		t.Fatalf("缺少图片分段：%+v", last.Parts)
	}

	messages := svc.Messages(session.ID)
	if len(messages) != 2 {
		t.Fatalf("消息数量 = %d, want 2", len(messages))
	}
	if len(messages[0].Artifacts) != 1 || messages[0].Artifacts[0].Kind != contract.ArtifactImage {
		t.Fatalf("用户消息未关联图片附件：%+v", messages[0].Artifacts)
	}

	items, err := st.Artifacts.ListByMessageIDs(context.Background(), []string{messages[0].ID})
	if err != nil || len(items) != 1 {
		t.Fatalf("附件记录异常：%v, %d", err, len(items))
	}
	if _, err := os.Stat(artifacts.AttachmentPath(items[0])); err != nil {
		t.Fatalf("附件文件未落盘：%v", err)
	}
}

func TestSendRejectsImageWithoutVisionModel(t *testing.T) {
	svc, artifacts, _ := newAttachmentFixture(t)
	svc.SetModels([]contract.ChatModel{{ID: "text-model", Vision: false}})

	session := svc.CreateSession()
	_, _, err := svc.Send(session.ID, "看看这张图", "text-model", []contract.AttachmentInput{{Path: writeTestPNG(t)}})
	if err == nil || !strings.Contains(err.Error(), "不支持图片理解") {
		t.Fatalf("非视觉模型应拒绝图片，got=%v", err)
	}
	if messages := svc.Messages(session.ID); len(messages) != 0 {
		t.Fatalf("被拒绝的消息不应落库，got=%d", len(messages))
	}
	if entries, err := os.ReadDir(artifacts.dir); err == nil && len(entries) != 0 {
		t.Fatalf("被拒绝的附件应清理，got=%d 个目录", len(entries))
	}
}

func TestDeleteMessageRemovesAttachments(t *testing.T) {
	svc, artifacts, _ := newAttachmentFixture(t)
	svc.SetModels([]contract.ChatModel{{ID: "vision-model", Vision: true}})

	session := svc.CreateSession()
	_, events, err := svc.Send(session.ID, "图", "vision-model", []contract.AttachmentInput{{Path: writeTestPNG(t)}})
	if err != nil {
		t.Fatalf("发送失败：%v", err)
	}
	drainEvents(t, events)

	messages := svc.Messages(session.ID)
	if len(messages) == 0 || len(messages[0].Artifacts) != 1 {
		t.Fatalf("附件未落库：%+v", messages)
	}
	artifactPath := ""
	if items, err := artifacts.store.Artifacts.ListByMessageIDs(context.Background(), []string{messages[0].ID}); err == nil && len(items) == 1 {
		artifactPath = artifacts.AttachmentPath(items[0])
	}

	if err := svc.DeleteMessage(session.ID, messages[0].ID); err != nil {
		t.Fatalf("删除消息失败：%v", err)
	}
	items, err := artifacts.store.Artifacts.ListByMessageIDs(context.Background(), []string{messages[0].ID})
	if err != nil || len(items) != 0 {
		t.Fatalf("附件记录未删除：%v, %d", err, len(items))
	}
	if artifactPath != "" {
		if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
			t.Fatalf("附件文件未删除：%v", err)
		}
	}
}

func TestSendTextAttachmentInlineOnlyForCurrentTurn(t *testing.T) {
	svc, _, capturedRequest := newAttachmentFixture(t)
	svc.SetModels([]contract.ChatModel{{ID: "text-model"}})

	textFile := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(textFile, []byte("附件正文内容"), 0o644); err != nil {
		t.Fatalf("写入文本附件失败：%v", err)
	}

	session := svc.CreateSession()
	_, events, err := svc.Send(session.ID, "", "text-model", []contract.AttachmentInput{{Path: textFile}})
	if err != nil {
		t.Fatalf("发送文本附件失败：%v", err)
	}
	drainEvents(t, events)

	messages := svc.Messages(session.ID)
	if len(messages) != 2 || len(messages[0].Artifacts) != 1 {
		t.Fatalf("文本附件未落库：%+v", messages)
	}
	if messages[0].Content != "" {
		t.Fatalf("纯附件消息正文应为空，got=%q", messages[0].Content)
	}

	captured := capturedRequest()
	last := captured.Messages[len(captured.Messages)-1]
	var inlined bool
	for _, part := range last.Parts {
		if part.Type == llm.ContentPartText && strings.Contains(part.Text, "附件正文内容") {
			inlined = true
		}
	}
	if !inlined {
		t.Fatalf("本轮文本附件应内联进请求：%+v", last.Parts)
	}
}

func TestAgentRejectsAttachmentsInGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st := openTestStore(t, path, false)
	root := t.TempDir()
	artifacts := NewArtifactService(st, root, filepath.Join(root, "artifacts"), testLogger())
	if err := os.MkdirAll(artifacts.dir, 0o755); err != nil {
		t.Fatalf("创建产物目录失败：%v", err)
	}
	agents := newFastAgent(st)
	agents.SetArtifacts(artifacts)
	t.Cleanup(func() { _ = agents.Shutdown(context.Background()) })

	_, _, err := agents.Send("group:test-group", "看图", "", []contract.AttachmentInput{{Path: writeTestPNG(t)}})
	if err == nil || !strings.Contains(err.Error(), "群聊") {
		t.Fatalf("群聊应拒绝附件，got=%v", err)
	}
}
