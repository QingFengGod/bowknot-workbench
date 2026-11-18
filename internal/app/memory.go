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
	// memoryRecentDays 是「最近记忆」的时间窗口，用于自动注入与检索加权。
	memoryRecentDays = 30
	// memoryContentLimit 是单条记忆的字符上限。
	memoryContentLimit = 200
	// memoryDigestTimeout 是单次摘要模型调用的超时。
	memoryDigestTimeout = 20 * time.Second
	// memoryDigestWeekWindow 是后台摘要回看的周数。
	memoryDigestWeekWindow = 8
	// memoryDigestMatchLimit 是单次检索最多展开的周期摘要数量。
	memoryDigestMatchLimit = 3
	// memoryDigestGenLimit 是单轮后台任务最多补建的摘要数量。
	memoryDigestGenLimit = 4
	// memoryDigestInterval 是后台摘要任务的执行间隔；
	// 已完成周在内容未变化时不会重复调用模型，实际频率接近每周一次。
	memoryDigestInterval = 6 * time.Hour
	// memorySearchDefaultLimit / memorySearchMaxLimit 控制检索返回条数。
	memorySearchDefaultLimit = 8
	memorySearchMaxLimit     = 20

	userMemoryOwnerID = assistantOwnerID
	memoryGlobalKey   = "memory.enabled"
	memoryAgentPrefix = "memory.agent."
)

const memoryDigestSystemPrompt = `你是 bowknot-workbench 的记忆摘要器。
把给定的一周原子记忆压缩成 150-300 字的中文摘要，并给出 3-8 个关键词。
摘要要保留可以据此查找原文的时间与主题线索，只压缩已有内容，不引入新事实。
只输出 JSON：{"summary":"...","keywords":["..."]}。`

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

// weekPeriod 是一个自然周（周一到周日）的日期范围。
type weekPeriod struct {
	start   string
	end     string
	startMs int64
	endMs   int64
}

// MemoryService 负责长期记忆的写入、检索、修改、删除与周期摘要。
// 写入与检索由模型通过内置工具触发；后台任务只负责维护周期摘要索引。
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

// Start 启动周期摘要任务：启动时补一次，之后按固定间隔检查。
// 由 main 在装配完成后调用，测试可以只调用 ensureDigests 而不启动循环。
func (m *MemoryService) Start() {
	if m == nil {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.ensureDigests()
		ticker := time.NewTicker(memoryDigestInterval)
		defer ticker.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.ensureDigests()
			}
		}
	}()
}

// Shutdown 取消进行中的摘要任务与检索记账，避免退出时写库。
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

// Remember 写入一条记忆；归属由调用方 Agent 推导，不接受模型指定作用域。
func (m *MemoryService) Remember(ctx context.Context, agentID, conversationID, messageID, content, kind string, occurredAt time.Time) (contract.MemoryItem, error) {
	if m == nil {
		return contract.MemoryItem{}, errors.New("记忆服务未启用")
	}
	owner := memoryOwnerFor(agentID)
	if !m.MemoryEnabled(memoryEnabledOwner(agentID)) {
		return contract.MemoryItem{}, errors.New("长期记忆已关闭，请先在记忆面板开启")
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return contract.MemoryItem{}, errors.New("记忆内容不能为空")
	}
	if containsSensitive(content) {
		return contract.MemoryItem{}, errors.New("记忆包含敏感信息，已拒绝写入")
	}
	content = truncateRunes(content, memoryContentLimit)

	now := m.now()
	occurred := clampOccurredAt(occurredAt, now)
	item := domain.Memory{
		ID:                   id.New(),
		Scope:                owner.scope,
		OwnerID:              owner.owner,
		Kind:                 normalizeMemoryKind(kind),
		Content:              content,
		ContentHash:          hashContent(content),
		Keywords:             strings.Join(deriveKeywords(content), " "),
		OccurredAt:           occurred.UnixMilli(),
		DayKey:               dayKeyOf(occurred),
		SourceConversationID: conversationID,
		SourceMessageID:      messageID,
		SourceRole:           "assistant",
		Confidence:           0.7,
		Status:               domain.MemoryActive,
		CreatedAt:            now.UnixMilli(),
		UpdatedAt:            now.UnixMilli(),
	}
	if item.Kind == domain.MemoryKindEvent {
		item.Importance = 1
	}

	saved, err := m.save(ctx, item)
	if err != nil {
		return contract.MemoryItem{}, fmt.Errorf("写入记忆失败：%w", err)
	}
	return toMemoryItem(saved), nil
}

// Search 检索记忆：先匹配周期摘要定位周，再按日期范围取原文；
// 摘要尚未覆盖时退回全量关键词扫描。
func (m *MemoryService) Search(ctx context.Context, agentID, query, fromDay, toDay string, limit int) []contract.MemoryItem {
	if m == nil {
		return nil
	}
	memOwner := memoryOwnerFor(agentID)
	scope, ownerID := memOwner.scope, memOwner.owner
	if !m.MemoryEnabled(memoryEnabledOwner(agentID)) {
		return nil
	}
	if limit <= 0 {
		limit = memorySearchDefaultLimit
	}
	if limit > memorySearchMaxLimit {
		limit = memorySearchMaxLimit
	}

	now := m.now()
	recentSince := now.AddDate(0, 0, -memoryRecentDays).UnixMilli()
	tokens := questionKeywords(query)

	seen := make(map[string]bool)
	items := make([]domain.Memory, 0, 16)
	add := func(list []domain.Memory) {
		for _, item := range list {
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			items = append(items, item)
		}
	}

	fromMs, hasFrom := parseDayStart(fromDay)
	toMs, hasTo := parseDayEnd(toDay)
	hasRange := hasFrom || hasTo
	switch {
	case hasRange:
		if !hasFrom {
			fromMs = 0
		}
		if !hasTo {
			toMs = math.MaxInt64
		}
		if list, err := m.store.Memories.ListByRange(ctx, scope, ownerID, fromMs, toMs); err == nil {
			add(list)
		} else {
			m.logger.Warn("按日期检索记忆失败", "error", err)
		}
	case len(tokens) > 0:
		// 先用周期摘要找出相关的周，再取这一周的原子记忆。
		digests, err := m.store.MemoryDigests.List(ctx, scope, ownerID, domain.DigestWeek, 52)
		if err != nil {
			m.logger.Warn("读取记忆摘要失败", "error", err)
		}
		matched := 0
		for _, digest := range digests {
			if matched >= memoryDigestMatchLimit {
				break
			}
			if !digestMatches(digest, tokens) {
				continue
			}
			matched++
			startMs, okFrom := parseDayStart(digest.PeriodStart)
			endMs, okTo := parseDayEnd(digest.PeriodEnd)
			if !okFrom || !okTo {
				continue
			}
			list, err := m.store.Memories.ListByRange(ctx, scope, ownerID, startMs, endMs)
			if err != nil {
				m.logger.Warn("读取周期记忆失败", "period", digest.PeriodStart, "error", err)
				continue
			}
			add(list)
		}
		if len(items) == 0 {
			// 摘要还没生成时按关键词扫描全部 active 记忆。
			all, err := m.store.Memories.ListActive(ctx, scope, ownerID, 0)
			if err != nil {
				m.logger.Warn("扫描记忆失败", "error", err)
			}
			for _, item := range all {
				if memoryMatches(item, tokens) {
					add([]domain.Memory{item})
				}
			}
		}
	}

	// 没有显式日期范围时补充最近窗口，保证能回答「最近发生了什么」。
	if !hasRange {
		recent, err := m.store.Memories.ListActive(ctx, scope, ownerID, 0)
		if err != nil {
			m.logger.Warn("读取最近记忆失败", "error", err)
		} else {
			for _, item := range recent {
				if item.UpdatedAt >= recentSince {
					add([]domain.Memory{item})
				}
			}
		}
	}
	if len(items) == 0 {
		return nil
	}

	sort.SliceStable(items, func(i, j int) bool {
		scoreI := scoreSearchMemory(items[i], tokens, recentSince)
		scoreJ := scoreSearchMemory(items[j], tokens, recentSince)
		if scoreI == scoreJ {
			return items[i].OccurredAt > items[j].OccurredAt
		}
		return scoreI > scoreJ
	})
	if len(items) > limit {
		items = items[:limit]
	}

	usedIDs := make([]string, 0, len(items))
	for _, item := range items {
		usedIDs = append(usedIDs, item.ID)
	}
	m.markUsedAsync(usedIDs)

	out := make([]contract.MemoryItem, 0, len(items))
	for _, item := range items {
		out = append(out, toMemoryItem(item))
	}
	return out
}

// UpdateMemory 修改一条属于当前 Agent 的记忆。
func (m *MemoryService) UpdateMemory(ctx context.Context, agentID, memoryID, content string) error {
	if m == nil {
		return errors.New("记忆服务未启用")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("记忆内容不能为空")
	}
	if containsSensitive(content) {
		return errors.New("记忆包含敏感信息，已拒绝写入")
	}
	content = truncateRunes(content, memoryContentLimit)

	item, err := m.store.Memories.Get(ctx, memoryID)
	if err != nil {
		return memoryError(err, memoryID)
	}
	if !m.ownsMemory(agentID, item) {
		return errors.New("无权修改其他 Agent 的记忆")
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
	m.markWeekDigestDirty(item.Scope, item.OwnerID, item.DayKey)
	return nil
}

// DeleteMemory 删除一条属于当前 Agent 的记忆。
func (m *MemoryService) DeleteMemory(ctx context.Context, agentID, memoryID string) error {
	if m == nil {
		return errors.New("记忆服务未启用")
	}
	item, err := m.store.Memories.Get(ctx, memoryID)
	if err != nil {
		return memoryError(err, memoryID)
	}
	if !m.ownsMemory(agentID, item) {
		return errors.New("无权删除其他 Agent 的记忆")
	}
	if err := m.store.Memories.Delete(ctx, memoryID); err != nil {
		return memoryError(err, memoryID)
	}
	m.markWeekDigestDirty(item.Scope, item.OwnerID, item.DayKey)
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

// save 处理去重；返回落库后的记录（去重时返回已有记录）。
func (m *MemoryService) save(ctx context.Context, item domain.Memory) (domain.Memory, error) {
	existing, err := m.store.Memories.GetActiveByHash(ctx, item.Scope, item.OwnerID, item.Kind, item.ContentHash)
	if err == nil {
		confidence := math.Max(existing.Confidence, item.Confidence)
		if err := m.store.Memories.Touch(ctx, existing.ID, confidence, item.UpdatedAt, existing.LastUsedAt); err != nil {
			return domain.Memory{}, err
		}
		existing.Confidence = confidence
		existing.UpdatedAt = item.UpdatedAt
		m.markWeekDigestDirty(item.Scope, item.OwnerID, item.DayKey)
		return existing, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return domain.Memory{}, err
	}

	if err := m.store.Memories.Insert(ctx, item); err != nil {
		if errors.Is(err, store.ErrConflict) {
			// 并发写入同一条内容：回查已有记录，按成功处理。
			if existing, getErr := m.store.Memories.GetActiveByHash(ctx, item.Scope, item.OwnerID, item.Kind, item.ContentHash); getErr == nil {
				return existing, nil
			}
			return item, nil
		}
		return domain.Memory{}, err
	}
	m.markWeekDigestDirty(item.Scope, item.OwnerID, item.DayKey)
	return item, nil
}

// PromptBlock 组装注入 prompt 的记忆块；没有可用记忆时返回空字符串。
// 主助手只读用户全局域，子 Agent 只读自己的专属域，两边不交叉注入。
// 这里只注入置顶、最近与关键词命中的热记忆，更久远的内容交给检索工具。
func (m *MemoryService) PromptBlock(agentID, question string) string {
	if m == nil {
		return ""
	}

	owner := memoryOwnerFor(agentID)
	if !m.MemoryEnabled(memoryEnabledOwner(agentID)) {
		return ""
	}

	now := m.now()
	recentSince := now.AddDate(0, 0, -memoryRecentDays).UnixMilli()
	tokens := questionKeywords(question)

	items, err := m.store.Memories.ListActive(context.Background(), owner.scope, owner.owner, 500)
	if err != nil {
		m.logger.Warn("读取记忆失败，跳过注入", "scope", owner.scope, "owner", owner.owner, "error", err)
		return ""
	}

	scored := make([]scoredMemory, 0, len(items))
	for _, item := range items {
		candidate := scoreMemory(item, tokens, recentSince)
		if !item.Pinned && !candidate.recent && !candidate.matched {
			continue
		}
		scored = append(scored, candidate)
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

// ensureDigests 为存在记忆的作用域补齐最近若干周的摘要。
// 未变化的周直接跳过，避免重复调用模型。
func (m *MemoryService) ensureDigests() {
	ctx, cancel := context.WithTimeout(m.ctx, memoryDigestTimeout*3)
	defer cancel()

	owners, err := m.store.Memories.ListOwners(ctx)
	if err != nil {
		m.logger.Warn("读取记忆归属失败，跳过摘要任务", "error", err)
		return
	}
	periods := recentWeekPeriods(m.now(), memoryDigestWeekWindow)
	generated := 0
	for _, owner := range owners {
		for _, period := range periods {
			if generated >= memoryDigestGenLimit {
				return
			}
			digest, err := m.store.MemoryDigests.Get(ctx, owner.Scope, owner.OwnerID, domain.DigestWeek, period.start)
			if err == nil && !digest.Dirty {
				continue
			}
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				m.logger.Warn("读取周摘要失败", "period", period.start, "error", err)
				continue
			}
			if _, err := m.ensureWeekDigest(ctx, memoryOwner{scope: owner.Scope, owner: owner.OwnerID}, period); err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					m.logger.Warn("生成周摘要失败", "period", period.start, "error", err)
				}
				continue
			}
			generated++
		}
	}
}

// ensureWeekDigest 生成或重建某一周的摘要；没有记忆时返回 ErrNotFound。
func (m *MemoryService) ensureWeekDigest(ctx context.Context, owner memoryOwner, period weekPeriod) (domain.MemoryDigest, error) {
	items, err := m.store.Memories.ListByRange(ctx, owner.scope, owner.owner, period.startMs, period.endMs)
	if err != nil {
		return domain.MemoryDigest{}, err
	}
	if len(items) == 0 {
		return domain.MemoryDigest{}, store.ErrNotFound
	}
	version := digestSourceVersion(items)

	existing, err := m.store.MemoryDigests.Get(ctx, owner.scope, owner.owner, domain.DigestWeek, period.start)
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
		Scope:         owner.scope,
		OwnerID:       owner.owner,
		Level:         domain.DigestWeek,
		PeriodStart:   period.start,
		PeriodEnd:     period.end,
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
	return m.store.MemoryDigests.Get(ctx, owner.scope, owner.owner, domain.DigestWeek, period.start)
}

func (m *MemoryService) summarize(ctx context.Context, items []domain.Memory) (string, []string, error) {
	// 单周记忆过多时只取前 50 条，避免一次输入过长。
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

// markWeekDigestDirty 把记忆所在的周摘要标记为过期；周摘要不存在时是空操作。
func (m *MemoryService) markWeekDigestDirty(scope domain.MemoryScope, ownerID, dayKey string) {
	if dayKey == "" {
		return
	}
	day, err := time.ParseInLocation("2006-01-02", dayKey, time.Local)
	if err != nil {
		return
	}
	period := weekPeriodOf(day)
	if err := m.store.MemoryDigests.MarkDirty(context.Background(), scope, ownerID, domain.DigestWeek, period.start); err != nil {
		m.logger.Warn("标记记忆摘要过期失败", "period", period.start, "error", err)
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

func (m *MemoryService) ownsMemory(agentID string, item domain.Memory) bool {
	owner := memoryOwnerFor(agentID)
	return item.Scope == owner.scope && item.OwnerID == owner.owner
}

// memoryOwnerFor 按调用方 Agent 推导记忆归属：主助手写用户全局域，子 Agent 写自己的专属域。
func memoryOwnerFor(agentID string) memoryOwner {
	if agentID == "" || agentID == mainAgentID {
		return memoryOwner{scope: domain.MemoryScopeUser, owner: userMemoryOwnerID}
	}
	return memoryOwner{scope: domain.MemoryScopeAgent, owner: agentID}
}

// memoryEnabledOwner 把 Agent ID 映射为开关键的归属：主助手用全局开关。
func memoryEnabledOwner(agentID string) string {
	if agentID == "" || agentID == mainAgentID {
		return ""
	}
	return agentID
}

// parseMemoryTime 解析模型给出的发生时间；无法解析时返回零值，由调用方回退到当前时间。
func parseMemoryTime(text string) time.Time {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, text, time.Local); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// clampOccurredAt 只接受最近一周到未来一天之间的时间，其余回退到当前时间，
// 避免模型编造久远或未来的日期污染周期摘要。
func clampOccurredAt(occurred, now time.Time) time.Time {
	if occurred.IsZero() {
		return now
	}
	if occurred.Before(now.Add(-7*24*time.Hour)) || occurred.After(now.Add(24*time.Hour)) {
		return now
	}
	return occurred
}

func normalizeMemoryKind(kind string) domain.MemoryKind {
	switch value := domain.MemoryKind(strings.TrimSpace(kind)); value {
	case domain.MemoryKindProfile, domain.MemoryKindPreference, domain.MemoryKindFact,
		domain.MemoryKindDecision, domain.MemoryKindEvent:
		return value
	default:
		return domain.MemoryKindPreference
	}
}

func parseDayStart(day string) (int64, bool) {
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(day), time.Local)
	if err != nil {
		return 0, false
	}
	return parsed.UnixMilli(), true
}

func parseDayEnd(day string) (int64, bool) {
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(day), time.Local)
	if err != nil {
		return 0, false
	}
	return parsed.AddDate(0, 0, 1).UnixMilli() - 1, true
}

// weekPeriodOf 返回给定日期所在自然周的周一与周日。
func weekPeriodOf(day time.Time) weekPeriod {
	local := day.In(time.Local)
	offset := (int(local.Weekday()) + 6) % 7 // 周一为 0
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -offset)
	end := start.AddDate(0, 0, 6)
	return weekPeriod{
		start:   start.Format("2006-01-02"),
		end:     end.Format("2006-01-02"),
		startMs: start.UnixMilli(),
		endMs:   end.AddDate(0, 0, 1).UnixMilli() - 1,
	}
}

// recentWeekPeriods 返回最近 count 个自然周，当前周在最前。
func recentWeekPeriods(now time.Time, count int) []weekPeriod {
	out := make([]weekPeriod, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, weekPeriodOf(now.AddDate(0, 0, -7*i)))
	}
	return out
}

func scoreSearchMemory(item domain.Memory, tokens []string, recentSince int64) float64 {
	score := item.Confidence + float64(item.Importance)*2
	if item.Pinned {
		score += 100
	}
	if memoryMatches(item, tokens) {
		score += 10
	}
	if item.UpdatedAt >= recentSince {
		score += 5
	}
	return score
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
		return false
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
