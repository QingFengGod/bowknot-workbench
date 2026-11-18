package store

import (
	"context"
	"testing"

	"bowknot-workbench/internal/domain"
)

func newTask(id string) domain.Task {
	return domain.Task{
		ID:                   id,
		Title:                "写一句标语",
		Brief:                "为新品写一句标语",
		Status:               domain.TaskQueued,
		AssignerKind:         domain.AssignerMain,
		AssignerID:           "main-agent",
		AssigneeID:           "agent-1",
		ThreadConversationID: "conv-" + id,
		CreatedAt:            1000,
	}
}

func TestTaskLifecycleTransitions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Tasks.Create(ctx, newTask("t-1")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	ok, err := s.Tasks.Claim(ctx, "t-1", 2000)
	if err != nil || !ok {
		t.Fatalf("Claim() = %v, %v", ok, err)
	}
	// 重复认领必须失败，保证状态迁移只发生一次。
	if ok, _ := s.Tasks.Claim(ctx, "t-1", 3000); ok {
		t.Fatal("重复 Claim 不应成功")
	}

	ok, err = s.Tasks.Finish(ctx, "t-1", domain.TaskDone, "结果", "msg-1", "", 4000)
	if err != nil || !ok {
		t.Fatalf("Finish() = %v, %v", ok, err)
	}
	if ok, _ := s.Tasks.Finish(ctx, "t-1", domain.TaskFailed, "", "", "迟到事件", 5000); ok {
		t.Fatal("终态任务不应再次迁移")
	}

	got, err := s.Tasks.Get(ctx, "t-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != domain.TaskDone || got.ResultSummary != "结果" || got.ResultMessageID != "msg-1" {
		t.Fatalf("任务终态不匹配：%+v", got)
	}

	ok, err = s.Tasks.Requeue(ctx, "t-1", 6000)
	if err != nil || !ok {
		t.Fatalf("Requeue() = %v, %v", ok, err)
	}
	got, _ = s.Tasks.Get(ctx, "t-1")
	if got.Status != domain.TaskQueued || got.Attempt != 1 || got.ResultSummary != "" || got.ReportedAt != 0 {
		t.Fatalf("重试后任务不匹配：%+v", got)
	}

	count, err := s.Tasks.ConvergeInterrupted(ctx)
	if err != nil || count != 1 {
		t.Fatalf("ConvergeInterrupted() = %d, %v", count, err)
	}
	got, _ = s.Tasks.Get(ctx, "t-1")
	if got.Status != domain.TaskFailed || got.Error == "" {
		t.Fatalf("收敛后任务不匹配：%+v", got)
	}
}

// RetryLater 把运行中的任务退回排队，并累计尝试次数。
func TestTaskRetryLater(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Tasks.Create(ctx, newTask("t-retry")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if ok, err := s.Tasks.Claim(ctx, "t-retry", 2000); err != nil || !ok {
		t.Fatalf("Claim() = %v, %v", ok, err)
	}

	ok, err := s.Tasks.RetryLater(ctx, "t-retry", "模型请求触发限流")
	if err != nil || !ok {
		t.Fatalf("RetryLater() = %v, %v", ok, err)
	}
	got, err := s.Tasks.Get(ctx, "t-retry")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != domain.TaskQueued || got.Attempt != 1 {
		t.Fatalf("退回排队后任务不匹配：%+v", got)
	}
	if got.StartedAt != 0 || got.FinishedAt != 0 || got.Error != "模型请求触发限流" {
		t.Fatalf("退回排队后的时间与错误不匹配：%+v", got)
	}

	// 非运行中的任务不能再走退避重试。
	if ok, _ := s.Tasks.RetryLater(ctx, "t-retry", "重复调用"); ok {
		t.Fatal("排队中的任务不应再次 RetryLater")
	}
}

func TestMessageTaskMetaRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Conversations.Create(ctx, conversation("conv-meta", 100)); err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	item := message("msg-meta", "conv-meta", domain.RoleAssistant)
	item.SenderKind = domain.SenderMain
	item.SenderID = "main-agent"
	item.MessageType = domain.MessageTypeTaskReport
	item.TaskID = "t-1"
	item.Visibility = domain.VisibilityUser
	if _, err := s.Messages.Append(ctx, item); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	got, err := s.Messages.Get(ctx, "msg-meta")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.SenderKind != domain.SenderMain || got.MessageType != domain.MessageTypeTaskReport ||
		got.TaskID != "t-1" || got.Visibility != domain.VisibilityUser {
		t.Fatalf("消息元信息不匹配：%+v", got)
	}

	// 旧数据只写了原有列，新增列必须落到默认值。
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO message(id, conversation_id, seq, role, content, status, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		"msg-legacy", "conv-meta", 2, "user", "旧消息", "done", 1000,
	); err != nil {
		t.Fatalf("插入旧消息失败：%v", err)
	}
	got, _ = s.Messages.Get(ctx, "msg-legacy")
	if got.MessageType != domain.MessageTypeChat || got.Visibility != domain.VisibilityUser || got.TaskID != "" {
		t.Fatalf("旧消息默认值不匹配：%+v", got)
	}
}
