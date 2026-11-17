package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/platform/id"
	"bowknot-workbench/internal/store"
)

const (
	// memoryBudgetRunes 是注入 prompt 的记忆块字符预算，超出按评分截断。
	memoryBudgetRunes = 2000
	// memoryMaxItems 是单次注入的记忆条数上限。
	memoryMaxItems = 12
	// memoryRecentDays 是「最近记忆」的时间窗口。
	memoryRecentDays = 7
	// memoryExtractLimit 是每轮抽取写入的记忆条数上限。
	memoryExtractLimit = 3
	// memoryContentLimit 是单条记忆的字符上限。
	memoryContentLimit = 200
	// memoryQueryTimeout 是抽取与摘要的单次模型调用超时。
	memoryQueryTimeout = 20 * time.Second
	// memoryDigestTimeout 是按需生成日摘要的超时，失败不阻塞回复。
	memoryDigestTimeout = 10 * time.Second
	// memoryDigestDayLimit 是时间线索出现时最多展开的摘要周期数。
	memoryDigestDayLimit = 3
	// memoryDigestGenLimit 是单次检索最多补建的摘要数量。
	memoryDigestGenLimit = 3

	userMemoryOwnerID = assistantOwnerID
	memoryGlobalKey   = "memory.enabled"
	memoryAgentPrefix = "memory.agent."
)

const memoryExtractSystemPrompt = `你是 bowknot-workbench 的长期记忆抽取器。
从用户消息里提取值得长期保留的原子事实，只记录你觉得应该记忆的事，例如用户偏好，对你说的有趣的事，难过的事。
输出 JSON 数组，最多 3 条；没有可记录内容时输出 []。
每个元素的字段：
- scope: "user"（跨 Agent 的全局偏好）或 "agent"（当前 Agent 专属）
- kind: "profile" | "preference" | "fact" | "decision" | "event"
- content: 以你第一人称写成的完整陈述，不超过 200 字；不要以「用户」作主语，例如写「主人今天丢了100元」而不是「用户今天丢了100元」;「主人今天夸奖了我」而不是「用户今天夸奖了我」
- keywords: 3-8 个关键词组成的字符串数组
- occurred_at: RFC3339 时间，缺省用当前时间
- confidence: 0 到 1
- supersedes_id: 用户明确纠正旧信息时填写旧记忆 ID，否则空字符串
不要输出 JSON 以外的任何内容。`

const memoryDigestSystemPrompt = `你是 bowknot-workbench 的记忆摘要器。
把给定的一组原子记忆压缩成 100-200 字的中文摘要，并给出 3-8 个关键词。
只压缩已有内容，不引入新事实。
只输出 JSON：{"summary":"...","keywords":["..."]}。`

// memoryCandidate 是模型抽取返回的一条候选记忆。
type memoryCandidate struct {
	Scope        string   `json:"scope"`
	Kind         string   `json:"kind"`
	Content      string   `json:"content"`
	Keywords     []string `json:"keywords"`
	OccurredAt   string   `json:"occurred_at"`
	Confidence   float64  `json:"confidence"`
	SupersedesID string   `json:"supersedes_id"`
}

type memoryOwner struct {
	scope domain.MemoryScope
	owner string
}

// scoredMemory 是检索阶段的中间结果。
type scoredMemory struct {
	item    domain.Memory
	score   float64
	matched bool
	recent  bool
}

// MemoryService 负责长期记忆的写入、检索、摘要与用户管理。
// 抽取与摘要异步执行，失败只记录日志，不影响聊天主链路。
type MemoryService struct {
	store    *store.Store
	logger   *slog.Logger
	provider llm.Provider

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// now 便于测试固定时间；并发读取安全。
	now func() time.Time
}

func NewMemoryService(st *store.Store, logger *slog.Logger, provider llm.Provider) *MemoryService {
	if logger == nil {
		logger = slog.Default()
	}
	if provider == nil {
		provider = llm.NewFake(llm.FakeOptions{})
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &MemoryService{
		store:    st,
		logger:   logger,
		provider: provider,
		ctx:      ctx,
		cancel:   cancel,
		now:      time.Now,
	}
}

// Shutdown 取消进行中的抽取与摘要任务，避免退出时写库。
func (m *MemoryService) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.cancel()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return nil
}

// MemoryEnabled 查询全局或某个 Agent 是否允许长期记忆；默认开启。
func (m *MemoryService) MemoryEnabled(ownerID string) bool {
	if m == nil {
		return false
	}
	key := memoryGlobalKey
	if ownerID != "" {
		key = memoryAgentPrefix + ownerID
	}
	value, err := m.store.Meta.Get(context.Background(), key)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			m.logger.Warn("读取记忆开关失败，按开启处理", "key", key, "error", err)
		}
		return true
	}
	return value != "0"
}

func (m *MemoryService) SetMemoryEnabled(ownerID string, enabled bool) error {
	if m == nil {
		return errors.New("记忆服务未启用")
	}
	key := memoryGlobalKey
	if ownerID != "" {
		key = memoryAgentPrefix + ownerID
	}
	value := "0"
	if enabled {
		value = "1"
	}
	if err := m.store.Meta.Set(context.Background(), key, value); err != nil {
		return fmt.Errorf("保存记忆开关失败：%w", err)
	}
	return nil
}

// Memories 返回记忆管理列表；ownerID 为空表示全局用户记忆。
func (m *MemoryService) Memories(scope, ownerID string) []contract.MemoryItem {
	if m == nil {
		return nil
	}
	memoryScope, owner := normalizeMemoryOwner(scope, ownerID)
	items, err := m.store.Memories.ListActive(context.Background(), memoryScope, owner, 500)
	if err != nil {
		m.logger.Error("读取记忆列表失败", "scope", scope, "owner", owner, "error", err)
		return nil
	}
	out := make([]contract.MemoryItem, 0, len(items))
	for _, item := range items {
		out = append(out, toMemoryItem(item))
	}
	return out
}

func (m *MemoryService) SetPinned(memoryID string, pinned bool) error {
	if m == nil {
		return errors.New("记忆服务未启用")
	}
	ctx := context.Background()
	item, err := m.store.Memories.Get(ctx, memoryID)
	if err != nil {
		return memoryError(err, memoryID)
	}
	if err := m.store.Memories.SetPinned(ctx, memoryID, pinned, m.now().UnixMilli()); err != nil {
		return memoryError(err, memoryID)
	}
	m.markDigestDirty(item.Scope, item.OwnerID, item.DayKey)
	return nil
}

func (m *MemoryService) UpdateMemory(memoryID, content string) error {
	if m == nil {
		return errors.New("记忆服务未启用")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("记忆内容不能为空")
	}
	content = truncateRunes(content, memoryContentLimit)

	ctx := context.Background()
	item, err := m.store.Memories.Get(ctx, memoryID)
	if err != nil {
		return memoryError(err, memoryID)
	}
	item.Content = content
	item.ContentHash = hashContent(content)
	item.Keywords = strings.Join(deriveKeywords(content), " ")
	item.UpdatedAt = m.now().UnixMilli()
	if err := m.store.Memories.Update(ctx, item); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return errors.New("已存在相同内容的记忆")
		}
		return memoryError(err, memoryID)
	}
	m.markDigestDirty(item.Scope, item.OwnerID, item.DayKey)
	return nil
}

func (m *MemoryService) DeleteMemory(memoryID string) error {
	if m == nil {
		return errors.New("记忆服务未启用")
	}
	ctx := context.Background()
	item, err := m.store.Memories.Get(ctx, memoryID)
	if err != nil {
		return memoryError(err, memoryID)
	}
	if err := m.store.Memories.Delete(ctx, memoryID); err != nil {
		return memoryError(err, memoryID)
	}
	m.markDigestDirty(item.Scope, item.OwnerID, item.DayKey)
	return nil
}

func (m *MemoryService) ClearMemories() error {
	if m == nil {
		return errors.New("记忆服务未启用")
	}
	ctx := context.Background()
	if err := m.store.Memories.DeleteAll(ctx); err != nil {
		return fmt.Errorf("清空记忆失败：%w", err)
	}
	if err := m.store.MemoryDigests.DeleteAll(ctx); err != nil {
		return fmt.Errorf("清空记忆摘要失败：%w", err)
	}
	return nil
}

// ExtractAsync 在回复完成后异步抽取记忆，不阻塞事件通道关闭。
func (m *MemoryService) ExtractAsync(agentID, conversationID, messageID, userText, replyText string) {
	if m == nil || strings.TrimSpace(userText) == "" {
		return
	}
	globalOn := m.MemoryEnabled("")
	agentOn := agentID != "" && agentID != mainAgentID && m.MemoryEnabled(agentID)
	if !globalOn && !agentOn {
		return
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ctx, cancel := context.WithTimeout(m.ctx, memoryQueryTimeout)
		defer cancel()
		if err := m.extract(ctx, agentID, conversationID, messageID, userText, replyText); err != nil {
			m.logger.Warn("记忆抽取失败，跳过本轮", "conversation", conversationID, "error", err)
		}
	}()
}

func (m *MemoryService) extract(ctx context.Context, agentID, conversationID, messageID, userText, replyText string) error {
	now := m.now()
	req := llm.ChatRequest{
		System: fmt.Sprintf("%s\n当前时间：%s。occurred_at 只能基于当前时间推算；对话里没有明确日期时，直接使用当前时间，严禁编造日期。",
			memoryExtractSystemPrompt, now.Format("2006-01-02 15:04:05")),
		Messages: []llm.Message{{
			Role:    "user",
			Content: fmt.Sprintf("用户消息：%s\n助手回复：%s", userText, replyText),
		}},
	}
	text, err := m.provider.Complete(ctx, req)
	if err != nil {
		return err
	}
	candidates, err := parseMemoryCandidates(text)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}

	written := 0
	for _, candidate := range candidates {
		if written >= memoryExtractLimit {
			break
		}
		item, ok := m.candidateToMemory(candidate, agentID, conversationID, messageID, now)
		if !ok {
			continue
		}
		ok, err := m.save(ctx, item)
		if err != nil {
			return err
		}
		if ok {
			written++
		}
	}
	if written == 0 {
		return nil
	}
	if err := m.store.Messages.SetMemoryWritten(ctx, messageID, true); err != nil && !errors.Is(err, store.ErrNotFound) {
		m.logger.Warn("更新消息记忆标记失败", "message", messageID, "error", err)
	}
	return nil
}

// candidateToMemory 校验并规范化一条候选记忆，命中敏感信息或开关关闭时跳过。
// 记忆归属由会话所属 Agent 决定：主助手写入用户全局域，子 Agent 写入自己的专属域，
// 不采用模型给出的 scope，避免两套记忆互相污染。
func (m *MemoryService) candidateToMemory(candidate memoryCandidate, agentID, conversationID, messageID string, now time.Time) (domain.Memory, bool) {
	scope := domain.MemoryScopeUser
	owner := userMemoryOwnerID
	if agentID != "" && agentID != mainAgentID {
		scope = domain.MemoryScopeAgent
		owner = agentID
	}
	if scope == domain.MemoryScopeUser && !m.MemoryEnabled("") {
		return domain.Memory{}, false
	}
	if scope == domain.MemoryScopeAgent && !m.MemoryEnabled(owner) {
		return domain.Memory{}, false
	}

	content := strings.TrimSpace(candidate.Content)
	if content == "" || containsSensitive(content) {
		return domain.Memory{}, false
	}
	content = truncateRunes(content, memoryContentLimit)

	kind := domain.MemoryKind(strings.TrimSpace(candidate.Kind))
	switch kind {
	case domain.MemoryKindProfile, domain.MemoryKindPreference, domain.MemoryKindFact,
		domain.MemoryKindDecision, domain.MemoryKindEvent:
	default:
		kind = domain.MemoryKindPreference
	}

	occurredAt := now
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(candidate.OccurredAt)); err == nil {
		// 模型偶尔会编造几个月前或未来的日期；只接受近期的时间，其余回退到当前时间。
		if !parsed.Before(now.Add(-7*24*time.Hour)) && !parsed.After(now.Add(24*time.Hour)) {
			occurredAt = parsed
		}
	}
	confidence := candidate.Confidence
	if confidence <= 0 || confidence > 1 {
		confidence = 0.5
	}
	importance := 0
	if kind == domain.MemoryKindEvent {
		importance = 1
	}

	return domain.Memory{
		ID:                   id.New(),
		Scope:                scope,
		OwnerID:              owner,
		Kind:                 kind,
		Content:              content,
		ContentHash:          hashContent(content),
		Keywords:             strings.Join(deriveKeywords(content), " "),
		OccurredAt:           occurredAt.UnixMilli(),
		DayKey:               dayKeyOf(occurredAt),
		SourceConversationID: conversationID,
		SourceMessageID:      messageID,
		SourceRole:           "user",
		Confidence:           confidence,
		Importance:           importance,
		Status:               domain.MemoryActive,
		SupersedesID:         strings.TrimSpace(candidate.SupersedesID),
		CreatedAt:            now.UnixMilli(),
		UpdatedAt:            now.UnixMilli(),
	}, true
}

// save 处理去重与替代；返回是否产生了一次写入或更新。
func (m *MemoryService) save(ctx context.Context, item domain.Memory) (bool, error) {
	existing, err := m.store.Memories.GetActiveByHash(ctx, item.Scope, item.OwnerID, item.Kind, item.ContentHash)
	if err == nil {
		confidence := math.Max(existing.Confidence, item.Confidence)
		if item.Pinned && !existing.Pinned {
			if err := m.store.Memories.SetPinned(ctx, existing.ID, true, item.UpdatedAt); err != nil {
				return false, err
			}
		}
		if err := m.store.Memories.Touch(ctx, existing.ID, confidence, item.UpdatedAt, existing.LastUsedAt); err != nil {
			return false, err
		}
		m.markDigestDirty(item.Scope, item.OwnerID, item.DayKey)
		return true, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}

	if item.SupersedesID != "" {
		if err := m.store.Memories.Supersede(ctx, item.SupersedesID, item.UpdatedAt); err != nil && !errors.Is(err, store.ErrNotFound) {
			m.logger.Warn("替代旧记忆失败", "memory", item.SupersedesID, "error", err)
		}
	}
	if err := m.store.Memories.Insert(ctx, item); err != nil {
		if errors.Is(err, store.ErrConflict) {
			// 并发写入同一条内容，交给下一次抽取去重。
			return false, nil
		}
		return false, err
	}
	m.markDigestDirty(item.Scope, item.OwnerID, item.DayKey)
	return true, nil
}

// PromptBlock 组装注入 prompt 的记忆块；没有可用记忆时返回空字符串。
// 主助手只读用户全局域，子 Agent 只读自己的专属域，两边不交叉注入。
func (m *MemoryService) PromptBlock(agentID, question string) string {
	if m == nil {
		return ""
	}

	owners := make([]memoryOwner, 0, 1)
	if agentID == "" || agentID == mainAgentID {
		if m.MemoryEnabled("") {
			owners = append(owners, memoryOwner{scope: domain.MemoryScopeUser, owner: userMemoryOwnerID})
		}
	} else if m.MemoryEnabled(agentID) {
		owners = append(owners, memoryOwner{scope: domain.MemoryScopeAgent, owner: agentID})
	}
	if len(owners) == 0 {
		return ""
	}

	now := m.now()
	recentSince := now.AddDate(0, 0, -memoryRecentDays).UnixMilli()
	tokens := questionKeywords(question)

	seen := make(map[string]bool)
	scored := make([]scoredMemory, 0)
	for _, owner := range owners {
		items, err := m.store.Memories.ListActive(context.Background(), owner.scope, owner.owner, 500)
		if err != nil {
			m.logger.Warn("读取记忆失败，跳过注入", "scope", owner.scope, "owner", owner.owner, "error", err)
			continue
		}
		for _, item := range items {
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			candidate := scoreMemory(item, tokens, recentSince)
			if !item.Pinned && !candidate.recent && !candidate.matched {
				continue
			}
			scored = append(scored, candidate)
		}
	}

	if needsRecall(question) {
		for _, candidate := range m.digestMemories(owners, tokens, seen) {
			scored = append(scored, candidate)
		}
	}
	if len(scored) == 0 {
		return ""
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].item.UpdatedAt > scored[j].item.UpdatedAt
		}
		return scored[i].score > scored[j].score
	})

	selected := make([]domain.Memory, 0, memoryMaxItems)
	usedIDs := make([]string, 0, memoryMaxItems)
	used := 0
	for _, candidate := range scored {
		if len(selected) >= memoryMaxItems {
			break
		}
		line := memoryLine(candidate.item)
		cost := utf8.RuneCountInString(line) + 1
		if used+cost > memoryBudgetRunes {
			continue
		}
		used += cost
		selected = append(selected, candidate.item)
		usedIDs = append(usedIDs, candidate.item.ID)
	}
	if len(selected) == 0 {
		return ""
	}
	m.markUsedAsync(usedIDs)
	return renderMemoryBlock(selected)
}

// digestMemories 在用户提到旧事时展开日摘要对应的原子记忆。
func (m *MemoryService) digestMemories(owners []memoryOwner, tokens []string, seen map[string]bool) []scoredMemory {
	ctx, cancel := context.WithTimeout(m.ctx, memoryDigestTimeout)
	defer cancel()

	out := make([]scoredMemory, 0)
	matchedDigests := 0
	generated := 0
	for _, owner := range owners {
		if matchedDigests >= memoryDigestDayLimit {
			break
		}
		days, err := m.store.Memories.ListDays(ctx, owner.scope, owner.owner, 14)
		if err != nil {
			m.logger.Warn("读取记忆日期失败", "scope", owner.scope, "owner", owner.owner, "error", err)
			continue
		}
		for _, day := range days {
			if matchedDigests >= memoryDigestDayLimit {
				break
			}
			digest, err := m.store.MemoryDigests.Get(ctx, owner.scope, owner.owner, domain.DigestDay, day)
			if err != nil || digest.Dirty {
				// 每次检索最多补建 3 个摘要，避免一次请求触发大量模型调用。
				if generated >= memoryDigestGenLimit {
					continue
				}
				generated++
				digest, err = m.ensureDayDigest(ctx, owner.scope, owner.owner, day)
				if err != nil {
					if !errors.Is(err, store.ErrNotFound) {
						m.logger.Warn("生成日摘要失败", "day", day, "error", err)
					}
					continue
				}
			}
			if !digestMatches(digest, tokens) {
				continue
			}
			matchedDigests++
			items, err := m.store.MemoryDigests.Items(ctx, digest.ID)
			if err != nil {
				m.logger.Warn("读取摘要关联记忆失败", "digest", digest.ID, "error", err)
				continue
			}
			for _, item := range items {
				if seen[item.ID] {
					continue
				}
				seen[item.ID] = true
				out = append(out, scoredMemory{item: item, score: 20 + item.Confidence, recent: false, matched: true})
			}
		}
	}
	return out
}

// ensureDayDigest 按需生成或重建某一天的摘要；没有记忆时返回 ErrNotFound。
func (m *MemoryService) ensureDayDigest(ctx context.Context, scope domain.MemoryScope, ownerID, day string) (domain.MemoryDigest, error) {
	items, err := m.store.Memories.ListByDay(ctx, scope, ownerID, day)
	if err != nil {
		return domain.MemoryDigest{}, err
	}
	if len(items) == 0 {
		return domain.MemoryDigest{}, store.ErrNotFound
	}
	version := digestSourceVersion(items)

	existing, err := m.store.MemoryDigests.Get(ctx, scope, ownerID, domain.DigestDay, day)
	if err == nil && !existing.Dirty && existing.SourceVersion == version {
		return existing, nil
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return domain.MemoryDigest{}, err
	}

	summary, keywords, err := m.summarize(ctx, items)
	if err != nil {
		return domain.MemoryDigest{}, err
	}
	now := m.now()
	digest := domain.MemoryDigest{
		ID:            id.New(),
		Scope:         scope,
		OwnerID:       ownerID,
		Level:         domain.DigestDay,
		PeriodStart:   day,
		PeriodEnd:     day,
		Summary:       summary,
		Keywords:      strings.Join(keywords, " "),
		MemoryCount:   len(items),
		SourceVersion: version,
		CreatedAt:     now.UnixMilli(),
		UpdatedAt:     now.UnixMilli(),
	}
	memoryIDs := make([]string, 0, len(items))
	for _, item := range items {
		memoryIDs = append(memoryIDs, item.ID)
	}
	if err := m.store.MemoryDigests.Upsert(ctx, digest, memoryIDs); err != nil {
		return domain.MemoryDigest{}, err
	}
	return m.store.MemoryDigests.Get(ctx, scope, ownerID, domain.DigestDay, day)
}

func (m *MemoryService) summarize(ctx context.Context, items []domain.Memory) (string, []string, error) {
	// 单日记忆过多时只取前 50 条，避免一次输入过长。
	if len(items) > 50 {
		items = items[:50]
	}
	var body strings.Builder
	for _, item := range items {
		fmt.Fprintf(&body, "- %s：%s\n", time.UnixMilli(item.OccurredAt).In(time.Local).Format("2006-01-02 15:04"), item.Content)
	}
	text, err := m.provider.Complete(ctx, llm.ChatRequest{
		System:   memoryDigestSystemPrompt,
		Messages: []llm.Message{{Role: "user", Content: body.String()}},
	})
	if err != nil {
		return "", nil, err
	}
	var decoded struct {
		Summary  string   `json:"summary"`
		Keywords []string `json:"keywords"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &decoded); err != nil || strings.TrimSpace(decoded.Summary) == "" {
		// 摘要失败不应该阻塞检索，用原文拼接作为兜底。
		return fallbackSummary(items), deriveKeywords(body.String()), nil
	}
	return strings.TrimSpace(decoded.Summary), decoded.Keywords, nil
}

func (m *MemoryService) markDigestDirty(scope domain.MemoryScope, ownerID, day string) {
	if day == "" {
		return
	}
	if err := m.store.MemoryDigests.MarkDirty(context.Background(), scope, ownerID, domain.DigestDay, day); err != nil {
		m.logger.Warn("标记记忆摘要过期失败", "day", day, "error", err)
	}
}

// markUsedAsync 异步刷新注入记忆的使用时间，不阻塞回复。
func (m *MemoryService) markUsedAsync(ids []string) {
	if len(ids) == 0 {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := m.store.Memories.MarkUsed(context.Background(), ids, m.now().UnixMilli()); err != nil {
			m.logger.Warn("更新记忆使用时间失败", "error", err)
		}
	}()
}

func scoreMemory(item domain.Memory, tokens []string, recentSince int64) scoredMemory {
	candidate := scoredMemory{item: item}
	if item.Pinned {
		candidate.score += 100
	}
	if memoryMatches(item, tokens) {
		candidate.matched = true
		candidate.score += 10
	}
	if item.UpdatedAt >= recentSince {
		candidate.recent = true
		candidate.score += 5
	}
	candidate.score += float64(item.Importance) * 2
	candidate.score += item.Confidence
	return candidate
}

func memoryMatches(item domain.Memory, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	content := strings.ToLower(item.Content)
	keywords := strings.ToLower(item.Keywords)
	for _, token := range tokens {
		if strings.Contains(content, token) || strings.Contains(keywords, token) {
			return true
		}
	}
	return false
}

func digestMatches(digest domain.MemoryDigest, tokens []string) bool {
	if len(tokens) == 0 {
		// 没有可匹配的关键词时，只在明确的回忆语境下展开最近摘要。
		return true
	}
	text := strings.ToLower(digest.Summary + " " + digest.Keywords)
	for _, token := range tokens {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func renderMemoryBlock(items []domain.Memory) string {
	userLines := make([]string, 0, len(items))
	agentLines := make([]string, 0, len(items))
	for _, item := range items {
		line := memoryLine(item)
		if item.Scope == domain.MemoryScopeAgent {
			agentLines = append(agentLines, line)
		} else {
			userLines = append(userLines, line)
		}
	}
	var out strings.Builder
	if len(userLines) > 0 {
		out.WriteString("[用户全局记忆]\n")
		out.WriteString(strings.Join(userLines, "\n"))
		out.WriteString("\n")
	}
	if len(agentLines) > 0 {
		out.WriteString("[Agent 专属记忆]\n")
		out.WriteString(strings.Join(agentLines, "\n"))
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func memoryLine(item domain.Memory) string {
	return "- " + time.UnixMilli(item.OccurredAt).In(time.Local).Format("2006-01-02") + "：" + item.Content
}

func needsRecall(question string) bool {
	for _, cue := range []string{"上次", "之前", "以前", "那天", "上周", "去年", "还记得", "回忆"} {
		if strings.Contains(question, cue) {
			return true
		}
	}
	return false
}

// questionKeywords 同时产出英文单词与中文二元组，便于与记忆关键词粗匹配。
func questionKeywords(question string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 16)
	add := func(token string) {
		token = strings.ToLower(strings.TrimSpace(token))
		if utf8.RuneCountInString(token) < 2 || utf8.RuneCountInString(token) > 8 || seen[token] {
			return
		}
		seen[token] = true
		out = append(out, token)
	}

	word := make([]rune, 0, 8)
	flush := func() {
		if len(word) > 0 {
			add(string(word))
			word = word[:0]
		}
	}
	for _, r := range question {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
			if unicode.Is(unicode.Han, r) && len(word) >= 2 {
				add(string(word[len(word)-2:]))
			}
			continue
		}
		flush()
	}
	flush()
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}

// deriveKeywords 从内容里提取关键词；中文按 2-4 字片段补充。
func deriveKeywords(content string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 8)
	add := func(token string) {
		token = strings.TrimSpace(token)
		if utf8.RuneCountInString(token) < 2 || seen[token] {
			return
		}
		seen[token] = true
		out = append(out, token)
	}

	word := make([]rune, 0, 16)
	flush := func() {
		if len(word) >= 2 {
			add(string(word))
		}
		word = word[:0]
	}
	for _, r := range content {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
			continue
		}
		flush()
	}
	flush()
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func parseMemoryCandidates(text string) ([]memoryCandidate, error) {
	body := extractJSONArray(text)
	if body == "" {
		return nil, nil
	}
	var candidates []memoryCandidate
	if err := json.Unmarshal([]byte(body), &candidates); err != nil {
		return nil, fmt.Errorf("记忆抽取结果解析失败：%w", err)
	}
	if len(candidates) > memoryExtractLimit {
		candidates = candidates[:memoryExtractLimit]
	}
	return candidates, nil
}

// extractJSONArray 去掉模型可能附带的说明文字与代码围栏。
func extractJSONArray(text string) string {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end <= start {
		return ""
	}
	return text[start : end+1]
}

func extractJSONObject(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return text
	}
	return text[start : end+1]
}

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)sk-[a-z0-9]{8,}`),
	regexp.MustCompile(`(?i)api[_-]?key`),
	regexp.MustCompile(`密码|口令|身份证|银行卡|信用卡|验证码|私钥`),
	regexp.MustCompile(`\d{16,19}`),
}

func containsSensitive(content string) bool {
	for _, pattern := range sensitivePatterns {
		if pattern.MatchString(content) {
			return true
		}
	}
	return false
}

func fallbackSummary(items []domain.Memory) string {
	var out strings.Builder
	for i, item := range items {
		if i >= 8 {
			break
		}
		if out.Len() > 0 {
			out.WriteString("；")
		}
		out.WriteString(item.Content)
	}
	return truncateRunes(out.String(), 200)
}

func digestSourceVersion(items []domain.Memory) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s:%d", item.ID, item.UpdatedAt))
	}
	sort.Strings(parts)
	return hashContent(strings.Join(parts, ","))
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(normalizeContent(content)))
	return hex.EncodeToString(sum[:])
}

// normalizeContent 统一空白与全角标点，保证同一事实得到相同的哈希。
func normalizeContent(content string) string {
	content = strings.TrimSpace(content)
	content = strings.Join(strings.Fields(content), " ")
	replacer := strings.NewReplacer("，", ",", "。", ".", "；", ";", "：", ":", "！", "!", "？", "?", "（", "(", "）", ")")
	return replacer.Replace(content)
}

func dayKeyOf(t time.Time) string {
	return t.In(time.Local).Format("2006-01-02")
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func normalizeMemoryOwner(scope, ownerID string) (domain.MemoryScope, string) {
	memoryScope := domain.MemoryScope(strings.TrimSpace(scope))
	if memoryScope != domain.MemoryScopeAgent {
		memoryScope = domain.MemoryScopeUser
	}
	owner := strings.TrimSpace(ownerID)
	if memoryScope == domain.MemoryScopeUser {
		owner = userMemoryOwnerID
	}
	if owner == "" {
		owner = mainAgentID
	}
	return memoryScope, owner
}

func toMemoryItem(item domain.Memory) contract.MemoryItem {
	return contract.MemoryItem{
		ID:         item.ID,
		Scope:      string(item.Scope),
		OwnerID:    item.OwnerID,
		Kind:       string(item.Kind),
		Content:    item.Content,
		Keywords:   item.Keywords,
		DayKey:     item.DayKey,
		OccurredAt: item.OccurredAt,
		Pinned:     item.Pinned,
	}
}

func memoryError(err error, memoryID string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("记忆不存在：%s", memoryID)
	case errors.Is(err, store.ErrClosed):
		return errors.New("数据存储已关闭")
	default:
		return fmt.Errorf("记忆操作失败：%w", err)
	}
}
