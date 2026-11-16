package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/llm"
)

func memoryTestStore(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func TestExtractAsyncWritesScopedMemories(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	ctx := context.Background()
	fake := llm.NewFake(llm.FakeOptions{
		CompleteReply: func(call int, req llm.ChatRequest) string {
			return `[
				{"scope":"user","kind":"preference","content":"用户偏好中文回复","keywords":["中文","回复"],"confidence":0.9},
				{"scope":"agent","kind":"fact","content":"用户负责支付模块","keywords":["支付"],"confidence":0.8}
			]`
		},
	})
	svc := NewMemoryService(st, testLogger(), fake)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	if err := st.Conversations.Create(ctx, domain.Conversation{
		ID: "conv-1", Kind: domain.KindAgent, OwnerID: "agent-1", Title: "t", CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := st.Messages.Append(ctx, domain.Message{
		ID: "msg-1", ConversationID: "conv-1", Role: domain.RoleAssistant, Status: domain.MessageDone, CreatedAt: 1,
	}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	svc.ExtractAsync("agent-1", "conv-1", "msg-1", "我偏好中文回复，负责支付模块", "好的")
	svc.wg.Wait()

	if items := svc.Memories("user", ""); len(items) != 1 || items[0].Content != "用户偏好中文回复" {
		t.Fatalf("用户记忆 = %+v", items)
	}
	if items := svc.Memories("agent", "agent-1"); len(items) != 1 || items[0].Content != "用户负责支付模块" {
		t.Fatalf("Agent 记忆 = %+v", items)
	}
	message, err := st.Messages.Get(ctx, "msg-1")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if !message.MemoryWritten {
		t.Fatal("抽取后没有标记消息")
	}

	// 关闭全局开关后不再抽取。
	if err := svc.SetMemoryEnabled("", false); err != nil {
		t.Fatalf("disable memory: %v", err)
	}
	svc.ExtractAsync("agent-1", "conv-1", "msg-1", "另一个事实", "好的")
	svc.wg.Wait()
	if items := svc.Memories("user", ""); len(items) != 1 {
		t.Fatalf("关闭后仍新增记忆：%+v", items)
	}
	// 关闭全局开关只影响用户作用域，Agent 专属记忆仍可写入且会去重。
	if items := svc.Memories("agent", "agent-1"); len(items) != 1 {
		t.Fatalf("Agent 记忆重复写入：%+v", items)
	}
}

func TestPromptBlockInjectsPinnedRecentAndBudgets(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	ctx := context.Background()
	svc := NewMemoryService(st, testLogger(), fastFake())
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	now := time.Now()
	insert := func(id, content string, pinned bool, updatedAt int64) {
		if err := st.Memories.Insert(ctx, domain.Memory{
			ID: id, Scope: domain.MemoryScopeUser, OwnerID: "main", Kind: domain.MemoryKindPreference,
			Content: content, ContentHash: hashContent(content), OccurredAt: updatedAt, DayKey: dayKeyOf(now),
			Status: domain.MemoryActive, Pinned: pinned, Confidence: 0.8,
			CreatedAt: updatedAt, UpdatedAt: updatedAt,
		}); err != nil {
			t.Fatalf("insert memory %s: %v", id, err)
		}
	}
	insert("m-pinned", "置顶的偏好：少用列表", true, now.AddDate(0, 0, -30).UnixMilli())
	insert("m-recent", "用户最近在推进支付重构", false, now.AddDate(0, 0, -2).UnixMilli())
	insert("m-old", "很久以前的一次无关记录", false, now.AddDate(0, 0, -60).UnixMilli())

	block := svc.PromptBlock(mainAgentID, "今天聊聊支付重构")
	if !strings.Contains(block, "置顶的偏好") || !strings.Contains(block, "支付重构") {
		t.Fatalf("注入内容不完整：%s", block)
	}
	if strings.Contains(block, "很久以前") {
		t.Fatalf("不该注入过期且不相关的记忆：%s", block)
	}
	if !strings.Contains(block, "[用户全局记忆]") {
		t.Fatalf("缺少记忆块标题：%s", block)
	}
	if utf8.RuneCountInString(block) > memoryBudgetRunes+64 {
		t.Fatalf("记忆块超出预算：%d", utf8.RuneCountInString(block))
	}

	if err := svc.SetMemoryEnabled("", false); err != nil {
		t.Fatalf("disable memory: %v", err)
	}
	if block := svc.PromptBlock(mainAgentID, "支付重构"); block != "" {
		t.Fatalf("关闭后仍注入记忆：%s", block)
	}
}

func TestDayDigestGeneratedOnDemandAndRebuiltAfterEdit(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	ctx := context.Background()
	fake := llm.NewFake(llm.FakeOptions{
		CompleteReply: func(call int, req llm.ChatRequest) string {
			if strings.Contains(req.System, "摘要器") {
				return `{"summary":"用户确认了支付重构方案","keywords":["支付","重构"]}`
			}
			return "[]"
		},
	})
	svc := NewMemoryService(st, testLogger(), fake)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	now := time.Now()
	content := "用户决定本周推进支付重构"
	if err := st.Memories.Insert(ctx, domain.Memory{
		ID: "m-1", Scope: domain.MemoryScopeUser, OwnerID: "main", Kind: domain.MemoryKindDecision,
		Content: content, ContentHash: hashContent(content), OccurredAt: now.UnixMilli(), DayKey: dayKeyOf(now),
		Status: domain.MemoryActive, Confidence: 0.9, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
	}); err != nil {
		t.Fatalf("insert memory: %v", err)
	}

	block := svc.PromptBlock(mainAgentID, "上次那个支付重构方案是什么")
	if !strings.Contains(block, content) {
		t.Fatalf("时间线索未展开原子记忆：%s", block)
	}
	digest, err := st.MemoryDigests.Get(ctx, domain.MemoryScopeUser, "main", domain.DigestDay, dayKeyOf(now))
	if err != nil {
		t.Fatalf("日摘要未生成：%v", err)
	}
	if digest.Summary != "用户确认了支付重构方案" {
		t.Fatalf("摘要内容 = %q", digest.Summary)
	}
	if fake.Completes() == 0 {
		t.Fatal("未调用模型生成摘要")
	}

	// 编辑记忆后摘要应标记 dirty，并在下次检索时重建。
	if err := svc.UpdateMemory("m-1", "用户决定下周推进支付重构"); err != nil {
		t.Fatalf("update memory: %v", err)
	}
	digest, err = st.MemoryDigests.Get(ctx, domain.MemoryScopeUser, "main", domain.DigestDay, dayKeyOf(now))
	if err != nil {
		t.Fatalf("get digest: %v", err)
	}
	if !digest.Dirty {
		t.Fatal("编辑记忆后摘要未标记 dirty")
	}
	if block := svc.PromptBlock(mainAgentID, "还记得上次支付重构吗"); !strings.Contains(block, "下周推进支付重构") {
		t.Fatalf("重建后注入内容异常：%s", block)
	}
}

func TestAssistantExtractsMemoryAfterReply(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	fake := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 4096,
		Reply: func(call int, req llm.ChatRequest) llm.FakeReply {
			return llm.FakeReply{Content: "好的，我记下了"}
		},
		CompleteReply: func(call int, req llm.ChatRequest) string {
			return `[{"scope":"user","kind":"preference","content":"用户偏好中文回复","keywords":["中文"],"confidence":0.9}]`
		},
	})
	memory := NewMemoryService(st, testLogger(), fake)
	t.Cleanup(func() { _ = memory.Shutdown(context.Background()) })

	svc := NewAssistantService(st, testLogger(), fake, nil, nil)
	svc.SetMemory(memory)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	session := svc.CreateSession()
	_, events, err := svc.Send(session.ID, "我喜欢中文回复", "bk-pro")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	drainEvents(t, events)
	memory.wg.Wait()

	if items := memory.Memories("user", ""); len(items) != 1 || items[0].Content != "用户偏好中文回复" {
		t.Fatalf("回复后未写入记忆：%+v", items)
	}
	messages := svc.Messages(session.ID)
	if len(messages) != 2 || !messages[1].MemoryWritten {
		t.Fatalf("消息记忆标记异常：%+v", messages)
	}
}
