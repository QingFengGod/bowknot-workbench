package backend

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"bowknot-workbench/internal/contract"
)

// agentService 是 agents 页的本地演示实现：Agent / 群聊 / 会话均为内存 mock，
// 流式回复由定时器模拟，与助手服务的事件协议一致。
type agentService struct {
	mu sync.Mutex

	agents   []contract.AgentInfo
	groups   []contract.AgentGroup
	sessions []contract.AgentSession
	// messages 以会话 ID 或 "group:群 ID" 为键。
	messages map[string][]contract.ChatMessage
	streams  map[string]chan struct{}
	// replies 是演示剧本，键同 messages。
	replies map[string]string
	// lastUser 记录每个会话最后一条用户消息，供重答时重新生成回复。
	lastUser map[string]string
	seq      int
}

// NewAgentService 创建内存版 agents 服务，初始数据对齐 Web 端演示数据。
func NewAgentService() contract.AgentService {
	now := time.Now().UnixMilli()
	const minute = int64(60 * 1000)
	const hour = 60 * minute

	s := &agentService{
		messages: map[string][]contract.ChatMessage{},
		streams:  map[string]chan struct{}{},
		lastUser: map[string]string{},
	}

	s.agents = []contract.AgentInfo{
		{ID: "writing", Name: "写作助手", Description: "负责长文撰写与润色", AvatarText: "写", ModelName: "Bowknot Pro", Persona: "你是一位克制、讲究节奏的中文写作者，擅长把生硬的内容改得更像人写的。", Pinned: true},
		{ID: "research", Name: "研究助手", Description: "搜集资料与竞品调研", AvatarText: "研", ModelName: "Bowknot Pro", Persona: "你负责查找资料、核对事实，并给出带来源的结论。"},
		{ID: "coding", Name: "代码搭档", Description: "读代码、改 bug、写测试", AvatarText: "码", ModelName: "Bowknot Reason", Persona: "你是一位严谨的工程搭档，先读代码再给方案，改动保持最小。"},
		{ID: "schedule", Name: "日程管家", Description: "管理日程与提醒", AvatarText: "程", ModelName: "Bowknot Flash", Persona: "你负责整理日程、设置提醒，并在冲突时给出可选方案。"},
	}

	s.groups = []contract.AgentGroup{
		{ID: "weekly", Name: "周报小组", MemberIDs: []string{"writing", "research"}, Pinned: true},
		{ID: "product", Name: "产品讨论组", MemberIDs: []string{"research", "coding"}},
	}

	s.sessions = []contract.AgentSession{
		{ID: "writing-report", AgentID: "writing", Title: "周报润色", Summary: "帮我把这份周报改得更像人写的", UpdatedAt: now - 3*minute, Pinned: true},
		{ID: "writing-article", AgentID: "writing", Title: "公众号长文", Summary: "还没有消息", UpdatedAt: now - 2*hour},
		{ID: "research-competitor", AgentID: "research", Title: "竞品调研", Summary: "整理近三个月的版本更新", UpdatedAt: now - 40*minute},
		{ID: "coding-scroll", AgentID: "coding", Title: "MessageList 滚动问题", Summary: "切换会话后没有回到底部", UpdatedAt: now - hour},
		{ID: "schedule-week", AgentID: "schedule", Title: "本周日程", Summary: "周报评审与竞品对比截止时间", UpdatedAt: now - 5*hour},
	}

	s.messages["writing-report"] = []contract.ChatMessage{
		s.userMsg("writing-report-u1", "writing-report", "帮我把这份周报改得更像人写的", now-4*minute),
		s.assistantMsg("writing-report-a1", "writing-report", writingReply, now-3*minute, "", ""),
	}
	s.messages["research-competitor"] = []contract.ChatMessage{
		s.userMsg("research-u1", "research-competitor", "整理一下近三个月的竞品更新", now-41*minute),
		s.assistantMsg("research-a1", "research-competitor", researchReply, now-40*minute, "", ""),
	}
	s.messages["coding-scroll"] = []contract.ChatMessage{
		s.userMsg("coding-u1", "coding-scroll", "看看 MessageList 切换会话时为什么不回到底部", now-61*minute),
		s.assistantMsg("coding-a1", "coding-scroll", codingReply, now-60*minute, "", ""),
	}
	s.messages["schedule-week"] = []contract.ChatMessage{
		s.userMsg("schedule-u1", "schedule-week", "这周还有什么没确认的？", now-301*minute),
		s.assistantMsg("schedule-a1", "schedule-week", scheduleReply, now-300*minute, "", ""),
	}
	s.messages["group:weekly"] = []contract.ChatMessage{
		s.userMsg("weekly-u1", "group:weekly", "把这周的工作整理成周报", now-30*minute),
		s.assistantMsg("weekly-a1", "group:weekly", "收到，我先拉一下这周的进展和风险项。", now-29*minute, "研究助手", "研"),
		s.assistantMsg("weekly-a2", "group:weekly", "结构我来出：本周进展、风险与阻塞、下周计划，三块就够。", now-28*minute, "写作助手", "写"),
		s.assistantMsg("weekly-a3", "group:weekly", groupSummary, now-27*minute, "助理", "助"),
	}

	s.replies = map[string]string{
		"writing-report":      writingReply,
		"writing-article":     articleReply,
		"research-competitor": researchReply,
		"coding-scroll":       codingReply,
		"schedule-week":       scheduleReply,
		"group:weekly":        groupReply,
		"group:product":       groupReply,
	}

	return s
}

func (s *agentService) userMsg(id, conversationID, text string, createdAt int64) contract.ChatMessage {
	return contract.ChatMessage{
		ID:             id,
		ConversationID: conversationID,
		Role:           contract.RoleUser,
		Content:        text,
		Status:         contract.MessageDone,
		CreatedAt:      createdAt,
	}
}

func (s *agentService) assistantMsg(id, conversationID, text string, createdAt int64, senderName, senderAvatar string) contract.ChatMessage {
	return contract.ChatMessage{
		ID:             id,
		ConversationID: conversationID,
		Role:           contract.RoleAssistant,
		Content:        text,
		Status:         contract.MessageDone,
		CreatedAt:      createdAt,
		SenderName:     senderName,
		SenderAvatar:   senderAvatar,
	}
}

func (s *agentService) Agents() []contract.AgentInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]contract.AgentInfo(nil), s.agents...)
}

func (s *agentService) Groups() []contract.AgentGroup {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]contract.AgentGroup(nil), s.groups...)
}

func (s *agentService) Sessions(agentID string) []contract.AgentSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := make([]contract.AgentSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		if session.AgentID == agentID {
			list = append(list, session)
		}
	}
	return list
}

func (s *agentService) Messages(conversationID string) []contract.ChatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]contract.ChatMessage(nil), s.messages[conversationID]...)
}

// MessagesPage 是内存实现，不做分页，一次性返回全部消息。
func (s *agentService) MessagesPage(conversationID string, beforeSeq int64, limit int) contract.MessagePage {
	return contract.MessagePage{Items: s.Messages(conversationID)}
}

func (s *agentService) Models() []contract.ChatModel {
	return []contract.ChatModel{
		{ID: "bk-flash", Name: "Bowknot Flash", Description: "日常对话，响应最快"},
		{ID: "bk-pro", Name: "Bowknot Pro", Description: "复杂任务与长文本"},
		{ID: "bk-reason", Name: "Bowknot Reason", Description: "推理与代码分析"},
	}
}

func (s *agentService) CreateAgent(name, description, persona string) contract.AgentInfo {
	name = strings.TrimSpace(name)
	if description == "" {
		description = "暂无描述"
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	agent := contract.AgentInfo{
		ID:          fmt.Sprintf("agent-%d", s.seq),
		Name:        name,
		Description: strings.TrimSpace(description),
		AvatarText:  firstRune(name),
		Persona:     strings.TrimSpace(persona),
		ModelName:   "Bowknot Pro",
	}
	s.agents = append(s.agents, agent)
	return agent
}

func (s *agentService) UpdateAgent(id, name, description, persona string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("Agent 名称不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.agents {
		if s.agents[i].ID != id {
			continue
		}
		s.agents[i].Name = name
		s.agents[i].Description = strings.TrimSpace(description)
		s.agents[i].AvatarText = firstRune(name)
		s.agents[i].Persona = strings.TrimSpace(persona)
		return nil
	}
	return fmt.Errorf("Agent 不存在：%s", id)
}

func (s *agentService) RemoveAgent(id string) error {
	s.mu.Lock()
	index := -1
	for i := range s.agents {
		if s.agents[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return fmt.Errorf("Agent 不存在：%s", id)
	}
	// 收集要清理的会话后解锁停止流，避免持锁回调。
	sessionIDs := make([]string, 0, 4)
	for _, session := range s.sessions {
		if session.AgentID == id {
			sessionIDs = append(sessionIDs, session.ID)
		}
	}
	s.mu.Unlock()

	for _, sessionID := range sessionIDs {
		s.Stop(sessionID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents = append(s.agents[:index], s.agents[index+1:]...)

	kept := s.sessions[:0]
	for _, session := range s.sessions {
		if session.AgentID == id {
			delete(s.messages, session.ID)
			delete(s.replies, session.ID)
			continue
		}
		kept = append(kept, session)
	}
	s.sessions = kept

	for i := range s.groups {
		s.groups[i].MemberIDs = without(s.groups[i].MemberIDs, id)
	}
	return nil
}

func (s *agentService) CreateGroup(name string, memberIDs []string) contract.AgentGroup {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	group := contract.AgentGroup{
		ID:        fmt.Sprintf("group-%d", s.seq),
		Name:      strings.TrimSpace(name),
		MemberIDs: append([]string(nil), memberIDs...),
	}
	s.groups = append(s.groups, group)
	return group
}

func (s *agentService) UpdateGroup(id, name string, memberIDs []string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("群聊名称不能为空")
	}
	if len(memberIDs) == 0 {
		return errors.New("群聊至少需要一名成员")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.groups {
		if s.groups[i].ID != id {
			continue
		}
		s.groups[i].Name = name
		s.groups[i].MemberIDs = append([]string(nil), memberIDs...)
		return nil
	}
	return fmt.Errorf("群聊不存在：%s", id)
}

func (s *agentService) RemoveGroup(id string) error {
	key := "group:" + id
	s.Stop(key)

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.groups {
		if s.groups[i].ID != id {
			continue
		}
		s.groups = append(s.groups[:i], s.groups[i+1:]...)
		delete(s.messages, key)
		delete(s.replies, key)
		return nil
	}
	return fmt.Errorf("群聊不存在：%s", id)
}

func (s *agentService) Pin(kind, id string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := &s.agents
	if kind == contract.ItemKindGroup {
		target = nil
		for i := range s.groups {
			if s.groups[i].ID == id {
				s.groups[i].Pinned = pinned
				return nil
			}
		}
		return fmt.Errorf("群聊不存在：%s", id)
	}
	for i := range *target {
		if (*target)[i].ID == id {
			(*target)[i].Pinned = pinned
			return nil
		}
	}
	return fmt.Errorf("Agent 不存在：%s", id)
}

func (s *agentService) CreateSession(agentID string) contract.AgentSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	session := contract.AgentSession{
		ID:        fmt.Sprintf("conv-%d", s.seq),
		AgentID:   agentID,
		Title:     "新会话",
		Summary:   "还没有消息",
		UpdatedAt: time.Now().UnixMilli(),
	}
	s.sessions = append(s.sessions, session)
	return session
}

func (s *agentService) RenameSession(id, title string) error {
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

func (s *agentService) DeleteSession(id string) error {
	s.Stop(id)

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sessions {
		if s.sessions[i].ID != id {
			continue
		}
		s.sessions = append(s.sessions[:i], s.sessions[i+1:]...)
		delete(s.messages, id)
		delete(s.replies, id)
		return nil
	}
	return fmt.Errorf("会话不存在：%s", id)
}

func (s *agentService) PinSession(id string, pinned bool) error {
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

func (s *agentService) Send(conversationID, text, modelID string) (string, <-chan contract.ChatEvent, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil, errors.New("请输入消息内容")
	}

	s.mu.Lock()
	if s.streams[conversationID] != nil {
		s.mu.Unlock()
		return "", nil, errors.New("当前会话正在生成回复，请先停止")
	}

	now := time.Now().UnixMilli()
	s.seq++
	userID := fmt.Sprintf("msg-%d-u", s.seq)
	s.messages[conversationID] = append(s.messages[conversationID],
		s.userMsg(userID, conversationID, text, now))
	s.lastUser[conversationID] = text

	// 新会话首条消息更新标题与摘要。
	session := s.sessionByID(conversationID)
	if session != nil && session.Title == "新会话" {
		session.Title = snippet(text, 20)
	}
	if session != nil {
		session.Summary = snippet(text, 60)
		session.UpdatedAt = now
	}

	reply := s.replies[conversationID]
	if reply == "" {
		name := "助手"
		if session != nil {
			name = session.Title
		}
		reply = fmt.Sprintf("收到，我先按「%s」的职责处理，结果出来后同步给你。", name)
	}

	messageID, events, cancel := s.beginReplyLocked(conversationID)
	// 群聊回复带发言人信息。
	senderName, senderAvatar := "", ""
	if strings.HasPrefix(conversationID, "group:") {
		senderName, senderAvatar = "助理", "助"
	}
	s.mu.Unlock()

	go s.stream(conversationID, messageID, reply, senderName, senderAvatar, events, cancel)
	return messageID, events, nil
}

func (s *agentService) Regenerate(conversationID, messageID string) (<-chan contract.ChatEvent, error) {
	s.mu.Lock()
	if s.streams[conversationID] != nil {
		s.mu.Unlock()
		return nil, errors.New("当前会话正在生成回复，请先停止")
	}

	var target *contract.ChatMessage
	for i := range s.messages[conversationID] {
		if s.messages[conversationID][i].ID == messageID {
			target = &s.messages[conversationID][i]
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
	reply, ok := s.replies[conversationID]
	if !ok {
		s.mu.Unlock()
		return nil, errors.New("该会话暂不支持重答")
	}

	senderName, senderAvatar := target.SenderName, target.SenderAvatar
	target.Content = ""
	target.Status = contract.MessageStreaming
	target.Error = ""

	cancel := make(chan struct{})
	s.streams[conversationID] = cancel
	events := make(chan contract.ChatEvent, 128)
	s.mu.Unlock()

	go s.stream(conversationID, messageID, reply, senderName, senderAvatar, events, cancel)
	return events, nil
}

func (s *agentService) DeleteMessage(conversationID, messageID string) error {
	s.mu.Lock()
	streaming := s.streams[conversationID] != nil
	s.mu.Unlock()

	if streaming {
		s.Stop(conversationID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.messages[conversationID]
	for i := range list {
		if list[i].ID != messageID {
			continue
		}
		s.messages[conversationID] = append(list[:i], list[i+1:]...)
		return nil
	}
	return errors.New("消息不存在")
}

func (s *agentService) Stop(conversationID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.streams[conversationID]; ok {
		close(cancel)
		delete(s.streams, conversationID)
	}
}

// beginReplyLocked 追加一条空的助手消息并注册流，调用方解锁后启动 goroutine。
func (s *agentService) beginReplyLocked(conversationID string) (string, chan contract.ChatEvent, chan struct{}) {
	s.seq++
	messageID := fmt.Sprintf("msg-%d-a", s.seq)
	s.messages[conversationID] = append(s.messages[conversationID], contract.ChatMessage{
		ID:             messageID,
		ConversationID: conversationID,
		Role:           contract.RoleAssistant,
		Status:         contract.MessageStreaming,
		CreatedAt:      time.Now().UnixMilli(),
	})

	cancel := make(chan struct{})
	s.streams[conversationID] = cancel
	return messageID, make(chan contract.ChatEvent, 128), cancel
}

// stream 与助手服务的模拟逻辑一致：每 30ms 推进几个字符，事件携带完整快照。
func (s *agentService) stream(conversationID, messageID, reply, senderName, senderAvatar string, events chan<- contract.ChatEvent, cancel <-chan struct{}) {
	defer func() {
		s.mu.Lock()
		if s.streams[conversationID] == cancel {
			delete(s.streams, conversationID)
		}
		s.mu.Unlock()
		close(events)
	}()

	runes := []rune(reply)
	content := make([]rune, 0, len(runes))

	finish := func(status contract.MessageStatus, errText string) {
		s.finishMessage(conversationID, messageID, string(content), status, errText, senderName, senderAvatar)
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

		s.setMessage(conversationID, messageID, string(content), contract.MessageStreaming, "", senderName, senderAvatar)
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

func (s *agentService) setMessage(conversationID, messageID, content string, status contract.MessageStatus, errText, senderName, senderAvatar string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.messages[conversationID]
	for i := range list {
		if list[i].ID != messageID {
			continue
		}
		list[i].Content = content
		list[i].Status = status
		list[i].Error = errText
		if senderName != "" {
			list[i].SenderName = senderName
			list[i].SenderAvatar = senderAvatar
		}
		return
	}
}

func (s *agentService) finishMessage(conversationID, messageID, content string, status contract.MessageStatus, errText, senderName, senderAvatar string) {
	s.setMessage(conversationID, messageID, content, status, errText, senderName, senderAvatar)
}

// sessionByID 按会话 ID 查找，调用方需持有锁。
func (s *agentService) sessionByID(id string) *contract.AgentSession {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			return &s.sessions[i]
		}
	}
	return nil
}

func without(ids []string, id string) []string {
	kept := ids[:0]
	for _, item := range ids {
		if item != id {
			kept = append(kept, item)
		}
	}
	return kept
}

const writingReply = `我先按你的要求过了一遍，问题主要集中在三处：

1. **开头太客套**：前两段都在铺垫，可以压成一句话直接进入结论。
2. **连接词重复**：「此外」「同时」出现了五次，读起来会有机器味。
3. **结尾太空**：最后一段没有落到具体动作，建议改成明确的下一步。

改完的版本我放在下面，你可以直接用，也可以告诉我保留哪一版的语气。`

const researchReply = `我查了一下近期的版本记录，竞品的主要变化集中在三块：

1. 支持多 Agent 协作，但入口藏在设置里。
2. 会话可以按项目归档，列表支持时间分组。
3. 插件权限按 Agent 单独授权。

如果你要做对比表，我可以按「能力 / 入口 / 限制」三列整理。`

const codingReply = `可以。我的建议是先把监听改成真正监听消息数组，再补一个「切换会话即回底」的测试用例，避免以后又回退。`

const scheduleReply = `下周三之前有两件事需要你确认：周报评审和竞品对比的截止时间。要我把它们排进同一段时间吗？`

const groupSummary = `汇总一下两位的结论：

- **研究助手**确认了竞品近三个月的更新节奏，重点在协作能力。
- **写作助手**给出的周报结构可以直接复用。

建议本周先把结论发给团队，下周再补数据附录。`

const articleReply = `这篇我建议先改结构：开头直接给结论，中间用三段分别讲背景、变化和影响，结尾落到一个具体动作。

标题可以再短一点，现在是 28 个字，读书类账号一般控制在 16 个字以内。`

const groupReply = `我先拆一下：研究助手负责资料，写作助手负责成文。

两位确认后我会把结果汇总在这里。`
