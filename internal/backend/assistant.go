package backend

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"bowknot-workbench/internal/contract"
)

// mockScript 是一条演示回复的剧本：文本与可选的失败点。
type mockScript struct {
	text         string
	errorAt      int
	errorMessage string
}

// assistantService 是助手对话的本地演示实现，用定时器模拟流式回复。
// 后续替换为真实模型服务时，UI 只依赖 contract.AssistantService。
type assistantService struct {
	mu         sync.Mutex
	sessions   []contract.ChatSession
	messages   map[string][]contract.ChatMessage
	streams    map[string]chan struct{}
	streamMsgs map[string]string
	scripts    map[string]mockScript
	autoPlayed map[string]bool
	seq        int
	persona    contract.AssistantPersona
}

// NewAssistantService 创建内存版助手服务，初始数据对齐 Web 端演示会话。
func NewAssistantService() contract.AssistantService {
	now := time.Now().UnixMilli()
	const minute = int64(60 * 1000)
	const hour = 60 * minute
	const day = 24 * hour

	s := &assistantService{
		messages:   map[string][]contract.ChatMessage{},
		streams:    map[string]chan struct{}{},
		streamMsgs: map[string]string{},
		autoPlayed: map[string]bool{},
	}

	s.sessions = []contract.ChatSession{
		{ID: "daily", Title: "日常", Summary: "下周计划已经整理好，按天推进即可。", UpdatedAt: now - 2*minute},
		{ID: "work", Title: "工作讨论", Summary: "周报要点分三块：本周进展、风险与下周计划。", UpdatedAt: now - 36*minute},
		{ID: "travel", Title: "旅行计划", Summary: "杭州三天两晚，按西湖、灵隐、龙井的顺序排。", UpdatedAt: now - 5*hour},
		{ID: "reading", Title: "读书笔记", Summary: "把读书笔记的摘录按章节归档。", UpdatedAt: now - day - 3*hour},
		{ID: "fitness", Title: "健身打卡", Summary: "本周完成 3 次力量训练，下一次建议练背。", UpdatedAt: now - 3*day},
	}

	// daily 只有用户提问，进入页面后自动播放一次流式回复，与 Web 端一致。
	s.messages["daily"] = []contract.ChatMessage{
		{
			ID:             "daily-u1",
			ConversationID: "daily",
			Role:           contract.RoleUser,
			Content:        "帮我整理一份下周的工作计划，越详细越好。",
			Status:         contract.MessageDone,
			CreatedAt:      now - 3*minute,
		},
	}

	s.messages["work"] = []contract.ChatMessage{
		{
			ID:             "work-u1",
			ConversationID: "work",
			Role:           contract.RoleUser,
			Content:        "帮我把这周的进展整理成周报要点。",
			Status:         contract.MessageDone,
			CreatedAt:      now - 40*minute,
		},
		{
			ID:             "work-a1",
			ConversationID: "work",
			Role:           contract.RoleAssistant,
			Content:        workReply,
			Status:         contract.MessageDone,
			CreatedAt:      now - 39*minute,
			MemoryWritten:  true,
			Emotion:        "happy",
		},
	}

	s.messages["travel"] = []contract.ChatMessage{
		{
			ID:             "travel-u1",
			ConversationID: "travel",
			Role:           contract.RoleUser,
			Content:        "杭州三天怎么玩？帮我排个不赶的行程。",
			Status:         contract.MessageDone,
			CreatedAt:      now - 5*hour - 20*minute,
		},
		{
			ID:             "travel-a1",
			ConversationID: "travel",
			Role:           contract.RoleAssistant,
			Content:        travelStopped,
			Status:         contract.MessageStopped,
			CreatedAt:      now - 5*hour - 19*minute,
		},
	}

	s.messages["reading"] = []contract.ChatMessage{
		{
			ID:             "reading-u1",
			ConversationID: "reading",
			Role:           contract.RoleUser,
			Content:        "把上次的读书笔记按章节重新整理一下。",
			Status:         contract.MessageDone,
			CreatedAt:      now - day - 4*hour,
		},
		{
			ID:             "reading-a1",
			ConversationID: "reading",
			Role:           contract.RoleAssistant,
			Content:        readingError,
			Status:         contract.MessageFailed,
			CreatedAt:      now - day - 4*hour + minute,
			Error:          "模型服务暂时不可用，请稍后重试",
		},
	}

	s.messages["fitness"] = []contract.ChatMessage{
		{
			ID:             "fitness-u1",
			ConversationID: "fitness",
			Role:           contract.RoleUser,
			Content:        "本周的健身记录帮我总结一下。",
			Status:         contract.MessageDone,
			CreatedAt:      now - 3*day,
		},
	}

	// 历史会话的演示回复，与 Web 端 mockReplies 对齐；没有配置的会话不自动播放。
	s.scripts = map[string]mockScript{
		"daily":   {text: dailyReply},
		"work":    {text: workReply},
		"travel":  {text: travelReply},
		"reading": {text: readingReply, errorAt: 60, errorMessage: "网络连接中断，请稍后重试"},
	}

	return s
}

func (s *assistantService) Sessions() []contract.ChatSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]contract.ChatSession(nil), s.sessions...)
}

func (s *assistantService) Messages(sessionID string) []contract.ChatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]contract.ChatMessage(nil), s.messages[sessionID]...)
}

// MessagesPage 是内存实现，不做分页，一次性返回全部消息。
func (s *assistantService) MessagesPage(sessionID string, beforeSeq int64, limit int) contract.MessagePage {
	return contract.MessagePage{Items: s.Messages(sessionID)}
}

// Revision 是内存实现，后台不会追加消息，固定返回 0。
func (s *assistantService) Revision() uint64 {
	return 0
}

func (s *assistantService) Persona() contract.AssistantPersona {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persona.Name == "" {
		return contract.DefaultAssistantPersona()
	}
	return s.persona
}

func (s *assistantService) SetPersona(persona contract.AssistantPersona) error {
	persona.Name = strings.TrimSpace(persona.Name)
	if persona.Name == "" {
		return fmt.Errorf("人设姓名不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persona = persona
	return nil
}

// GreetOnLaunch 由持久化实现负责，内存实现不主动问候。
func (s *assistantService) GreetOnLaunch() (string, string, <-chan contract.ChatEvent, bool) {
	return "", "", nil, false
}

// Greeting 是内存实现，固定返回首次使用时的自我介绍。
func (s *assistantService) Greeting() string {
	persona := s.Persona()
	name := persona.Name
	if name == "" {
		name = "Bowknot"
	}
	role := persona.Role
	if role == "" {
		role = "你的小助手"
	}
	return fmt.Sprintf("你好呀，我是%s，%s。我该怎么称呼你呢？", name, role)
}

func (s *assistantService) Models() []contract.ChatModel {
	return []contract.ChatModel{
		{ID: "bk-flash", Name: "Bowknot Flash", Description: "日常对话，响应最快"},
		{ID: "bk-pro", Name: "Bowknot Pro", Description: "复杂任务与长文本"},
		{ID: "bk-reason", Name: "Bowknot Reason", Description: "推理与代码分析"},
	}
}

func (s *assistantService) CreateSession() contract.ChatSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	session := contract.ChatSession{
		ID:        fmt.Sprintf("session-%d", s.seq),
		Title:     "新会话",
		Summary:   "还没有消息",
		UpdatedAt: time.Now().UnixMilli(),
	}
	s.sessions = append([]contract.ChatSession{session}, s.sessions...)
	return session
}

func (s *assistantService) RenameSession(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("会话标题不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].Title = title
			return nil
		}
	}
	return fmt.Errorf("会话不存在：%s", id)
}

func (s *assistantService) DeleteSession(id string) error {
	s.mu.Lock()
	if len(s.sessions) <= 1 {
		s.mu.Unlock()
		return errors.New("至少保留一个会话")
	}
	s.mu.Unlock()

	s.Stop(id)

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sessions {
		if s.sessions[i].ID != id {
			continue
		}
		s.sessions = append(s.sessions[:i], s.sessions[i+1:]...)
		delete(s.messages, id)
		delete(s.scripts, id)
		delete(s.autoPlayed, id)
		return nil
	}
	return fmt.Errorf("会话不存在：%s", id)
}

func (s *assistantService) PinSession(id string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].Pinned = pinned
			return nil
		}
	}
	return fmt.Errorf("会话不存在：%s", id)
}

func (s *assistantService) Send(sessionID, text, modelID string, attachments []contract.AttachmentInput) (string, <-chan contract.ChatEvent, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(attachments) == 0 {
		return "", nil, errors.New("请输入消息内容")
	}

	s.mu.Lock()
	index := s.sessionIndex(sessionID)
	if index < 0 {
		s.mu.Unlock()
		return "", nil, fmt.Errorf("会话不存在：%s", sessionID)
	}
	if s.streams[sessionID] != nil {
		s.mu.Unlock()
		return "", nil, errors.New("当前会话正在生成回复，请先停止")
	}

	now := time.Now().UnixMilli()
	s.seq++
	userID := fmt.Sprintf("msg-%d-u", s.seq)

	session := &s.sessions[index]
	if session.Title == "新会话" || len(s.messages[sessionID]) == 0 {
		session.Title = snippet(text, 20)
	}
	session.Summary = snippet(text, 60)
	session.UpdatedAt = now

	s.messages[sessionID] = append(s.messages[sessionID], contract.ChatMessage{
		ID:             userID,
		ConversationID: sessionID,
		Role:           contract.RoleUser,
		Content:        text,
		Status:         contract.MessageDone,
		CreatedAt:      now,
	})

	script := mockScript{text: mockReply(text, s.modelName(modelID))}
	messageID, events, cancel := s.beginReplyLocked(sessionID)
	s.mu.Unlock()

	go s.stream(sessionID, messageID, script, events, cancel)
	return messageID, events, nil
}

func (s *assistantService) AutoReply(sessionID string) (string, <-chan contract.ChatEvent, bool) {
	s.mu.Lock()
	if s.sessionIndex(sessionID) < 0 || s.streams[sessionID] != nil || s.autoPlayed[sessionID] {
		s.mu.Unlock()
		return "", nil, false
	}
	script, ok := s.scripts[sessionID]
	if !ok {
		s.mu.Unlock()
		return "", nil, false
	}
	for _, message := range s.messages[sessionID] {
		if message.Role == contract.RoleAssistant {
			s.autoPlayed[sessionID] = true
			s.mu.Unlock()
			return "", nil, false
		}
	}

	s.autoPlayed[sessionID] = true
	messageID, events, cancel := s.beginReplyLocked(sessionID)
	s.mu.Unlock()

	go s.stream(sessionID, messageID, script, events, cancel)
	return messageID, events, true
}

func (s *assistantService) Regenerate(sessionID, messageID string) (<-chan contract.ChatEvent, error) {
	s.mu.Lock()
	if s.sessionIndex(sessionID) < 0 {
		s.mu.Unlock()
		return nil, fmt.Errorf("会话不存在：%s", sessionID)
	}
	if s.streams[sessionID] != nil {
		s.mu.Unlock()
		return nil, errors.New("当前会话正在生成回复，请先停止")
	}

	var target *contract.ChatMessage
	for i := range s.messages[sessionID] {
		if s.messages[sessionID][i].ID == messageID {
			target = &s.messages[sessionID][i]
			break
		}
	}
	if target == nil {
		s.mu.Unlock()
		return nil, errors.New("消息不存在")
	}
	if target.Role != contract.RoleAssistant {
		s.mu.Unlock()
		return nil, errors.New("只能重新生成助手消息")
	}

	script, ok := s.scripts[sessionID]
	if !ok {
		script = mockScript{text: mockReply(s.lastUserTextLocked(sessionID), s.modelName(""))}
	}

	target.Content = ""
	target.Status = contract.MessageStreaming
	target.Error = ""

	cancel := make(chan struct{})
	s.streams[sessionID] = cancel
	s.streamMsgs[sessionID] = messageID
	events := make(chan contract.ChatEvent, 128)
	s.mu.Unlock()

	go s.stream(sessionID, messageID, script, events, cancel)
	return events, nil
}

func (s *assistantService) DeleteMessage(sessionID, messageID string) error {
	s.mu.Lock()
	streaming := s.streamMsgs[sessionID] == messageID
	s.mu.Unlock()

	if streaming {
		s.Stop(sessionID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.messages[sessionID]
	for i := range list {
		if list[i].ID != messageID {
			continue
		}
		s.messages[sessionID] = append(list[:i], list[i+1:]...)
		return nil
	}
	return errors.New("消息不存在")
}

func (s *assistantService) Stop(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.streams[sessionID]; ok {
		close(cancel)
		delete(s.streams, sessionID)
		delete(s.streamMsgs, sessionID)
	}
}

// beginReplyLocked 追加一条空的助手消息并注册流，调用方负责解锁后启动 goroutine。
func (s *assistantService) beginReplyLocked(sessionID string) (string, chan contract.ChatEvent, chan struct{}) {
	s.seq++
	messageID := fmt.Sprintf("msg-%d-a", s.seq)
	now := time.Now().UnixMilli()
	s.messages[sessionID] = append(s.messages[sessionID], contract.ChatMessage{
		ID:             messageID,
		ConversationID: sessionID,
		Role:           contract.RoleAssistant,
		Status:         contract.MessageStreaming,
		CreatedAt:      now,
	})

	cancel := make(chan struct{})
	s.streams[sessionID] = cancel
	s.streamMsgs[sessionID] = messageID
	return messageID, make(chan contract.ChatEvent, 128), cancel
}

// stream 每隔一小段时间推进几个字符，模拟模型的流式输出。
// 每个事件都带完整内容，避免 UI 因事件乱序重复拼接。
func (s *assistantService) stream(sessionID, messageID string, script mockScript, events chan<- contract.ChatEvent, cancel <-chan struct{}) {
	defer func() {
		s.mu.Lock()
		if s.streams[sessionID] == cancel {
			delete(s.streams, sessionID)
			delete(s.streamMsgs, sessionID)
		}
		s.mu.Unlock()
		close(events)
	}()

	runes := []rune(script.text)
	content := make([]rune, 0, len(runes))

	finish := func(status contract.MessageStatus, errText string) {
		s.finishMessage(sessionID, messageID, string(content), status, errText)
		events <- contract.ChatEvent{
			MessageID: messageID,
			Content:   string(content),
			Status:    status,
			Error:     errText,
			Done:      true,
		}
	}

	for i := 0; i < len(runes); i += 3 {
		select {
		case <-cancel:
			finish(contract.MessageStopped, "")
			return
		case <-time.After(30 * time.Millisecond):
		}

		end := min(i+3, len(runes))
		content = append(content, runes[i:end]...)

		if script.errorAt > 0 && len(content) >= script.errorAt {
			s.setMessageContent(sessionID, messageID, string(content))
			finish(contract.MessageFailed, script.errorMessage)
			return
		}

		s.setMessageContent(sessionID, messageID, string(content))
		select {
		case events <- contract.ChatEvent{
			MessageID: messageID,
			Content:   string(content),
			Status:    contract.MessageStreaming,
		}:
		case <-cancel:
			finish(contract.MessageStopped, "")
			return
		}
	}

	finish(contract.MessageDone, "")
}

func (s *assistantService) finishMessage(sessionID, messageID, content string, status contract.MessageStatus, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.messages[sessionID]
	for i := range list {
		if list[i].ID != messageID {
			continue
		}
		list[i].Content = content
		list[i].Status = status
		list[i].Error = errText
		return
	}
}

func (s *assistantService) setMessageContent(sessionID, messageID, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.messages[sessionID]
	for i := range list {
		if list[i].ID == messageID {
			list[i].Content = content
			return
		}
	}
}

// lastUserTextLocked 返回会话里最后一条用户消息，调用方需持有锁。
func (s *assistantService) lastUserTextLocked(sessionID string) string {
	list := s.messages[sessionID]
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Role == contract.RoleUser {
			return list[i].Content
		}
	}
	return ""
}

func (s *assistantService) sessionIndex(id string) int {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *assistantService) modelName(id string) string {
	for _, model := range s.Models() {
		if model.ID == id {
			return model.Name
		}
	}
	return "默认模型"
}

// mockReply 按输入内容稳定挑选模板，保证同一句话得到同一条回复。
func mockReply(text, modelName string) string {
	snippetText := snippet(text, 24)
	hash := 0
	for _, r := range text {
		hash += int(r)
	}

	templates := []string{
		fmt.Sprintf("已收到：「%s」。\n\n当前使用 %s，处于本地演示模式，真实模型接入后会在这里返回完整回答。\n\n你可以先验证发送、停止与会话切换的交互。", snippetText, modelName),
		fmt.Sprintf("关于「%s」，我的理解是这样：\n\n1. 先确认目标和约束\n2. 拆出最小可执行的一步\n3. 完成后再回头复盘\n\n（%s · 演示回复，未接入真实模型）", snippetText, modelName),
		fmt.Sprintf("收到「%s」。\n\n这是一条演示回复，用于验证消息上屏、流式输出和停止操作。切换模型后，回复里的模型名会同步变化（当前：%s）。", snippetText, modelName),
	}
	return templates[hash%len(templates)]
}

func snippet(text string, limit int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}

const dailyReply = `## 下周工作计划

先给结论：下周的重点是**把工作台的消息渲染链路打通**，同时保证会话管理不出现回归。下面的安排按目标、节奏、实现、风险四部分展开。

### 一、目标拆解

1. **消息模型统一**：文本、代码、工具调用都作为独立 part 存在，渲染层不再判断内容属于谁。
2. **流式渲染稳定**：生成过程中只更新最后一个未完成的块，已完成块直接复用缓存结果。
3. **交互闭环**：停止、重试、复制、重答、删除都能在消息区完成。
4. **可观测性**：开发环境打印单帧渲染耗时，超过阈值时给出提示。

这四个目标里，前两个是地基，后两个是体验。地基不稳时先不要急着做动画。

### 二、每日节奏

| 日期 | 上午 | 下午 |
| --- | --- | --- |
| 周一 | 数据模型与 mock 数据 | Markdown 切块与缓存 |
| 周二 | 流式驱动器 | 停止与错误分支 |
| 周三 | 消息容器与操作栏 | 自动滚动与回到底部 |
| 周四 | 代码块与非文本 part | 明暗主题适配 |
| 周五 | 联调与回归 | 文档与验收记录 |

表格里的安排留了缓冲。遇到阻塞时优先保证主链路，样式细节可以顺延。

### 三、关键实现

先把切块逻辑收敛成纯函数，输入是完整文本，输出是带完成标记的块数组：

~~~ts
type Block = { id: string; content: string; done: boolean }

export function splitBlocks(text: string): Block[] {
  return text.split("\n\n").map((content, index) => ({
    id: "block-" + index,
    content,
    done: true
  }))
}
~~~

渲染层只对未完成的块做节流，已完成块直接复用缓存的 HTML 字符串。

### 四、风险与应对

- **语法未闭合**：围栏代码在流式中可能只出现一半，渲染前补齐结尾。
- **表格抖动**：未完成的表格先按普通文本展示，等分隔行出现后再切换成表格。
- **滚动争抢**：生成时如果用户主动上滚，暂停吸底；用户回到底部后再恢复跟随。
- **性能回退**：为每个已完成块建立缓存，长文本只重算最后一块。

> 计划是给人用的，不是用来证明自己排得很满。如果某一天状态不好，把当天最重要的一件事做完就够了。
`

const workReply = `本周进展可以拆成三块：

1. **消息渲染**：完成块级切分与缓存，流式过程中只重算最后一个未完成的块。
2. **交互闭环**：补齐停止、重试、复制与删除，失败消息不再需要刷新页面。
3. **待观察项**：长文本下的滚动跟随还有轻微抖动，下周优先处理。

下周建议先做代码高亮的异步加载，再补工具调用卡片的样式。`

const travelStopped = `杭州三天两晚可以这样安排：

第一天中午到酒店放行李，下午沿西湖走北线，从断桥到孤山，傍晚在湖滨看日落。

第二天上午去灵隐寺，下午转龙井村，找一家茶室坐一会儿。

第三天留给河坊街和南宋御街，下午返程。`

const travelReply = travelStopped + `

补充几点：

- 西湖边周末人多，北线尽量放在工作日或清晨。
- 灵隐寺需要提前预约，飞来峰门票单独购买。
- 龙井村到九溪有一段徒步路线，穿轻便的鞋。
- 返程当天留出至少两小时到高铁站，避免晚高峰。`

const readingError = `我先按章节把摘录重新排了一遍，第一部分已经整理好，后面两部分还需要确认几个页码。`

const readingReply = readingError + ` 重新整理后的版本如下：第一章讲旅行与维修的关系，第二章开始讨论“良质”的定义。`
