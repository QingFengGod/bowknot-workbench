package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/infra/tool"
)

func memoryTestStore(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

// TestRememberClampsHallucinatedDate 验证模型编造的久远日期会回退到当前时间。
func TestRememberClampsHallucinatedDate(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	svc := NewMemoryService(st, testLogger(), fastFake())
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }

	item, err := svc.Remember(context.Background(), "agent-1", "conv", "msg", "我喜欢看英雄联盟游戏视频", "fact",
		time.Date(2026, 7, 19, 10, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if item.DayKey != "2026-11-17" || item.OccurredAt != now.UnixMilli() {
		t.Fatalf("久远日期未回退：day=%s occurred=%d", item.DayKey, item.OccurredAt)
	}

	// 近期日期仍然保留，用于记录「昨天」这类明确时间。
	recent := now.Add(-30 * time.Hour)
	item, err = svc.Remember(context.Background(), "agent-1", "conv", "msg", "我昨天去看了一场电影", "event", recent)
	if err != nil {
		t.Fatalf("remember recent: %v", err)
	}
	if item.DayKey != dayKeyOf(recent) {
		t.Fatalf("近期日期被改写：day=%s want=%s", item.DayKey, dayKeyOf(recent))
	}
}

func TestRememberWritesScopedMemoriesAndDedupes(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	ctx := context.Background()
	svc := NewMemoryService(st, testLogger(), fastFake())
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	// 子 Agent 的记忆全部写入它自己的专属域，不进入用户全局域。
	if _, err := svc.Remember(ctx, "agent-1", "conv-1", "msg-1", "用户偏好中文回复", "preference", time.Time{}); err != nil {
		t.Fatalf("remember agent memory: %v", err)
	}
	if items := svc.Memories("user", ""); len(items) != 0 {
		t.Fatalf("子 Agent 不应写入用户全局记忆：%+v", items)
	}
	if items := svc.Memories("agent", "agent-1"); len(items) != 1 {
		t.Fatalf("Agent 记忆 = %+v", items)
	}

	// 相同内容重复写入只更新置信度，不新增记录。
	if _, err := svc.Remember(ctx, "agent-1", "conv-1", "msg-1", "用户偏好中文回复", "preference", time.Time{}); err != nil {
		t.Fatalf("remember duplicate: %v", err)
	}
	if items := svc.Memories("agent", "agent-1"); len(items) != 1 {
		t.Fatalf("重复写入产生多条记忆：%+v", items)
	}

	// 主助手写入用户全局域，与子 Agent 隔离。
	if _, err := svc.Remember(ctx, mainAgentID, "conv-2", "msg-2", "用户在做支付重构", "fact", time.Time{}); err != nil {
		t.Fatalf("remember user memory: %v", err)
	}
	if items := svc.Memories("user", ""); len(items) != 1 {
		t.Fatalf("用户全局记忆 = %+v", items)
	}

	// 敏感信息与空内容直接拒绝。
	if _, err := svc.Remember(ctx, mainAgentID, "conv-2", "msg-2", "我的密码是 123456", "fact", time.Time{}); err == nil {
		t.Fatal("敏感信息不应写入")
	}
	if _, err := svc.Remember(ctx, mainAgentID, "conv-2", "msg-2", "   ", "fact", time.Time{}); err == nil {
		t.Fatal("空内容不应写入")
	}

	// 关闭某个 Agent 的开关后不再写入该域。
	if err := svc.SetMemoryEnabled("agent-1", false); err != nil {
		t.Fatalf("disable memory: %v", err)
	}
	if _, err := svc.Remember(ctx, "agent-1", "conv-1", "msg-1", "另一个事实", "fact", time.Time{}); err == nil {
		t.Fatal("关闭开关后仍然写入")
	}
}

func TestPromptBlockKeepsAgentMemorySeparate(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	ctx := context.Background()
	svc := NewMemoryService(st, testLogger(), fastFake())
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	now := time.Now()
	insert := func(id, scope, owner, content string) {
		t.Helper()
		if err := st.Memories.Insert(ctx, domain.Memory{
			ID: id, Scope: domain.MemoryScope(scope), OwnerID: owner, Kind: domain.MemoryKindFact,
			Content: content, ContentHash: hashContent(content), OccurredAt: now.UnixMilli(),
			DayKey: dayKeyOf(now), Status: domain.MemoryActive, Pinned: true,
			Confidence: 0.9, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
		}); err != nil {
			t.Fatalf("insert memory %s: %v", id, err)
		}
	}
	insert("m-user", "user", userMemoryOwnerID, "主助手记录的用户偏好")
	insert("m-agent", "agent", "agent-1", "该 Agent 专有的项目背景")

	mainBlock := svc.PromptBlock(mainAgentID, "")
	if !strings.Contains(mainBlock, "主助手记录的用户偏好") || strings.Contains(mainBlock, "该 Agent 专有的项目背景") {
		t.Fatalf("主助手记忆块串到 Agent 记忆：%s", mainBlock)
	}
	agentBlock := svc.PromptBlock("agent-1", "")
	if !strings.Contains(agentBlock, "该 Agent 专有的项目背景") || strings.Contains(agentBlock, "主助手记录的用户偏好") {
		t.Fatalf("Agent 记忆块串到用户全局记忆：%s", agentBlock)
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
	insert("m-pinned", "置顶的偏好：少用列表", true, now.AddDate(0, 0, -60).UnixMilli())
	insert("m-recent", "用户最近在推进支付重构", false, now.AddDate(0, 0, -2).UnixMilli())
	insert("m-old", "很久以前的一次无关记录", false, now.AddDate(0, 0, -90).UnixMilli())

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

func TestWeekDigestGeneratedAndSearchable(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	ctx := context.Background()
	fake := llm.NewFake(llm.FakeOptions{
		CompleteReply: func(call int, req llm.ChatRequest) string {
			return `{"summary":"用户确认了支付重构方案","keywords":["支付","重构"]}`
		},
	})
	svc := NewMemoryService(st, testLogger(), fake)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	now := time.Now()
	content := "用户决定本周推进支付重构"
	if err := st.Memories.Insert(ctx, domain.Memory{
		ID: "m-1", Scope: domain.MemoryScopeUser, OwnerID: userMemoryOwnerID, Kind: domain.MemoryKindDecision,
		Content: content, ContentHash: hashContent(content), OccurredAt: now.UnixMilli(), DayKey: dayKeyOf(now),
		Status: domain.MemoryActive, Confidence: 0.9,
		// 更新时间放到 40 天前，确保检索只能通过周摘要命中，而不是最近窗口。
		CreatedAt: now.AddDate(0, 0, -40).UnixMilli(), UpdatedAt: now.AddDate(0, 0, -40).UnixMilli(),
	}); err != nil {
		t.Fatalf("insert memory: %v", err)
	}

	svc.ensureDigests()
	period := weekPeriodOf(now)
	digest, err := st.MemoryDigests.Get(ctx, domain.MemoryScopeUser, userMemoryOwnerID, domain.DigestWeek, period.start)
	if err != nil {
		t.Fatalf("周摘要未生成：%v", err)
	}
	if digest.Summary != "用户确认了支付重构方案" {
		t.Fatalf("摘要内容 = %q", digest.Summary)
	}

	// 检索先走周期摘要，再按日期范围取原文。
	items := svc.Search(ctx, mainAgentID, "支付重构", "", "", 8)
	if len(items) != 1 || items[0].Content != content {
		t.Fatalf("摘要检索结果异常：%+v", items)
	}

	// 修改记忆后周摘要标记 dirty，并在下次任务中重建。
	if err := svc.UpdateMemory(ctx, mainAgentID, "m-1", "用户决定下周推进支付重构"); err != nil {
		t.Fatalf("update memory: %v", err)
	}
	digest, err = st.MemoryDigests.Get(ctx, domain.MemoryScopeUser, userMemoryOwnerID, domain.DigestWeek, period.start)
	if err != nil {
		t.Fatalf("get digest: %v", err)
	}
	if !digest.Dirty {
		t.Fatal("修改记忆后周摘要未标记 dirty")
	}
	svc.ensureDigests()
	digest, err = st.MemoryDigests.Get(ctx, domain.MemoryScopeUser, userMemoryOwnerID, domain.DigestWeek, period.start)
	if err != nil || digest.Dirty {
		t.Fatalf("周摘要未重建：err=%v dirty=%v", err, digest.Dirty)
	}
	if items := svc.Search(ctx, mainAgentID, "支付重构", "", "", 8); len(items) != 1 || !strings.Contains(items[0].Content, "下周") {
		t.Fatalf("重建后检索内容异常：%+v", items)
	}

	// 子 Agent 不能修改或删除主助手的记忆。
	if err := svc.UpdateMemory(ctx, "agent-1", "m-1", "越权修改"); err == nil {
		t.Fatal("子 Agent 越权修改未被拒绝")
	}
	if err := svc.DeleteMemory(ctx, "agent-1", "m-1"); err == nil {
		t.Fatal("子 Agent 越权删除未被拒绝")
	}

	// 按日期范围可以直接定位原文。
	day := dayKeyOf(now)
	if items := svc.Search(ctx, mainAgentID, "", day, day, 8); len(items) != 1 {
		t.Fatalf("按日期检索失败：%+v", items)
	}
}

func TestAssistantWritesMemoryByTool(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 4096,
		Reply: func(call int, req llm.ChatRequest) llm.FakeReply {
			if call == 0 {
				return llm.FakeReply{ToolCalls: []llm.ToolCall{{
					ID:        "call-1",
					Name:      tool.RememberToolID,
					Arguments: `{"content":"我喜欢中文回复","kind":"preference"}`,
				}}}
			}
			return llm.FakeReply{Content: "好的"}
		},
	})
	memory := NewMemoryService(st, testLogger(), provider)
	t.Cleanup(func() { _ = memory.Shutdown(context.Background()) })
	executor := NewToolExecutor(st, nil)
	executor.SetMemory(memory)
	tools := tool.NewService(tool.Options{Store: st, Executor: executor, Logger: testLogger()})
	tools.RegisterBuiltin(tool.MemoryToolSpecs()...)

	svc := NewAssistantService(st, testLogger(), provider, tools, NewApprovalHub())
	svc.SetMemory(memory)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	session := svc.CreateSession()
	messageID, events, err := svc.Send(session.ID, "我更喜欢中文", "bk-pro")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	var sawToolEvent bool
	collectEvents(t, events, func(ev contract.ChatEvent) {
		if ev.Type == contract.ChatEventToolCall && ev.ToolName == tool.RememberToolID {
			sawToolEvent = true
		}
	})
	if sawToolEvent {
		t.Fatal("记忆工具不应向 UI 推送工具卡片")
	}

	items := memory.Memories("user", "")
	if len(items) != 1 || items[0].Content != "我喜欢中文回复" {
		t.Fatalf("工具未写入记忆：%+v", items)
	}
	messages := svc.Messages(session.ID)
	if len(messages) != 2 || !messages[1].MemoryWritten {
		t.Fatalf("消息记忆标记异常：%+v", messages)
	}

	// 工具调用仍然落库，保留审计能力。
	calls, err := st.ToolCalls.ListByMessage(context.Background(), messageID)
	if err != nil {
		t.Fatalf("list tool calls: %v", err)
	}
	if len(calls) != 1 || calls[0].ToolID != tool.RememberToolID {
		t.Fatalf("工具调用记录异常：%+v", calls)
	}
}
