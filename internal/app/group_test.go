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
)

func newGroupStack(t *testing.T, provider llm.Provider) (*AgentService, *GroupService) {
	t.Helper()
	st := openTestStore(t, memoryTestStore(t), false)
	agents := NewAgentService(st, testLogger(), provider, nil, nil)
	agents.SetModels([]contract.ChatModel{{ID: "fake", Name: "Fake"}})
	groups := NewGroupService(st, testLogger(), agents)
	agents.SetGroups(groups)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = groups.Shutdown(ctx)
		_ = agents.Shutdown(ctx)
	})
	return agents, groups
}

func createMember(t *testing.T, agents *AgentService, name string) string {
	t.Helper()
	info := agents.CreateAgent(name, name+"的职责", "")
	if info.ID == "" {
		t.Fatalf("创建成员 %s 失败", name)
	}
	return info.ID
}

func waitGroupIdle(t *testing.T, groups *GroupService, conversationID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !groups.Busy(conversationID) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待群聊协作结束超时")
}

// 主助手 @ 两名成员，其中一名成员再 @ 第三名，最后群主汇总。
func TestGroupOrchestrationRoundsAndSummary(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	replies := []string{
		"我先请 @甲 和 @乙 一起看一下。",
		"甲的意见：方案可行。@丙",
		"乙的意见：需要补充预算。",
		"丙补充：预算可以用上季度数据。",
		"综合大家意见：方案可行，先补预算数据。",
	}
	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 1024,
		Reply: func(call int, _ llm.ChatRequest) llm.FakeReply {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if call < len(replies) {
				return llm.FakeReply{Content: replies[call]}
			}
			return llm.FakeReply{Content: "不应出现的额外调用"}
		},
	})

	agents, groups := newGroupStack(t, provider)
	first := createMember(t, agents, "甲")
	second := createMember(t, agents, "乙")
	third := createMember(t, agents, "丙")
	group := agents.CreateGroup("方案评审群", []string{first, second, third})
	key := "group:" + group.ID

	_, events, err := agents.Send(key, "帮我看下这个方案", "")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	for range events {
	}
	waitGroupIdle(t, groups, key)

	mu.Lock()
	totalCalls := calls
	mu.Unlock()
	if totalCalls != 5 {
		t.Fatalf("模型调用次数 = %d, want 5（群主 + 3 成员 + 汇总）", totalCalls)
	}

	messages := agents.Messages(key)
	mainCount, memberNames := 0, map[string]bool{}
	var summary string
	for _, message := range messages {
		if message.Role != contract.RoleAssistant {
			continue
		}
		switch message.SenderKind {
		case "main":
			mainCount++
			summary = message.Content
		case "agent":
			memberNames[message.SenderName] = true
		}
	}
	if mainCount != 2 {
		t.Fatalf("群主消息数量 = %d, want 2（首轮 + 汇总）", mainCount)
	}
	for _, name := range []string{"甲", "乙", "丙"} {
		if !memberNames[name] {
			t.Fatalf("缺少成员 %s 的发言：%+v", name, memberNames)
		}
	}
	if !strings.Contains(summary, "综合大家意见") {
		t.Fatalf("最后一条群主消息不是汇总：%s", summary)
	}
}

// 单条用户消息最多 3 轮成员发言，第四名成员不会被唤醒。
func TestGroupOrchestrationRoundLimit(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	replies := []string{
		"请 @甲 先看。",
		"甲的意见。@乙",
		"乙的意见。@丙",
		"丙的意见。@丁",
		"汇总完成。",
	}
	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 1024,
		Reply: func(call int, _ llm.ChatRequest) llm.FakeReply {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if call < len(replies) {
				return llm.FakeReply{Content: replies[call]}
			}
			return llm.FakeReply{Content: "不应出现的额外调用"}
		},
	})

	agents, groups := newGroupStack(t, provider)
	var memberIDs []string
	for _, name := range []string{"甲", "乙", "丙", "丁"} {
		memberIDs = append(memberIDs, createMember(t, agents, name))
	}
	group := agents.CreateGroup("轮次测试群", memberIDs)
	key := "group:" + group.ID

	_, events, err := agents.Send(key, "请依次讨论", "")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	for range events {
	}
	waitGroupIdle(t, groups, key)

	mu.Lock()
	totalCalls := calls
	mu.Unlock()
	if totalCalls != 5 {
		t.Fatalf("模型调用次数 = %d, want 5（群主 + 3 轮成员 + 汇总）", totalCalls)
	}
	for _, message := range agents.Messages(key) {
		if message.SenderName == "丁" && message.Role == contract.RoleAssistant {
			t.Fatal("第四名成员不应在 3 轮限制内被唤醒")
		}
	}
}

// 用户直接在群里 @ 成员时，群主完全不发言，由成员直接回答。
func TestGroupUserMentionSkipsMain(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	replies := []string{
		"我的看法是可行的。",
	}
	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 1024,
		Reply: func(call int, _ llm.ChatRequest) llm.FakeReply {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if call < len(replies) {
				return llm.FakeReply{Content: replies[call]}
			}
			return llm.FakeReply{Content: "不应出现的额外调用"}
		},
	})

	agents, groups := newGroupStack(t, provider)
	memberID := createMember(t, agents, "甲")
	group := agents.CreateGroup("用户点名群", []string{memberID})
	key := "group:" + group.ID

	messageID, events, err := agents.Send(key, "@甲 你怎么看", "")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if messageID != "" {
		t.Fatalf("群主不应生成消息，messageID = %q", messageID)
	}
	for range events {
	}
	waitGroupIdle(t, groups, key)

	mu.Lock()
	totalCalls := calls
	mu.Unlock()
	if totalCalls != 1 {
		t.Fatalf("模型调用次数 = %d, want 1（只有成员）", totalCalls)
	}

	mainCount := 0
	var memberSpoke, userFound bool
	for _, message := range agents.Messages(key) {
		if message.Role == contract.RoleUser && message.Content == "@甲 你怎么看" {
			userFound = true
			continue
		}
		if message.Role != contract.RoleAssistant {
			continue
		}
		switch message.SenderKind {
		case "main":
			mainCount++
		case "agent":
			if message.SenderName == "甲" {
				memberSpoke = true
			}
		}
	}
	if !userFound {
		t.Fatal("用户消息没有写入群聊")
	}
	if mainCount != 0 {
		t.Fatalf("群主消息数量 = %d, want 0", mainCount)
	}
	if !memberSpoke {
		t.Fatal("用户 @ 的成员没有发言")
	}
}

func TestStripSpeakerPrefix(t *testing.T) {
	cases := []struct {
		content string
		name    string
		want    string
	}{
		{"【李娜】【李娜】收到老板！", "李娜", "收到老板！"},
		{"【大肥鱼】标语都给爸爸铺开了", "大肥鱼", "标语都给爸爸铺开了"},
		{"李娜：收到", "李娜", "收到"},
		{"【重要】内容", "李娜", "【重要】内容"},
		{"【李娜】", "李娜", "【李娜】"},
		{"普通内容", "李娜", "普通内容"},
	}
	for _, tc := range cases {
		if got := stripSpeakerPrefix(tc.content, tc.name); got != tc.want {
			t.Fatalf("stripSpeakerPrefix(%q, %q) = %q, want %q", tc.content, tc.name, got, tc.want)
		}
	}
}

// 模型模仿群聊历史前缀时，落库与展示都要去掉。
func TestGroupStripsSpeakerPrefixFromReply(t *testing.T) {
	provider := llm.NewFake(llm.FakeOptions{
		Interval:  time.Millisecond,
		ChunkSize: 1024,
		Reply: func(int, llm.ChatRequest) llm.FakeReply {
			return llm.FakeReply{Content: "【甲】【甲】收到老板！"}
		},
	})
	agents, groups := newGroupStack(t, provider)
	memberID := createMember(t, agents, "甲")
	group := agents.CreateGroup("前缀群", []string{memberID})
	key := "group:" + group.ID

	_, events, err := agents.Send(key, "@甲 在吗", "")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	for range events {
	}
	waitGroupIdle(t, groups, key)

	var reply string
	for _, message := range agents.Messages(key) {
		if message.Role == contract.RoleAssistant && message.SenderName == "甲" {
			reply = message.Content
		}
	}
	if reply != "收到老板！" {
		t.Fatalf("成员回复 = %q, want 收到老板！", reply)
	}
}

func TestMatchMentions(t *testing.T) {
	members := []domain.Agent{
		{ID: "a", Name: "小明"},
		{ID: "b", Name: "小明助手"},
		{ID: "c", Name: "小红"},
	}
	got := matchMentions("@小明助手 和 ＠小明，还有 @小红", members, "")
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("matchMentions() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("matchMentions() = %v, want %v", got, want)
		}
	}

	if got := matchMentions("@小明助手", members, ""); len(got) != 1 || got[0] != "b" {
		t.Fatalf("长名称优先匹配失败：%v", got)
	}
	if got := matchMentions("@小明 请看", members, "a"); len(got) != 0 {
		t.Fatalf("排除自身失败：%v", got)
	}
}

// 群内派发的任务需要落 GroupID，汇报仍写回群聊。
func TestGroupTaskPersistsGroupID(t *testing.T) {
	st := openTestStore(t, memoryTestStore(t), false)
	provider := llm.NewFake(llm.FakeOptions{Interval: time.Millisecond, ChunkSize: 1024})
	agents := NewAgentService(st, testLogger(), provider, nil, nil)
	tasks := NewTaskService(st, testLogger(), provider, agents, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tasks.Shutdown(ctx)
		_ = agents.Shutdown(ctx)
	})

	ctx := context.Background()
	if err := st.Conversations.Create(ctx, domain.Conversation{
		ID: "group-g1", Kind: domain.KindGroup, OwnerID: "g1", Title: "测试群",
		CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("创建群会话失败：%v", err)
	}
	agentID := createMember(t, agents, "执行者")

	detail, err := tasks.Dispatch(ctx, TaskDispatchInput{
		AgentID:              agentID,
		Brief:                "整理群里的结论",
		OriginConversationID: "group-g1",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	item, err := st.Tasks.Get(ctx, detail.ID)
	if err != nil {
		t.Fatalf("读取任务失败：%v", err)
	}
	if item.GroupID != "g1" {
		t.Fatalf("GroupID = %q, want g1", item.GroupID)
	}
	if item.AssignerKind != domain.AssignerMain {
		t.Fatalf("AssignerKind = %q, want main", item.AssignerKind)
	}
	_ = tasks.CancelTask(detail.ID)
	tasks.Wait()
}
