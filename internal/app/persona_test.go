package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/infra/tool"
)

func TestAssistantPersona(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newFastAssistant(st)

	def := svc.Persona()
	if def.Name != "Bowknot" || def.Role != "你的小助手" {
		t.Fatalf("默认人设 = %+v", def)
	}
	if prompt := svc.systemPrompt(); !strings.Contains(prompt, "Bowknot") {
		t.Fatalf("系统提示词未包含默认人设：%q", prompt)
	}

	persona := contract.AssistantPersona{
		Name:        "小甜",
		Gender:      "女",
		Age:         "18",
		Personality: "活泼、直接",
		Role:        "你的搭档",
		UserName:    "老板",
		Custom:      "回答先给结论",
	}
	if err := svc.SetPersona(persona); err != nil {
		t.Fatalf("SetPersona() error = %v", err)
	}
	got := svc.Persona()
	if got.Name != "小甜" || got.Gender != "女" || got.Age != "18" ||
		got.Personality != "活泼、直接" || got.Role != "你的搭档" ||
		got.UserName != "老板" || got.Custom != "回答先给结论" {
		t.Fatalf("人设 = %+v", got)
	}
	if prompt := svc.systemPrompt(); !strings.Contains(prompt, "小甜") || !strings.Contains(prompt, "老板") || !strings.Contains(prompt, "回答先给结论") {
		t.Fatalf("系统提示词未包含人设：%q", prompt)
	}

	if err := svc.SetPersona(contract.AssistantPersona{Name: "  "}); err == nil {
		t.Fatal("空姓名应当返回错误")
	}
	if got := svc.Persona(); got.Name != "小甜" {
		t.Fatalf("保存失败后不应覆盖已有姓名：%+v", got)
	}
}

// TestPersonaLegacyText 兼容旧版直接存纯文本人设的记录。
func TestPersonaLegacyText(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	ctx := context.Background()
	if err := st.Meta.Set(ctx, assistantPersonaKey, "你是我的技术搭档，回答简洁"); err != nil {
		t.Fatalf("写入旧人设失败: %v", err)
	}

	persona, err := loadPersona(ctx, st)
	if err != nil {
		t.Fatalf("loadPersona() error = %v", err)
	}
	if persona.Name != "Bowknot" || persona.Custom != "你是我的技术搭档，回答简洁" {
		t.Fatalf("旧人设兼容结果 = %+v", persona)
	}
}

func TestAssistantGreeting(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newFastAssistant(st)

	first := svc.Greeting()
	if !strings.Contains(first, "你好呀") || !strings.Contains(first, "Bowknot") || !strings.Contains(first, "怎么称呼你") {
		t.Fatalf("首次问候 = %q", first)
	}

	// 配置了对用户的称呼后，问候直接使用该称呼。
	if err := svc.SetPersona(contract.AssistantPersona{Name: "Bowknot", Role: "你的小助手", UserName: "老板"}); err != nil {
		t.Fatalf("SetPersona() error = %v", err)
	}
	named := svc.Greeting()
	if !strings.Contains(named, "老板") || strings.Contains(named, "怎么称呼你") {
		t.Fatalf("带称呼的问候 = %q", named)
	}

	if session := svc.CreateSession(); session.ID == "" {
		t.Fatal("CreateSession() 返回空会话")
	}
	returning := svc.Greeting()
	if !strings.Contains(returning, "最近怎么样") || !strings.Contains(returning, "老板") {
		t.Fatalf("回访问候 = %q", returning)
	}
}

func TestGreetOnLaunch(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newFastAssistant(st)

	sessionID, messageID, events, started := svc.GreetOnLaunch()
	if !started {
		t.Fatal("首次进入应当启动问候")
	}
	if sessionID == "" || messageID == "" || events == nil {
		t.Fatalf("问候结果不完整：session=%q message=%q", sessionID, messageID)
	}
	drainEvents(t, events)

	messages := svc.Messages(sessionID)
	if len(messages) != 1 || messages[0].ID != messageID {
		t.Fatalf("问候消息 = %+v", messages)
	}
	if messages[0].Status != contract.MessageDone {
		t.Fatalf("问候状态 = %q, want done", messages[0].Status)
	}
	if !strings.Contains(messages[0].Content, "你好呀") || !strings.Contains(messages[0].Content, "Bowknot") {
		t.Fatalf("问候内容 = %q", messages[0].Content)
	}

	// 同一次启动内不再重复问候。
	if _, _, _, again := svc.GreetOnLaunch(); again {
		t.Fatal("第二次进入不应再次问候")
	}

	// 手动新建会话不会带默认问候。
	if session := svc.CreateSession(); session.ID == "" {
		t.Fatal("CreateSession() 返回空会话")
	} else if got := svc.Messages(session.ID); len(got) != 0 {
		t.Fatalf("新会话不应有问候消息：%+v", got)
	}
}

// TestGreetOnLaunchUsesModel 验证问候语优先由模型生成，失败时才回退到模板。
func TestGreetOnLaunchUsesModel(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	want := "用户，最近怎么样？"
	captured := ""
	provider := llm.NewFake(llm.FakeOptions{
		CompleteReply: func(_ int, req llm.ChatRequest) string {
			captured = req.System
			return want
		},
	})
	svc := NewAssistantService(st, testLogger(), provider, nil, nil)
	if err := svc.SetPersona(contract.AssistantPersona{Name: "Bowknot", Role: "你的小助手", UserName: "老板"}); err != nil {
		t.Fatalf("SetPersona() error = %v", err)
	}

	sessionID, _, events, started := svc.GreetOnLaunch()
	if !started {
		t.Fatal("首次进入应当启动问候")
	}
	drainEvents(t, events)

	messages := svc.Messages(sessionID)
	if len(messages) != 1 {
		t.Fatalf("模型问候 = %+v", messages)
	}
	// 模型若把记忆里的第三人称带出来，也要改写为对用户的称呼。
	if strings.Contains(messages[0].Content, "用户") || !strings.Contains(messages[0].Content, "老板") {
		t.Fatalf("问候称呼 = %q", messages[0].Content)
	}
	// 提示词必须带上当前时间，避免把刚发生的事说成「前几天」。
	if !strings.Contains(captured, "当前时间") || !strings.Contains(captured, "时间间隔") {
		t.Fatalf("问候提示词缺少时间信息：%q", captured)
	}
}

func TestToolExecutorUpdatesPersona(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newFastAssistant(st)
	if err := svc.SetPersona(contract.AssistantPersona{Name: "Bowknot", Role: "你的小助手"}); err != nil {
		t.Fatalf("SetPersona() error = %v", err)
	}

	executor := NewToolExecutor(st)
	result, err := executor.Execute(context.Background(), tool.Call{
		Tool:      tool.PersonaToolSpec(),
		Arguments: `{"name":"小助手","personality":"活泼","userName":"老板"}`,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Content, "已更新助手人设") {
		t.Fatalf("执行结果 = %q", result.Content)
	}

	got := svc.Persona()
	if got.Name != "小助手" || got.Personality != "活泼" || got.UserName != "老板" {
		t.Fatalf("工具更新后人设 = %+v", got)
	}
	if got.Role != "你的小助手" {
		t.Fatalf("未提交的字段不应被覆盖：%+v", got)
	}
}
