package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/store"
)

// maxGroupRounds 限制单条用户消息触发的群成员发言轮数，防止 Agent 循环对话。
const maxGroupRounds = 3

// GroupService 负责群聊编排：识别主助手与成员的 @ 点名，
// 依次唤醒成员发言，最后让群主汇总。
type GroupService struct {
	store  *store.Store
	logger *slog.Logger
	agents *AgentService

	mu    sync.Mutex
	turns map[string]*groupTurn
	wg    sync.WaitGroup
	// revision 在群聊状态变化时递增，UI 据此刷新成员发言。
	revision atomic.Uint64
}

type groupTurn struct {
	cancel chan struct{}
	once   sync.Once
}

func (t *groupTurn) stop() {
	t.once.Do(func() { close(t.cancel) })
}

func (t *groupTurn) stopped() bool {
	select {
	case <-t.cancel:
		return true
	default:
		return false
	}
}

func NewGroupService(st *store.Store, logger *slog.Logger, agents *AgentService) *GroupService {
	if logger == nil {
		logger = slog.Default()
	}
	return &GroupService{store: st, logger: logger, agents: agents, turns: map[string]*groupTurn{}}
}

// Revision 返回群聊协作版本号。
func (g *GroupService) Revision() uint64 {
	return g.revision.Load()
}

// Shutdown 停止所有群聊协作并等待编排收尾。
func (g *GroupService) Shutdown(ctx context.Context) error {
	g.mu.Lock()
	turns := make([]*groupTurn, 0, len(g.turns))
	for _, turn := range g.turns {
		turns = append(turns, turn)
	}
	g.mu.Unlock()
	for _, turn := range turns {
		turn.stop()
	}

	done := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return nil
}

// Busy 判断指定群聊是否仍在协作轮次中。
func (g *GroupService) Busy(conversationID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.turns[conversationID] != nil
}

// Cancel 停止指定群聊的后续协作；当前正在生成的成员会被 Stop 一并停止。
func (g *GroupService) Cancel(conversationID string) {
	g.mu.Lock()
	turn := g.turns[conversationID]
	g.mu.Unlock()
	if turn != nil {
		turn.stop()
	}
}

// Start 写入用户消息并启动主助手回复，成员编排在主回复结束后进行。
func (g *GroupService) Start(conversationID, text string) (string, <-chan contract.ChatEvent, error) {
	_, groupID, isGroup := resolveConversationID(conversationID)
	if !isGroup {
		return "", nil, errors.New("群聊会话 ID 无效")
	}
	group, err := g.store.Groups.Get(context.Background(), groupID)
	if err != nil {
		return "", nil, groupError(err, groupID)
	}

	// 用户直接点名成员、且没有点名群主时，群主完全不发言：
	// 只写入用户消息，交给成员直接回答，避免无关的插话。
	members := g.agents.groupMembers(group)
	direct := matchMentions(text, members, "")
	mainName := g.agents.mainDisplayName()
	if len(direct) > 0 && !mentionsMemberName(text, mainName) {
		if err := g.agents.AppendGroupUserMessage(conversationID, text); err != nil {
			return "", nil, err
		}
		turn := &groupTurn{cancel: make(chan struct{})}
		g.mu.Lock()
		if previous := g.turns[conversationID]; previous != nil {
			previous.stop()
		}
		g.turns[conversationID] = turn
		g.mu.Unlock()
		g.bump()

		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			g.orchestrate(conversationID, group, text, "", turn)
			g.finish(conversationID, turn)
		}()

		done := make(chan contract.ChatEvent)
		close(done)
		return "", done, nil
	}

	finished := make(chan struct{})
	messageID, events, err := g.agents.StartTurn(TurnInput{
		ConversationID: conversationID,
		Text:           text,
		SenderKind:     domain.SenderMain,
		MessageType:    domain.MessageTypeChat,
		Visibility:     domain.VisibilityUser,
		Finished:       finished,
	})
	if err != nil {
		return "", nil, err
	}

	turn := &groupTurn{cancel: make(chan struct{})}
	g.mu.Lock()
	if previous := g.turns[conversationID]; previous != nil {
		previous.stop()
	}
	g.turns[conversationID] = turn
	g.mu.Unlock()
	g.bump()

	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		select {
		case <-finished:
		case <-turn.cancel:
			g.finish(conversationID, turn)
			return
		}
		g.orchestrate(conversationID, group, text, messageID, turn)
		g.finish(conversationID, turn)
	}()

	return messageID, events, nil
}

func (g *GroupService) finish(conversationID string, turn *groupTurn) {
	g.mu.Lock()
	if g.turns[conversationID] == turn {
		delete(g.turns, conversationID)
	}
	g.mu.Unlock()
	g.bump()
}

func (g *GroupService) bump() {
	g.revision.Add(1)
}

// orchestrate 解析用户与主回复里的 @ 点名，按轮次唤醒成员，最后让群主汇总。
// mainMessageID 为空表示本轮没有群主回复（用户直接点名的场景）。
func (g *GroupService) orchestrate(conversationID string, group domain.Group, userText, mainMessageID string, turn *groupTurn) {
	ctx := context.Background()
	mainContent := ""
	if mainMessageID != "" {
		mainMessage, err := g.store.Messages.Get(ctx, mainMessageID)
		if err != nil {
			g.logger.Warn("读取群主回复失败", "message", mainMessageID, "error", err)
			return
		}
		if mainMessage.Status != domain.MessageDone {
			return
		}
		mainContent = mainMessage.Content
	}

	members := g.agents.groupMembers(group)
	if len(members) == 0 {
		return
	}

	// 用户直接 @ 的成员也要被唤醒；随后补充群主回复里的点名。
	direct := matchMentions(userText, members, "")
	pending := append([]string(nil), direct...)
	for _, mentioned := range matchMentions(mainContent, members, "") {
		if containsString(pending, mentioned) {
			continue
		}
		pending = append(pending, mentioned)
	}
	spoken := make(map[string]bool, len(members))
	names := make([]string, 0, len(members))

	for round := 1; round <= maxGroupRounds && len(pending) > 0; round++ {
		next := make([]string, 0, len(pending))
		for _, memberID := range pending {
			if turn.stopped() {
				return
			}
			if spoken[memberID] {
				continue
			}
			spoken[memberID] = true

			content, ok := g.runMember(conversationID, group, memberID, round)
			if !ok {
				continue
			}
			names = append(names, memberName(members, memberID))
			g.bump()

			for _, mentioned := range matchMentions(content, members, memberID) {
				if spoken[mentioned] || containsString(next, mentioned) {
					continue
				}
				next = append(next, mentioned)
			}
		}
		pending = next
	}

	if len(names) == 0 || turn.stopped() {
		return
	}
	// 用户直接点名成员时，成员回答完就结束，群主不再追加总结，
	// 避免同一条消息里群主反复出现。
	if len(direct) > 0 {
		return
	}
	g.runSummary(conversationID, names)
	g.bump()
}

// runMember 让一位成员在群内发言，返回其最终内容。
func (g *GroupService) runMember(conversationID string, group domain.Group, agentID string, round int) (string, bool) {
	_, events, err := g.agents.StartTurn(TurnInput{
		ConversationID: conversationID,
		GroupAgentID:   agentID,
		SystemExtra:    g.memberExtra(group, agentID, round),
		SenderKind:     domain.SenderAgent,
		MessageType:    domain.MessageTypeChat,
		Visibility:     domain.VisibilityUser,
	})
	if err != nil {
		g.logger.Warn("群成员发言失败", "group", group.ID, "agent", agentID, "error", err)
		return "", false
	}

	content := ""
	ok := false
	for event := range events {
		if !event.Done {
			continue
		}
		if event.Status != contract.MessageDone {
			return event.Content, false
		}
		content = event.Content
		ok = true
	}
	return content, ok && strings.TrimSpace(content) != ""
}

// runSummary 让群主在成员讨论后给出最终答复。
func (g *GroupService) runSummary(conversationID string, names []string) {
	extra := fmt.Sprintf("【群聊协作】成员「%s」已经在群里发言。现在请用 1-2 句话面向用户收尾：只补充必要结论，不要复述成员原话，不要逐条罗列，不要再 @ 任何成员；如果成员已经直接回答了用户，一句话确认即可。",
		strings.Join(names, "」「"))
	_, events, err := g.agents.StartTurn(TurnInput{
		ConversationID: conversationID,
		GroupSummary:   true,
		SystemExtra:    extra,
		SenderKind:     domain.SenderMain,
		MessageType:    domain.MessageTypeChat,
		Visibility:     domain.VisibilityUser,
	})
	if err != nil {
		g.logger.Warn("群主汇总失败", "error", err)
		return
	}
	for range events {
	}
}

func (g *GroupService) memberExtra(group domain.Group, agentID string, round int) string {
	name := "成员"
	if agent, err := g.store.Agents.Get(context.Background(), agentID); err == nil && strings.TrimSpace(agent.Name) != "" {
		name = agent.Name
	}
	lastRound := round >= maxGroupRounds
	extra := fmt.Sprintf("【群聊协作】你是群成员「%s」，当前是第 %d/%d 轮成员发言。请针对群里的讨论给出你的专业意见。", name, round, maxGroupRounds)
	if lastRound {
		extra += "\n这是最后一轮，不要再 @ 其他成员。"
	}
	return extra
}

// mentionsMemberName 判断文本是否 @ 了指定名字，兼容全角 @。
func mentionsMemberName(text, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	normalized := strings.ReplaceAll(text, "＠", "@")
	return strings.Contains(normalized, "@"+name)
}

// matchMentions 从文本中解析 @成员名，按出现顺序返回成员 ID。
// 名称较长的成员优先匹配，避免「小明」抢走「小明助手」的 @。
func matchMentions(content string, members []domain.Agent, exclude string) []string {
	normalized := strings.ReplaceAll(content, "＠", "@")
	type hit struct {
		id  string
		pos int
	}
	hits := make([]hit, 0, len(members))
	claimed := make([]bool, len(normalized))

	sorted := append([]domain.Agent(nil), members...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len([]rune(sorted[i].Name)) > len([]rune(sorted[j].Name))
	})

	for _, member := range sorted {
		name := strings.TrimSpace(member.Name)
		if name == "" || member.ID == exclude {
			continue
		}
		needle := "@" + name
		offset := 0
		for offset < len(normalized) {
			index := strings.Index(normalized[offset:], needle)
			if index < 0 {
				break
			}
			start := offset + index
			end := start + len(needle)
			free := true
			for i := start; i < end; i++ {
				if claimed[i] {
					free = false
					break
				}
			}
			if free {
				for i := start; i < end; i++ {
					claimed[i] = true
				}
				hits = append(hits, hit{id: member.ID, pos: start})
				break
			}
			offset = end
		}
	}

	sort.SliceStable(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })
	out := make([]string, 0, len(hits))
	for _, item := range hits {
		out = append(out, item.id)
	}
	return out
}

func memberName(members []domain.Agent, agentID string) string {
	for _, member := range members {
		if member.ID == agentID {
			return member.Name
		}
	}
	return "成员"
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
