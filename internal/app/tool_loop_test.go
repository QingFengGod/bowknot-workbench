package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/store"
)

// collectEvents 读到 Done 为止，超时视为测试失败。
func collectEvents(t *testing.T, events <-chan contract.ChatEvent, onEvent func(contract.ChatEvent)) []contract.ChatEvent {
	t.Helper()
	var out []contract.ChatEvent
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return out
			}
			out = append(out, ev)
			if onEvent != nil {
				onEvent(ev)
			}
			if ev.Done {
				return out
			}
		case <-timeout:
			t.Fatal("等待生成事件超时")
		}
	}
}

// seedAppTool 在 store 里登记一个已安装应用及其工具。
func seedAppTool(t *testing.T, st *store.Store, appID, toolName string, dangerous bool) {
	t.Helper()
	ctx := context.Background()
	if err := st.Apps.Insert(ctx, domain.App{
		ID:          appID,
		Name:        appID,
		DisplayName: appID + " 应用",
		Version:     "1.0.0",
		InstalledAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("seed app: %v", err)
	}
	if err := st.Apps.ReplaceTools(ctx, appID, []domain.Tool{{
		Name:        toolName,
		Description: "测试工具",
		Dangerous:   dangerous,
		Enabled:     true,
	}}); err != nil {
		t.Fatalf("seed tool: %v", err)
	}
}

func TestAssistantToolCallLoop(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	ctx := context.Background()
	seedAppTool(t, st, "demo", "runTask", false)

	tools := tool.NewService(tool.Options{Store: st, Logger: testLogger()})
	if err := tools.SyncApps(ctx); err != nil {
		t.Fatalf("sync apps: %v", err)
	}

	var mu sync.Mutex
	var requests []llm.ChatRequest
	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 4096,
		Reply: func(call int, req llm.ChatRequest) llm.FakeReply {
			mu.Lock()
			requests = append(requests, req)
			mu.Unlock()
			if call == 0 {
				return llm.FakeReply{ToolCalls: []llm.ToolCall{{
					ID:        "call-1",
					Name:      "app:demo/runTask",
					Arguments: `{"task":"周报"}`,
				}}}
			}
			return llm.FakeReply{Content: "已根据工具结果完成回答"}
		},
	})

	svc := NewAssistantService(st, testLogger(), provider, tools, NewApprovalHub())
	session := svc.CreateSession()
	messageID, events, err := svc.Send(session.ID, "帮我执行任务", "bk-pro", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	var sawCall, sawResult bool
	got := collectEvents(t, events, func(ev contract.ChatEvent) {
		switch ev.Type {
		case contract.ChatEventToolCall:
			sawCall = ev.ToolName == "app:demo/runTask" && ev.Arguments == `{"task":"周报"}`
		case contract.ChatEventToolResult:
			sawResult = ev.Result != "" && ev.Error == ""
		}
	})
	if !sawCall || !sawResult {
		t.Fatalf("缺少工具事件：call=%v result=%v", sawCall, sawResult)
	}
	last := got[len(got)-1]
	if last.Status != contract.MessageDone || !strings.Contains(last.Content, "工具结果") {
		t.Fatalf("最终回复异常：%+v", last)
	}

	calls, err := st.ToolCalls.ListByMessage(ctx, messageID)
	if err != nil {
		t.Fatalf("list tool calls: %v", err)
	}
	if len(calls) != 1 || calls[0].Status != domain.ToolCallOK || calls[0].ToolID != "app:demo/runTask" {
		t.Fatalf("工具调用记录 = %+v", calls)
	}

	messages := svc.Messages(session.ID)
	final := messages[len(messages)-1]
	if len(final.ToolCalls) != 1 || final.ToolCalls[0].Result == "" {
		t.Fatalf("消息未带上工具调用：%+v", final.ToolCalls)
	}

	mu.Lock()
	reqs := append([]llm.ChatRequest(nil), requests...)
	mu.Unlock()
	if len(reqs) != 2 {
		t.Fatalf("模型调用次数 = %d, want 2", len(reqs))
	}
	replayed := false
	for _, message := range reqs[1].Messages {
		if message.Role == "tool" && message.ToolCallID == "call-1" && message.Content != "" {
			replayed = true
		}
	}
	if !replayed {
		t.Fatal("第二轮请求缺少 role=tool 的结果回填")
	}
}

func TestToolApprovalRejected(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	ctx := context.Background()
	seedAppTool(t, st, "danger", "deleteFiles", true)

	hub := NewApprovalHub()
	tools := tool.NewService(tool.Options{Store: st, Logger: testLogger(), Approver: hub.Approve})
	if err := tools.SyncApps(ctx); err != nil {
		t.Fatalf("sync apps: %v", err)
	}

	var mu sync.Mutex
	var requests []llm.ChatRequest
	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 4096,
		Reply: func(call int, req llm.ChatRequest) llm.FakeReply {
			mu.Lock()
			requests = append(requests, req)
			mu.Unlock()
			if call == 0 {
				return llm.FakeReply{ToolCalls: []llm.ToolCall{{
					ID:   "call-danger",
					Name: "app:danger/deleteFiles",
				}}}
			}
			return llm.FakeReply{Content: "已取消删除操作"}
		},
	})

	svc := NewAssistantService(st, testLogger(), provider, tools, hub)
	session := svc.CreateSession()
	messageID, events, err := svc.Send(session.ID, "删除文件", "bk-pro", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	sawApproval := false
	got := collectEvents(t, events, func(ev contract.ChatEvent) {
		if ev.Type != contract.ChatEventApproval {
			return
		}
		sawApproval = true
		if ev.RunID == "" || ev.ToolCallID != "call-danger" || !ev.Dangerous {
			t.Errorf("审批事件字段异常：%+v", ev)
		}
		// 模拟用户点击「拒绝」。
		svc.ResolveApproval(ev.RunID, ev.ToolCallID, false)
	})
	if !sawApproval {
		t.Fatal("未收到审批事件")
	}
	if last := got[len(got)-1]; last.Status != contract.MessageDone || last.Content != "已取消删除操作" {
		t.Fatalf("最终回复异常：%+v", last)
	}

	calls, err := st.ToolCalls.ListByMessage(ctx, messageID)
	if err != nil {
		t.Fatalf("list tool calls: %v", err)
	}
	if len(calls) != 1 || calls[0].Status != domain.ToolCallRejected {
		t.Fatalf("拒绝后工具调用状态 = %+v", calls)
	}

	mu.Lock()
	reqs := append([]llm.ChatRequest(nil), requests...)
	mu.Unlock()
	if len(reqs) != 2 {
		t.Fatalf("模型调用次数 = %d, want 2", len(reqs))
	}
	foundRejected := false
	for _, message := range reqs[1].Messages {
		if message.Role == "tool" && message.ToolCallID == "call-danger" && strings.Contains(message.Content, "未确认") {
			foundRejected = true
		}
	}
	if !foundRejected {
		t.Fatal("模型没有收到审批拒绝结果")
	}
}

func TestToolLoopStopsAtMaxRounds(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	ctx := context.Background()
	seedAppTool(t, st, "demo", "runTask", false)

	tools := tool.NewService(tool.Options{Store: st, Logger: testLogger()})
	if err := tools.SyncApps(ctx); err != nil {
		t.Fatalf("sync apps: %v", err)
	}

	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 4096,
		Reply: func(call int, req llm.ChatRequest) llm.FakeReply {
			return llm.FakeReply{ToolCalls: []llm.ToolCall{{
				ID:   fmt.Sprintf("call-%d", call),
				Name: "app:demo/runTask",
			}}}
		},
	})

	svc := NewAssistantService(st, testLogger(), provider, tools, NewApprovalHub())
	session := svc.CreateSession()
	_, events, err := svc.Send(session.ID, "一直调用工具", "bk-pro", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	got := collectEvents(t, events, nil)
	last := got[len(got)-1]
	if last.Status != contract.MessageFailed || !strings.Contains(last.Error, "工具调用超过") {
		t.Fatalf("超限后应失败退出：%+v", last)
	}

	calls, err := st.ToolCalls.ListByConversation(ctx, session.ID)
	if err != nil {
		t.Fatalf("list tool calls: %v", err)
	}
	if len(calls) != maxToolRounds {
		t.Fatalf("工具调用次数 = %d, want %d", len(calls), maxToolRounds)
	}
}

// TestDangerousToolKeepsMessageID 验证危险工具审批后仍带着消息 ID 执行，
// 这样写入的文件才能登记到当前消息，出现在文件条与会话文件里。
func TestDangerousToolKeepsMessageID(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("创建工作区失败：%v", err)
	}
	st := openTestStore(t, filepath.Join(root, "test.db"), false)

	hub := NewApprovalHub()
	artifacts := NewArtifactService(st, root, filepath.Join(workspace, "artifacts"), testLogger())
	exec := NewToolExecutor(st, nil)
	exec.SetArtifacts(artifacts)
	exec.SetFS(NewFSService(workspace))
	tools := tool.NewService(tool.Options{Store: st, Logger: testLogger(), Approver: hub.Approve, Executor: exec})
	tools.RegisterBuiltin(tool.FSToolSpecs()...)

	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 4096,
		Reply: func(call int, req llm.ChatRequest) llm.FakeReply {
			if call == 0 {
				return llm.FakeReply{ToolCalls: []llm.ToolCall{{
					ID:        "call-write",
					Name:      tool.WriteFileToolID,
					Arguments: `{"path":"docs/报告.md","content":"# 报告"}`,
				}}}
			}
			return llm.FakeReply{Content: "已经写入报告"}
		},
	})

	svc := NewAssistantService(st, testLogger(), provider, tools, hub)
	svc.SetArtifacts(artifacts)
	session := svc.CreateSession()
	messageID, events, err := svc.Send(session.ID, "写一份报告", "bk-pro", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	sawApproval := false
	collectEvents(t, events, func(ev contract.ChatEvent) {
		if ev.Type != contract.ChatEventApproval {
			return
		}
		sawApproval = true
		svc.ResolveApproval(ev.RunID, ev.ToolCallID, true)
	})
	if !sawApproval {
		t.Fatal("未收到危险工具审批事件")
	}

	items, err := st.Artifacts.ListByMessageIDs(context.Background(), []string{messageID})
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("消息关联文件 = %+v, want 1", items)
	}
	if items[0].SourcePath != filepath.Join(workspace, "docs", "报告.md") {
		t.Fatalf("文件来源路径 = %q", items[0].SourcePath)
	}

	messages := svc.Messages(session.ID)
	if len(messages) == 0 || len(messages[len(messages)-1].Artifacts) != 1 {
		t.Fatalf("消息没有带上文件条：%+v", messages)
	}
}
