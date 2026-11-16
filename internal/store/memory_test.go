package store

import (
	"context"
	"errors"
	"testing"

	"bowknot-workbench/internal/domain"
)

func memoryItem(id, scope, ownerID, kind, content, hash, dayKey string) domain.Memory {
	return domain.Memory{
		ID:          id,
		Scope:       domain.MemoryScope(scope),
		OwnerID:     ownerID,
		Kind:        domain.MemoryKind(kind),
		Content:     content,
		ContentHash: hash,
		Keywords:    "中文 回复",
		OccurredAt:  1000,
		DayKey:      dayKey,
		SourceRole:  "user",
		Confidence:  0.9,
		Status:      domain.MemoryActive,
		CreatedAt:   1000,
		UpdatedAt:   1000,
		LastUsedAt:  0,
	}
}

func TestMemoryScopeIsolationAndDayQuery(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Memories.Insert(ctx, memoryItem("m-user", "user", "main", "preference", "偏好中文回复", "h1", "2026-11-16")); err != nil {
		t.Fatalf("insert user memory: %v", err)
	}
	if err := s.Memories.Insert(ctx, memoryItem("m-agent", "agent", "agent-1", "fact", "用户负责支付模块", "h2", "2026-11-15")); err != nil {
		t.Fatalf("insert agent memory: %v", err)
	}

	userItems, err := s.Memories.ListActive(ctx, domain.MemoryScopeUser, "main", 10)
	if err != nil {
		t.Fatalf("list user memories: %v", err)
	}
	if len(userItems) != 1 || userItems[0].ID != "m-user" {
		t.Fatalf("用户记忆 = %+v", userItems)
	}

	agentItems, err := s.Memories.ListActive(ctx, domain.MemoryScopeAgent, "agent-1", 10)
	if err != nil {
		t.Fatalf("list agent memories: %v", err)
	}
	if len(agentItems) != 1 || agentItems[0].ID != "m-agent" {
		t.Fatalf("Agent 记忆 = %+v", agentItems)
	}

	byDay, err := s.Memories.ListByDay(ctx, domain.MemoryScopeUser, "main", "2026-11-16")
	if err != nil {
		t.Fatalf("list by day: %v", err)
	}
	if len(byDay) != 1 || byDay[0].ID != "m-user" {
		t.Fatalf("按日查询 = %+v", byDay)
	}

	days, err := s.Memories.ListDays(ctx, domain.MemoryScopeUser, "main", 10)
	if err != nil {
		t.Fatalf("list days: %v", err)
	}
	if len(days) != 1 || days[0] != "2026-11-16" {
		t.Fatalf("日期列表 = %+v", days)
	}
}

func TestMemoryDedupeAndSupersede(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	first := memoryItem("m1", "user", "main", "preference", "偏好中文回复", "hash-1", "2026-11-16")
	if err := s.Memories.Insert(ctx, first); err != nil {
		t.Fatalf("insert: %v", err)
	}

	found, err := s.Memories.GetActiveByHash(ctx, domain.MemoryScopeUser, "main", domain.MemoryKindPreference, "hash-1")
	if err != nil {
		t.Fatalf("get by hash: %v", err)
	}
	if found.ID != "m1" {
		t.Fatalf("按哈希查询 = %+v", found)
	}

	duplicate := memoryItem("m2", "user", "main", "preference", "偏好中文回复", "hash-1", "2026-11-16")
	if err := s.Memories.Insert(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("重复写入错误 = %v, want ErrConflict", err)
	}

	if err := s.Memories.Supersede(ctx, "m1", 2000); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if _, err := s.Memories.GetActiveByHash(ctx, domain.MemoryScopeUser, "main", domain.MemoryKindPreference, "hash-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("被替代后仍可查到 active：%v", err)
	}
	if err := s.Memories.Insert(ctx, memoryItem("m3", "user", "main", "preference", "偏好中文回复", "hash-1", "2026-11-16")); err != nil {
		t.Fatalf("替代后重新写入失败：%v", err)
	}

	old, err := s.Memories.Get(ctx, "m1")
	if err != nil {
		t.Fatalf("get old: %v", err)
	}
	if old.Status != domain.MemorySuperseded {
		t.Fatalf("旧记忆状态 = %s", old.Status)
	}
}

func TestMemoryUpdatePinDeleteAndTouch(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	item := memoryItem("m1", "user", "main", "preference", "偏好中文回复", "hash-1", "2026-11-16")
	if err := s.Memories.Insert(ctx, item); err != nil {
		t.Fatalf("insert: %v", err)
	}

	item.Content = "偏好英文回复"
	item.ContentHash = "hash-2"
	item.UpdatedAt = 3000
	if err := s.Memories.Update(ctx, item); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, err := s.Memories.Get(ctx, "m1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if updated.Content != "偏好英文回复" || updated.ContentHash != "hash-2" {
		t.Fatalf("更新后 = %+v", updated)
	}

	if err := s.Memories.SetPinned(ctx, "m1", true, 4000); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := s.Memories.MarkUsed(ctx, []string{"m1"}, 5000); err != nil {
		t.Fatalf("mark used: %v", err)
	}
	after, err := s.Memories.Get(ctx, "m1")
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !after.Pinned || after.LastUsedAt != 5000 {
		t.Fatalf("置顶/使用时间 = %+v", after)
	}

	if err := s.Memories.Delete(ctx, "m1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Memories.Get(ctx, "m1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后仍可读取：%v", err)
	}
}

func TestMemoryDigestUpsertItemsAndDirty(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for _, item := range []domain.Memory{
		memoryItem("m1", "user", "main", "preference", "偏好中文回复", "h1", "2026-11-16"),
		memoryItem("m2", "user", "main", "fact", "用户在杭州", "h2", "2026-11-16"),
	} {
		if err := s.Memories.Insert(ctx, item); err != nil {
			t.Fatalf("insert %s: %v", item.ID, err)
		}
	}

	digest := domain.MemoryDigest{
		ID:            "d1",
		Scope:         domain.MemoryScopeUser,
		OwnerID:       "main",
		Level:         domain.DigestDay,
		PeriodStart:   "2026-11-16",
		PeriodEnd:     "2026-11-16",
		Summary:       "用户确认了回复语言与所在地",
		Keywords:      "中文 杭州",
		MemoryCount:   2,
		SourceVersion: "v1",
		CreatedAt:     1000,
		UpdatedAt:     1000,
	}
	if err := s.MemoryDigests.Upsert(ctx, digest, []string{"m1", "m2"}); err != nil {
		t.Fatalf("upsert digest: %v", err)
	}

	got, err := s.MemoryDigests.Get(ctx, domain.MemoryScopeUser, "main", domain.DigestDay, "2026-11-16")
	if err != nil {
		t.Fatalf("get digest: %v", err)
	}
	if got.ID != "d1" || got.MemoryCount != 2 {
		t.Fatalf("摘要 = %+v", got)
	}

	items, err := s.MemoryDigests.Items(ctx, got.ID)
	if err != nil {
		t.Fatalf("digest items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("摘要关联记忆数量 = %d, want 2", len(items))
	}

	digest.Summary = "更新后的摘要"
	digest.SourceVersion = "v2"
	digest.UpdatedAt = 2000
	if err := s.MemoryDigests.Upsert(ctx, digest, []string{"m1"}); err != nil {
		t.Fatalf("re-upsert digest: %v", err)
	}
	got, err = s.MemoryDigests.Get(ctx, domain.MemoryScopeUser, "main", domain.DigestDay, "2026-11-16")
	if err != nil {
		t.Fatalf("get digest again: %v", err)
	}
	if got.ID != "d1" || got.Summary != "更新后的摘要" || got.SourceVersion != "v2" {
		t.Fatalf("覆盖后摘要 = %+v", got)
	}
	items, err = s.MemoryDigests.Items(ctx, got.ID)
	if err != nil {
		t.Fatalf("digest items after upsert: %v", err)
	}
	if len(items) != 1 || items[0].ID != "m1" {
		t.Fatalf("覆盖后关联记忆 = %+v", items)
	}

	if err := s.MemoryDigests.MarkDirty(ctx, domain.MemoryScopeUser, "main", domain.DigestDay, "2026-11-16"); err != nil {
		t.Fatalf("mark dirty: %v", err)
	}
	got, err = s.MemoryDigests.Get(ctx, domain.MemoryScopeUser, "main", domain.DigestDay, "2026-11-16")
	if err != nil {
		t.Fatalf("get dirty digest: %v", err)
	}
	if !got.Dirty {
		t.Fatal("摘要未标记为 dirty")
	}
}
