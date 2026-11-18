package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/infra/tool"
)

// flakyStreamProvider 前 failures 次 Stream 返回限流错误，之后交给 Fake；
// 用于验证任务级的退避重试。
type flakyStreamProvider struct {
	inner    *llm.Fake
	mu       sync.Mutex
	failures int
	streams  int
}

func (p *flakyStreamProvider) Stream(ctx context.Context, req llm.ChatRequest) (<-chan llm.Chunk, error) {
	p.mu.Lock()
	p.streams++
	fail := p.streams <= p.failures
	p.mu.Unlock()
	if fail {
		return nil, &llm.Error{Kind: llm.KindRateLimit, Message: "限流"}
	}
	return p.inner.Stream(ctx, req)
}

func (p *flakyStreamProvider) Complete(ctx context.Context, req llm.ChatRequest) (string, error) {
	return p.inner.Complete(ctx, req)
}

func (p *flakyStreamProvider) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	return p.inner.Models(ctx)
}

// 临时错误自动退避重试，中间失败不向用户汇报。
func TestTaskAutoRetriesTransientErrors(t *testing.T) {
	oldDelays := taskRetryDelays
	taskRetryDelays = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	t.Cleanup(func() { taskRetryDelays = oldDelays })

	provider := &flakyStreamProvider{
		inner: llm.NewFake(llm.FakeOptions{
			Interval:  time.Millisecond,
			ChunkSize: 1024,
			Reply: func(int, llm.ChatRequest) llm.FakeReply {
				return llm.FakeReply{Content: "标语：让工作更轻。"}
			},
		}),
		failures: 2,
	}
	agents, assistant, tasks := newTaskStack(t, provider)
	info := agents.CreateAgent("执行助手", "负责执行任务", "")
	session := assistant.CreateSession()

	detail, err := tasks.Dispatch(context.Background(), TaskDispatchInput{
		AgentID:              info.ID,
		Brief:                "写一句产品标语",
		OriginConversationID: session.ID,
	})
	if err != nil {
		t.Fatalf("派发任务失败：%v", err)
	}

	final := waitTaskTerminal(t, tasks, detail.ID)
	if final.Status != contract.TaskDone {
		t.Fatalf("任务状态 = %s, want done（错误：%s）", final.Status, final.Error)
	}
	if final.Attempt != 2 {
		t.Fatalf("尝试次数 = %d, want 2", final.Attempt)
	}
	if final.ResultSummary == "" {
		t.Fatal("自动重试成功后结果摘要为空")
	}

	provider.mu.Lock()
	streams := provider.streams
	provider.mu.Unlock()
	if streams != 3 {
		t.Fatalf("模型调用次数 = %d, want 3", streams)
	}

	reports := 0
	for _, message := range assistant.Messages(session.ID) {
		if message.MessageType == "task_report" {
			reports++
		}
	}
	if reports != 1 {
		t.Fatalf("任务汇报数量 = %d, want 1（中间失败不应汇报）", reports)
	}
}

// 审计统计按时间范围汇总任务，并支持按标题与描述检索。
func TestTaskAuditStatsAndSearch(t *testing.T) {
	agents, _, tasks := newTaskStack(t, llm.NewFake(llm.FakeOptions{}))
	info := agents.CreateAgent("统计助手", "负责统计", "")
	ctx := context.Background()
	now := time.Now().UnixMilli()

	seed := func(id, title, brief string, status domain.TaskStatus, created, started, finished int64) {
		t.Helper()
		err := tasks.store.Tasks.Create(ctx, domain.Task{
			ID:                   id,
			Title:                title,
			Brief:                brief,
			Status:               status,
			AssignerKind:         domain.AssignerMain,
			AssignerID:           mainAgentID,
			AssigneeID:           info.ID,
			ThreadConversationID: "thread-" + id,
			CreatedAt:            created,
			StartedAt:            started,
			FinishedAt:           finished,
		})
		if err != nil {
			t.Fatalf("创建任务 %s 失败：%v", id, err)
		}
	}

	hour := int64(time.Hour / time.Millisecond)
	seed("t-done", "写一句产品标语", "为新品写一句标语", domain.TaskDone, now-2*hour, now-2*hour, now-hour)
	seed("t-failed", "整理竞品清单", "整理一份竞品清单", domain.TaskFailed, now-hour, now-hour, now)
	seed("t-queued", "准备周报素材", "收集本周数据", domain.TaskQueued, now-hour, 0, 0)
	seed("t-old", "很久以前的任务", "历史任务", domain.TaskDone, now-10*24*hour, 0, 0)

	all := tasks.TaskAudit(0)
	if all.Overall.Total != 4 || all.Overall.Done != 2 || all.Overall.Failed != 1 || all.Overall.Queued != 1 {
		t.Fatalf("全部时间统计不匹配：%+v", all.Overall)
	}
	if got := all.Overall.SuccessRate; got < 0.66 || got > 0.67 {
		t.Fatalf("成功率 = %f, want 2/3", got)
	}
	if all.Overall.AvgDurationMs != hour {
		t.Fatalf("平均耗时 = %d, want %d", all.Overall.AvgDurationMs, hour)
	}
	if len(all.Agents) != 1 || all.Agents[0].AgentName != "统计助手" {
		t.Fatalf("员工统计不匹配：%+v", all.Agents)
	}
	if len(all.Recent) != 4 {
		t.Fatalf("最近任务数量 = %d, want 4", len(all.Recent))
	}

	recent := tasks.TaskAudit(7)
	if recent.Overall.Total != 3 || recent.Overall.Done != 1 {
		t.Fatalf("近 7 天统计不匹配：%+v", recent.Overall)
	}

	if got := tasks.SearchTasks("标语", 10); len(got) != 1 || got[0].ID != "t-done" {
		t.Fatalf("按标题检索结果 = %+v", got)
	}
	if got := tasks.SearchTasks("竞品", 10); len(got) != 1 || got[0].ID != "t-failed" {
		t.Fatalf("按描述检索结果 = %+v", got)
	}
	if got := tasks.SearchTasks("", 10); len(got) != 0 {
		t.Fatalf("空关键词检索结果 = %+v", got)
	}

	hub := NewAgentHub(agents, tasks)
	refs, err := hub.ListTasks(ctx, tool.ListTasksInput{Query: "竞品", Limit: 5})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "t-failed" {
		t.Fatalf("工具检索结果 = %+v", refs)
	}
}

// 任务结束后写入员工自己的记忆快照；关闭记忆时跳过。
func TestTaskWritesMemorySnapshot(t *testing.T) {
	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 1024,
		Reply: func(int, llm.ChatRequest) llm.FakeReply {
			return llm.FakeReply{Content: "已经整理好了。"}
		},
	})
	agents, _, tasks := newTaskStack(t, provider)
	memory := NewMemoryService(tasks.store, testLogger(), provider)
	tasks.SetMemory(memory)

	enabled := agents.CreateAgent("记忆助手", "负责执行", "")
	if _, err := tasks.Dispatch(context.Background(), TaskDispatchInput{
		AgentID: enabled.ID,
		Brief:   "整理一份周报",
	}); err != nil {
		t.Fatalf("派发任务失败：%v", err)
	}
	tasks.Wait()

	items := memory.Memories("agent", enabled.ID)
	if len(items) != 1 {
		t.Fatalf("记忆数量 = %d, want 1", len(items))
	}
	if !strings.Contains(items[0].Content, "整理一份周报") || items[0].Kind != "event" {
		t.Fatalf("记忆快照不匹配：%+v", items[0])
	}

	disabled := agents.CreateAgent("无记忆助手", "负责执行", "")
	if err := memory.SetMemoryEnabled(disabled.ID, false); err != nil {
		t.Fatalf("关闭记忆失败：%v", err)
	}
	if _, err := tasks.Dispatch(context.Background(), TaskDispatchInput{
		AgentID: disabled.ID,
		Brief:   "再整理一份",
	}); err != nil {
		t.Fatalf("派发任务失败：%v", err)
	}
	tasks.Wait()

	if items := memory.Memories("agent", disabled.ID); len(items) != 0 {
		t.Fatalf("关闭记忆后仍写入：%+v", items)
	}
}

func waitTaskTerminal(t *testing.T, tasks *TaskService, taskID string) contract.TaskDetail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		detail := tasks.Task(taskID)
		if detail.Status.Terminal() {
			return detail
		}
		time.Sleep(5 * time.Millisecond)
	}
	detail := tasks.Task(taskID)
	t.Fatalf("等待任务 %s 进入终态超时，当前 %s", taskID, detail.Status)
	return detail
}
