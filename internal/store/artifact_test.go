package store

import (
	"context"
	"errors"
	"testing"

	"bowknot-workbench/internal/domain"
)

func newArtifact(id, conversationID, taskID, messageID string) domain.Artifact {
	return domain.Artifact{
		ID:             id,
		ConversationID: conversationID,
		TaskID:         taskID,
		MessageID:      messageID,
		Title:          "周报初稿",
		Kind:           domain.ArtifactMarkdown,
		MIME:           "text/markdown; charset=utf-8",
		Filename:       "report.md",
		RelativePath:   id + "/report.md",
		Size:           128,
		Summary:        "本周进展",
		CreatedAt:      1000,
	}
}

func TestArtifactStoreCRUD(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for _, item := range []domain.Conversation{
		{ID: "conv-a", Kind: domain.KindAgent, OwnerID: "agent-1", Title: "A", CreatedAt: 1, UpdatedAt: 1},
		{ID: "conv-b", Kind: domain.KindAgent, OwnerID: "agent-2", Title: "B", CreatedAt: 1, UpdatedAt: 1},
	} {
		if err := s.Conversations.Create(ctx, item); err != nil {
			t.Fatalf("创建会话失败：%v", err)
		}
	}

	if err := s.Artifacts.Create(ctx, newArtifact("art-1", "conv-a", "task-1", "msg-1")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	link := newArtifact("art-2", "conv-a", "", "msg-2")
	link.Kind = domain.ArtifactLink
	link.Filename = ""
	link.RelativePath = ""
	link.URI = "https://example.com"
	link.Title = "参考链接"
	if err := s.Artifacts.Create(ctx, link); err != nil {
		t.Fatalf("Create link error = %v", err)
	}
	third := newArtifact("art-3", "conv-b", "task-2", "msg-3")
	third.Title = "另一份报告"
	if err := s.Artifacts.Create(ctx, third); err != nil {
		t.Fatalf("Create third error = %v", err)
	}

	got, err := s.Artifacts.Get(ctx, "art-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != "周报初稿" || got.Kind != domain.ArtifactMarkdown || got.TaskID != "task-1" {
		t.Fatalf("Get() = %+v", got)
	}

	byKind, err := s.Artifacts.List(ctx, ArtifactFilter{Kind: domain.ArtifactLink})
	if err != nil || len(byKind) != 1 || byKind[0].ID != "art-2" {
		t.Fatalf("按类型过滤 = %+v, %v", byKind, err)
	}
	byTask, err := s.Artifacts.List(ctx, ArtifactFilter{TaskID: "task-2"})
	if err != nil || len(byTask) != 1 || byTask[0].ID != "art-3" {
		t.Fatalf("按任务过滤 = %+v, %v", byTask, err)
	}
	byKinds, err := s.Artifacts.List(ctx, ArtifactFilter{
		Kinds: []domain.ArtifactKind{domain.ArtifactMarkdown, domain.ArtifactLink},
	})
	if err != nil || len(byKinds) != 3 {
		t.Fatalf("按类型组过滤 = %+v, %v", byKinds, err)
	}
	bySearch, err := s.Artifacts.List(ctx, ArtifactFilter{Search: "报告"})
	if err != nil || len(bySearch) != 1 || bySearch[0].ID != "art-3" {
		t.Fatalf("按标题搜索 = %+v, %v", bySearch, err)
	}
	byOwner, err := s.Artifacts.List(ctx, ArtifactFilter{OwnerID: "agent-1"})
	if err != nil || len(byOwner) != 2 {
		t.Fatalf("按会话归属过滤 = %+v, %v", byOwner, err)
	}

	byMessage, err := s.Artifacts.ListByMessageIDs(ctx, []string{"msg-1", "msg-3"})
	if err != nil || len(byMessage) != 2 {
		t.Fatalf("ListByMessageIDs = %+v, %v", byMessage, err)
	}
	byTaskIDs, err := s.Artifacts.ListByTaskIDs(ctx, []string{"task-1"})
	if err != nil || len(byTaskIDs) != 1 || byTaskIDs[0].ID != "art-1" {
		t.Fatalf("ListByTaskIDs = %+v, %v", byTaskIDs, err)
	}

	if err := s.Artifacts.Delete(ctx, "art-2"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := s.Artifacts.Get(ctx, "art-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后 Get() = %v, want ErrNotFound", err)
	}
}
