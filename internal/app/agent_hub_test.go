package app

import (
	"context"
	"strings"
	"testing"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/infra/tool"
)

// 委派应立即返回，并在子 Agent 完成后向来源会话追加一条主助手消息。
func TestAgentHubReportsAsynchronously(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	polish := llm.NewFake(llm.FakeOptions{
		CompleteReply: func(int, llm.ChatRequest) string { return "文案已交付，效果不错。" },
	})
	assistant := NewAssistantService(st, testLogger(), polish, nil, nil)
	agents := newFastAgent(st)
	agents.SetModels([]contract.ChatModel{{ID: "fake", Name: "Fake"}})

	info := agents.CreateAgent("文案助手", "负责产品文案", "")
	if info.ID == "" {
		t.Fatal("创建 Agent 失败")
	}

	hub := NewAgentHub(agents, assistant)
	session := assistant.CreateSession()

	reply, err := hub.RunAgent(context.Background(), tool.RunAgentInput{
		AgentID:        info.ID,
		Task:           "写一句产品标语",
		ConversationID: session.ID,
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if !strings.Contains(reply, "已把任务交给") {
		t.Fatalf("委派回复 = %q", reply)
	}

	hub.wg.Wait()

	page := assistant.MessagesPage(session.ID, 0, 50)
	found := false
	for _, message := range page.Items {
		if message.Role == contract.RoleAssistant && strings.Contains(message.Content, "任务完成了") {
			if !strings.Contains(message.Content, "文案已交付") {
				t.Fatalf("汇报没有使用润色结果：%s", message.Content)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("来源会话没有汇报消息：%+v", page.Items)
	}
	if assistant.Revision() == 0 {
		t.Fatal("后台汇报后 revision 没有递增")
	}
}
