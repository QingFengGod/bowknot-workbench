package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/store"
)

func newArtifactStack(t *testing.T) (*ArtifactService, *store.Store, string) {
	t.Helper()
	root := t.TempDir()
	st := openTestStore(t, filepath.Join(root, "test.db"), false)
	dir := filepath.Join(root, "artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建产物目录失败：%v", err)
	}
	svc := NewArtifactService(st, root, dir, testLogger())
	return svc, st, root
}

func TestSaveArtifactAndAttach(t *testing.T) {
	svc, st, _ := newArtifactStack(t)
	ctx := context.Background()

	if err := st.Conversations.Create(ctx, domain.Conversation{
		ID: "conv-1", Kind: domain.KindAgent, OwnerID: "agent-1", Title: "任务：写周报",
		CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	if _, err := st.Messages.Append(ctx, domain.Message{
		ID: "msg-1", ConversationID: "conv-1", Role: domain.RoleAssistant,
		Status: domain.MessageDone, CreatedAt: 1000, TaskID: "task-1",
	}); err != nil {
		t.Fatalf("写入消息失败：%v", err)
	}

	execCtx := tool.WithMessageID(ctx, "msg-1")
	ref, err := svc.SaveArtifact(execCtx, "conv-1", tool.ArtifactInput{
		Title: "周报初稿", Kind: "markdown", Filename: "周报.md", Content: "# 本周进展",
	})
	if err != nil {
		t.Fatalf("SaveArtifact() error = %v", err)
	}
	if ref.ID == "" || ref.Filename != "周报.md" {
		t.Fatalf("SaveArtifact() = %+v", ref)
	}

	detail := svc.Artifact(ref.ID)
	if !detail.Available || detail.Path == "" || detail.TaskID != "task-1" || detail.MessageID != "msg-1" {
		t.Fatalf("产物详情不匹配：%+v", detail)
	}
	if detail.SourceKind != "agent" {
		t.Fatalf("来源类型 = %q, want agent", detail.SourceKind)
	}
	body, err := os.ReadFile(detail.Path)
	if err != nil || string(body) != "# 本周进展" {
		t.Fatalf("产物内容 = %q, err = %v", string(body), err)
	}
	if svc.ArtifactRevision() == 0 {
		t.Fatal("保存产物后 revision 没有递增")
	}

	items := []contract.ChatMessage{{ID: "msg-1", TaskID: "task-1"}}
	attachArtifacts(st, svc, items)
	if len(items[0].Artifacts) != 1 || items[0].Artifacts[0].ID != ref.ID {
		t.Fatalf("消息没有附加产物：%+v", items[0].Artifacts)
	}

	// 任务汇报在来源会话里的消息 ID 不同，也要按任务 ID 关联到产物。
	report := []contract.ChatMessage{{
		ID: "msg-report", TaskID: "task-1", MessageType: "task_report", Visibility: "user",
	}}
	attachArtifacts(st, svc, report)
	if len(report[0].Artifacts) != 1 {
		t.Fatalf("任务汇报没有附加产物：%+v", report[0].Artifacts)
	}

	if err := svc.DeleteArtifact(ref.ID); err != nil {
		t.Fatalf("DeleteArtifact() error = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(detail.Path)); !os.IsNotExist(err) {
		t.Fatalf("删除后目录仍存在：%v", err)
	}
	if got := svc.Artifact(ref.ID); got.ID != "" {
		t.Fatalf("删除后仍能读到产物：%+v", got)
	}
}

func TestArtifactToolExecutor(t *testing.T) {
	svc, st, _ := newArtifactStack(t)
	exec := NewToolExecutor(st, nil)
	exec.SetArtifacts(svc)

	result, err := exec.Execute(context.Background(), tool.Call{
		Tool:           tool.ArtifactToolSpec(),
		AgentID:        "agent-1",
		ConversationID: "conv-1",
		Arguments:      `{"title":"交付清单","kind":"text","content":"a\nb"}`,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Content, "已保存产物") || !strings.Contains(result.Content, "交付清单") {
		t.Fatalf("工具结果 = %q", result.Content)
	}
	items := svc.Artifacts(contract.ArtifactFilter{Limit: 10})
	if len(items) != 1 || items[0].Title != "交付清单" {
		t.Fatalf("产物列表 = %+v", items)
	}
}

func TestSaveArtifactValidation(t *testing.T) {
	svc, _, root := newArtifactStack(t)
	ctx := context.Background()

	if _, err := svc.SaveArtifact(ctx, "", tool.ArtifactInput{
		Title: "坏链接", Kind: "link", Content: "ftp://example.com/file",
	}); err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatalf("非法链接未被拒绝：%v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("准备外部文件失败：%v", err)
	}
	if _, err := svc.SaveArtifact(ctx, "", tool.ArtifactInput{
		Title: "外部文件", Kind: "file", SourcePath: outside,
	}); err == nil || !strings.Contains(err.Error(), "数据目录") {
		t.Fatalf("外部来源未被拒绝：%v", err)
	}

	inside := filepath.Join(root, "tmp", "inside.txt")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatalf("创建目录失败：%v", err)
	}
	if err := os.WriteFile(inside, []byte("hello"), 0o600); err != nil {
		t.Fatalf("准备内部文件失败：%v", err)
	}
	ref, err := svc.SaveArtifact(ctx, "", tool.ArtifactInput{
		Title: "内部文件", Kind: "file", SourcePath: inside,
	})
	if err != nil {
		t.Fatalf("保存内部文件失败：%v", err)
	}
	if detail := svc.Artifact(ref.ID); detail.Size != 5 {
		t.Fatalf("文件大小 = %d, want 5", detail.Size)
	}

	if _, err := svc.SaveArtifact(ctx, "", tool.ArtifactInput{
		Title: "伪装图片", Kind: "image", SourcePath: inside,
	}); err == nil {
		t.Fatal("非图片文件不应被保存为图片")
	}

	ref, err = svc.SaveArtifact(ctx, "", tool.ArtifactInput{
		Title: "路径穿越", Kind: "text", Filename: "../../evil.txt", Content: "safe",
	})
	if err != nil {
		t.Fatalf("保存文本失败：%v", err)
	}
	detail := svc.Artifact(ref.ID)
	if detail.Filename != "evil.txt" || !strings.HasPrefix(detail.Path, filepath.Clean(root)) {
		t.Fatalf("文件名清洗不匹配：%+v", detail)
	}

	tooLarge := strings.Repeat("a", maxTextArtifactSize+1)
	if _, err := svc.SaveArtifact(ctx, "", tool.ArtifactInput{
		Title: "超大文本", Kind: "text", Content: tooLarge,
	}); err == nil {
		t.Fatal("超大文本未被拒绝")
	}
}
