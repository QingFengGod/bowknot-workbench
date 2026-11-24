package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/infra/tool"
)

// newTaskStack 组装一套带任务服务的最小依赖。
func newTaskStack(t *testing.T, provider llm.Provider) (*AgentService, *AssistantService, *TaskService) {
	t.Helper()
	st := openTestStore(t, memoryTestStore(t), false)
	assistant := NewAssistantService(st, testLogger(), provider, nil, nil)
	agents := NewAgentService(st, testLogger(), provider, nil, nil)
	agents.SetModels([]contract.ChatModel{{ID: "fake", Name: "Fake"}})
	tasks := NewTaskService(st, testLogger(), provider, agents, assistant)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tasks.Shutdown(ctx)
		_ = agents.Shutdown(ctx)
		_ = assistant.Shutdown(ctx)
	})
	return agents, assistant, tasks
}

// 派发应立即返回任务 ID，员工完成后向来源会话写入带任务元信息的汇报。
func TestAgentHubDispatchesTaskAndReports(t *testing.T) {
	provider := llm.NewFake(llm.FakeOptions{
		Interval:      time.Millisecond,
		ChunkSize:     1024,
		CompleteReply: func(int, llm.ChatRequest) string { return "文案已交付，效果不错。" },
	})
	agents, assistant, tasks := newTaskStack(t, provider)
	info := agents.CreateAgent("文案助手", "负责产品文案", "")
	if info.ID == "" {
		t.Fatal("创建 Agent 失败")
	}

	hub := NewAgentHub(agents, tasks)
	session := assistant.CreateSession()
	reply, err := hub.RunAgent(context.Background(), tool.RunAgentInput{
		AgentID:        info.ID,
		Task:           "写一句产品标语",
		ConversationID: session.ID,
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if !strings.Contains(reply, "任务 ID") {
		t.Fatalf("委派回复 = %q", reply)
	}

	tasks.Wait()

	items := tasks.Tasks(info.ID, "", 10)
	if len(items) != 1 {
		t.Fatalf("任务数量 = %d, want 1", len(items))
	}
	task := items[0]
	if task.Status != contract.TaskDone {
		t.Fatalf("任务状态 = %s, want done（错误：%s）", task.Status, task.Error)
	}

	page := assistant.MessagesPage(session.ID, 0, 50)
	var report *contract.ChatMessage
	for i := range page.Items {
		if page.Items[i].MessageType == "task_report" {
			report = &page.Items[i]
			break
		}
	}
	if report == nil {
		t.Fatalf("来源会话没有任务汇报：%+v", page.Items)
	}
	if report.Task == nil || report.Task.ID != task.ID || report.Task.Status != contract.TaskDone {
		t.Fatalf("汇报任务摘要不匹配：%+v", report.Task)
	}
	if !strings.Contains(report.Content, "文案已交付") {
		t.Fatalf("汇报没有使用润色结果：%s", report.Content)
	}
	if assistant.Revision() == 0 {
		t.Fatal("后台汇报后 revision 没有递增")
	}
	if tasks.TaskRevision() == 0 {
		t.Fatal("任务状态变化后任务 revision 没有递增")
	}

	thread := tasks.TaskMessages(task.ID)
	if len(thread) == 0 {
		t.Fatal("任务线程没有消息")
	}
	if thread[0].MessageType != "task_dispatch" || thread[0].Visibility != "internal" {
		t.Fatalf("任务线程首条消息不匹配：%+v", thread[0])
	}
}

// 同一员工的任务串行执行，排队中的任务可以被取消。
func TestTaskServiceQueuesAndCancels(t *testing.T) {
	provider := llm.NewFake(llm.FakeOptions{
		Interval:      2 * time.Millisecond,
		ChunkSize:     1,
		CompleteReply: func(int, llm.ChatRequest) string { return "已完成。" },
	})
	agents, _, tasks := newTaskStack(t, provider)
	info := agents.CreateAgent("执行助手", "负责执行任务", "")

	first, err := tasks.Dispatch(context.Background(), TaskDispatchInput{
		AgentID: info.ID,
		Brief:   "第一个任务：写一段较长的工作说明",
	})
	if err != nil {
		t.Fatalf("派发第一个任务失败：%v", err)
	}
	second, err := tasks.DispatchTask(info.ID, "第二个任务：整理一份清单", "")
	if err != nil {
		t.Fatalf("派发第二个任务失败：%v", err)
	}

	// 等第一个任务进入运行中，第二个应保持排队。
	waitTaskStatus(t, tasks, first.ID, contract.TaskRunning)
	if got := tasks.Task(second.ID).Status; got != contract.TaskQueued {
		t.Fatalf("第二个任务状态 = %s, want queued", got)
	}

	if err := tasks.CancelTask(second.ID); err != nil {
		t.Fatalf("取消排队任务失败：%v", err)
	}
	if got := tasks.Task(second.ID).Status; got != contract.TaskCanceled {
		t.Fatalf("取消后状态 = %s, want canceled", got)
	}

	tasks.Wait()
	if got := tasks.Task(first.ID).Status; got != contract.TaskDone {
		t.Fatalf("第一个任务状态 = %s, want done", got)
	}

	// 取消后的任务可重试，重试沿用原线程并再次执行。
	if err := tasks.RetryTask(second.ID); err != nil {
		t.Fatalf("重试任务失败：%v", err)
	}
	tasks.Wait()
	if got := tasks.Task(second.ID).Status; got != contract.TaskDone {
		t.Fatalf("重试后状态 = %s, want done", got)
	}
}

// 直聊时注入员工自己的历史任务摘要，任务线程本身不重复注入。
func TestDirectChatInjectsOwnTaskHistory(t *testing.T) {
	var mu sync.Mutex
	systems := make([]string, 0, 2)

	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 1024,
		Reply: func(_ int, req llm.ChatRequest) llm.FakeReply {
			mu.Lock()
			systems = append(systems, req.System)
			mu.Unlock()
			return llm.FakeReply{Content: "已完成。"}
		},
		CompleteReply: func(int, llm.ChatRequest) string { return "标语已经写好。" },
	})
	agents, _, tasks := newTaskStack(t, provider)
	info := agents.CreateAgent("执行助手", "负责执行任务", "")

	if _, err := tasks.Dispatch(context.Background(), TaskDispatchInput{
		AgentID: info.ID,
		Brief:   "写一句产品标语",
	}); err != nil {
		t.Fatalf("派发任务失败：%v", err)
	}
	tasks.Wait()

	session := agents.CreateSession(info.ID)
	if session.ID == "" {
		t.Fatal("创建直聊会话失败")
	}
	_, events, err := agents.Send(session.ID, "你之前做过哪些任务？", "", nil)
	if err != nil {
		t.Fatalf("直聊发送失败：%v", err)
	}
	for range events {
	}

	mu.Lock()
	defer mu.Unlock()
	if len(systems) != 2 {
		t.Fatalf("模型调用次数 = %d, want 2", len(systems))
	}
	if strings.Contains(systems[0], "你的历史任务") {
		t.Fatal("任务线程不应注入历史任务块")
	}
	if !strings.Contains(systems[1], "你的历史任务") || !strings.Contains(systems[1], "写一句产品标语") {
		t.Fatalf("直聊未注入任务历史：%s", systems[1])
	}
}

func waitTaskStatus(t *testing.T, tasks *TaskService, taskID string, want contract.TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tasks.Task(taskID).Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待任务 %s 进入 %s 超时，当前 %s", taskID, want, tasks.Task(taskID).Status)
}
