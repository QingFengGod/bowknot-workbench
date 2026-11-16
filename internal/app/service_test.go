package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func openTestStore(t *testing.T, path string, seed bool) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{
		Path:   path,
		Logger: testLogger(),
		Seed:   seed,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close(context.Background())
	})
	return st
}

// fastFake 以极小间隔一次性输出全部内容，缩短测试耗时。
func fastFake() *llm.Fake {
	return llm.NewFake(llm.FakeOptions{Interval: time.Millisecond, ChunkSize: 1024})
}

// slowFake 逐字慢速输出，用于测试停止与并发行为。
func slowFake() *llm.Fake {
	return llm.NewFake(llm.FakeOptions{Interval: 5 * time.Millisecond, ChunkSize: 1})
}

func newFastAssistant(st *store.Store) *AssistantService {
	return NewAssistantService(st, testLogger(), fastFake())
}

func newSlowAssistant(st *store.Store) *AssistantService {
	return NewAssistantService(st, testLogger(), slowFake())
}

func newFastAgent(st *store.Store) *AgentService {
	return NewAgentService(st, testLogger(), fastFake())
}

func drainEvents(t *testing.T, events <-chan contract.ChatEvent) []contract.ChatEvent {
	t.Helper()
	out := make([]contract.ChatEvent, 0)
	done := make(chan struct{})
	go func() {
		for event := range events {
			out = append(out, event)
		}
		close(done)
	}()

	select {
	case <-done:
		return out
	case <-time.After(3 * time.Second):
		t.Fatal("等待流式事件超时")
		return nil
	}
}

func TestAssistantPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st := openTestStore(t, path, false)
	svc := newFastAssistant(st)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	session := svc.CreateSession()
	if session.ID == "" {
		t.Fatal("创建会话失败")
	}

	messageID, events, err := svc.Send(session.ID, "你好", "bk-pro")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if messageID == "" {
		t.Fatal("消息 ID 为空")
	}
	drainEvents(t, events)

	messages := svc.Messages(session.ID)
	if len(messages) != 2 {
		t.Fatalf("消息数量 = %d, want 2", len(messages))
	}
	if messages[0].Role != contract.RoleUser || messages[0].Content != "你好" {
		t.Fatalf("用户消息不匹配: %+v", messages[0])
	}
	if messages[1].Role != contract.RoleAssistant || messages[1].Status != contract.MessageDone {
		t.Fatalf("助手消息不匹配: %+v", messages[1])
	}
	if messages[1].Content == "" {
		t.Fatal("助手消息内容为空")
	}

	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := st.Close(context.Background()); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened := openTestStore(t, path, false)
	restored := newFastAssistant(reopened)
	t.Cleanup(func() { _ = restored.Shutdown(context.Background()) })

	sessions := restored.Sessions()
	if len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("重启后会话不匹配: %+v", sessions)
	}
	if sessions[0].Title != "你好" {
		t.Fatalf("会话标题 = %q, want 你好", sessions[0].Title)
	}
	messages = restored.Messages(session.ID)
	if len(messages) != 2 || messages[1].Status != contract.MessageDone {
		t.Fatalf("重启后消息不匹配: %+v", messages)
	}
}

func TestAssistantStopPersistsStoppedMessage(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newSlowAssistant(st)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	session := svc.CreateSession()
	_, events, err := svc.Send(session.ID, "请生成一段比较长的回复用于测试停止行为", "bk-pro")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("流在停止前就结束了")
		}
		if event.Status != contract.MessageStreaming {
			t.Fatalf("首个事件状态 = %s, want streaming", event.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待首个流式事件超时")
	}

	svc.Stop(session.ID)
	drainEvents(t, events)

	messages := svc.Messages(session.ID)
	if len(messages) != 2 {
		t.Fatalf("消息数量 = %d, want 2", len(messages))
	}
	if messages[1].Status != contract.MessageStopped {
		t.Fatalf("停止后状态 = %s, want stopped", messages[1].Status)
	}
}

func TestAssistantRejectsConcurrentSend(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newSlowAssistant(st)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	session := svc.CreateSession()
	_, events, err := svc.Send(session.ID, "第一条消息", "bk-pro")
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if _, _, err := svc.Send(session.ID, "第二条消息", "bk-pro"); err == nil {
		t.Fatal("并发发送未被拒绝")
	}

	svc.Stop(session.ID)
	drainEvents(t, events)
}

func TestAssistantDeleteStreamingMessage(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newSlowAssistant(st)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	session := svc.CreateSession()
	messageID, events, err := svc.Send(session.ID, "删除正在生成的消息", "bk-pro")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if err := svc.DeleteMessage(session.ID, messageID); err != nil {
		t.Fatalf("delete streaming message: %v", err)
	}
	drainEvents(t, events)

	messages := svc.Messages(session.ID)
	if len(messages) != 1 || messages[0].Role != contract.RoleUser {
		t.Fatalf("删除后消息不匹配: %+v", messages)
	}
}

func TestAssistantAutoReplyOnlyOnce(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), true)
	svc := newFastAssistant(st)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	_, events, started := svc.AutoReply("daily")
	if !started {
		t.Fatal("daily 首次自动回复未启动")
	}
	drainEvents(t, events)

	if _, _, started := svc.AutoReply("daily"); started {
		t.Fatal("daily 第二次仍启动了自动回复")
	}
	if messages := svc.Messages("daily"); len(messages) != 2 {
		t.Fatalf("daily 消息数量 = %d, want 2", len(messages))
	}
}

func TestAgentAndGroupPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st := openTestStore(t, path, false)
	svc := newFastAgent(st)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	agent := svc.CreateAgent("测试助手", "验证持久化", "你是测试助手")
	if agent.ID == "" {
		t.Fatal("创建 Agent 失败")
	}
	group := svc.CreateGroup("测试群", []string{agent.ID})
	if group.ID == "" {
		t.Fatal("创建群聊失败")
	}

	session := svc.CreateSession(agent.ID)
	_, events, err := svc.Send(session.ID, "单聊消息", "bk-pro")
	if err != nil {
		t.Fatalf("send agent: %v", err)
	}
	drainEvents(t, events)

	groupConversationID := groupKey(group.ID)
	_, events, err = svc.Send(groupConversationID, "群聊消息", "bk-pro")
	if err != nil {
		t.Fatalf("send group: %v", err)
	}
	drainEvents(t, events)

	messages := svc.Messages(groupConversationID)
	if len(messages) != 2 {
		t.Fatalf("群聊消息数量 = %d, want 2", len(messages))
	}
	for _, message := range messages {
		if message.ConversationID != groupConversationID {
			t.Fatalf("群聊消息会话 ID = %q, want %q", message.ConversationID, groupConversationID)
		}
	}
	if messages[1].SenderName != "助理" {
		t.Fatalf("群聊发言人 = %q, want 助理", messages[1].SenderName)
	}

	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := st.Close(context.Background()); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened := openTestStore(t, path, false)
	restored := newFastAgent(reopened)
	t.Cleanup(func() { _ = restored.Shutdown(context.Background()) })

	agents := restored.Agents()
	if len(agents) != 1 || agents[0].ID != agent.ID {
		t.Fatalf("重启后 Agent 不匹配: %+v", agents)
	}
	groups := restored.Groups()
	if len(groups) != 1 || groups[0].ID != group.ID {
		t.Fatalf("重启后群聊不匹配: %+v", groups)
	}
	sessions := restored.Sessions(agent.ID)
	if len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("重启后 Agent 会话不匹配: %+v", sessions)
	}
	if messages := restored.Messages(session.ID); len(messages) != 2 {
		t.Fatalf("重启后单聊消息数量 = %d, want 2", len(messages))
	}
	if messages := restored.Messages(groupConversationID); len(messages) != 2 {
		t.Fatalf("重启后群聊消息数量 = %d, want 2", len(messages))
	}
}

func TestRemoveAgentDeletesSessionsAndMembership(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newFastAgent(st)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	agent := svc.CreateAgent("待删除", "", "")
	group := svc.CreateGroup("测试群", []string{agent.ID})
	session := svc.CreateSession(agent.ID)

	if err := svc.RemoveAgent(agent.ID); err != nil {
		t.Fatalf("remove agent: %v", err)
	}
	if sessions := svc.Sessions(agent.ID); len(sessions) != 0 {
		t.Fatalf("删除后仍存在会话: %+v", sessions)
	}
	groups := svc.Groups()
	if len(groups) != 1 {
		t.Fatalf("群聊数量 = %d, want 1", len(groups))
	}
	if len(groups[0].MemberIDs) != 0 {
		t.Fatalf("群聊成员未清理: %+v", groups[0].MemberIDs)
	}

	if err := svc.DeleteSession(session.ID); err == nil {
		t.Fatal("预期会话已被级联删除")
	}
	_ = group
}
