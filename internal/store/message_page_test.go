package store

import (
	"context"
	"fmt"
	"testing"

	"bowknot-workbench/internal/domain"
)

func TestMessageReasoningRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Conversations.Create(ctx, conversation("c1", 10)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	item := message("m1", "c1", domain.RoleAssistant)
	item.Content = "最终回答"
	item.Reasoning = "先读一下上下文"
	created, err := s.Messages.Append(ctx, item)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	got, err := s.Messages.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Reasoning != "先读一下上下文" {
		t.Fatalf("Reasoning = %q, want %q", got.Reasoning, "先读一下上下文")
	}

	got.Reasoning = "更新后的思考"
	if err := s.Messages.Update(ctx, got); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	again, err := s.Messages.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after Update error = %v", err)
	}
	if again.Reasoning != "更新后的思考" {
		t.Fatalf("Reasoning after Update = %q, want %q", again.Reasoning, "更新后的思考")
	}
}

func TestMessageListPage(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Conversations.Create(ctx, conversation("c1", 10)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	const total = 10000
	if err := s.Tx(ctx, func(tx *Store) error {
		for i := 0; i < total; i++ {
			if _, err := tx.Messages.Append(ctx, message(fmt.Sprintf("m%d", i), "c1", domain.RoleUser)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("批量写入消息失败: %v", err)
	}

	page, hasMore, err := s.Messages.ListPage(ctx, "c1", 0, 50)
	if err != nil {
		t.Fatalf("ListPage() error = %v", err)
	}
	if len(page) != 50 || !hasMore {
		t.Fatalf("首屏 = %d 条, hasMore = %v, want 50/true", len(page), hasMore)
	}
	if page[0].Seq != total-49 || page[49].Seq != total {
		t.Fatalf("首屏 seq 范围 = [%d, %d], want [%d, %d]", page[0].Seq, page[49].Seq, total-49, total)
	}
	for i := 1; i < len(page); i++ {
		if page[i].Seq != page[i-1].Seq+1 {
			t.Fatalf("首屏未按 seq 升序: %d -> %d", page[i-1].Seq, page[i].Seq)
		}
	}

	older, hasMore, err := s.Messages.ListPage(ctx, "c1", page[0].Seq, 50)
	if err != nil {
		t.Fatalf("ListPage() error = %v", err)
	}
	if len(older) != 50 || !hasMore {
		t.Fatalf("上一页 = %d 条, hasMore = %v, want 50/true", len(older), hasMore)
	}
	if older[len(older)-1].Seq != page[0].Seq-1 {
		t.Fatalf("上一页末尾 seq = %d, want %d", older[len(older)-1].Seq, page[0].Seq-1)
	}

	// 从最新一直往前翻，应当恰好取回全部消息。
	cursor := int64(0)
	collected := 0
	for {
		items, more, err := s.Messages.ListPage(ctx, "c1", cursor, 500)
		if err != nil {
			t.Fatalf("翻页 ListPage() error = %v", err)
		}
		collected += len(items)
		if len(items) > 0 {
			cursor = items[0].Seq
		}
		if !more {
			break
		}
	}
	if collected != total {
		t.Fatalf("翻页收集 = %d 条, want %d", collected, total)
	}

	// beforeSeq 指向第一条消息时没有更早的数据。
	items, more, err := s.Messages.ListPage(ctx, "c1", 1, 50)
	if err != nil {
		t.Fatalf("ListPage() error = %v", err)
	}
	if len(items) != 0 || more {
		t.Fatalf("边界结果 = %d 条, hasMore = %v, want 0/false", len(items), more)
	}

	// limit <= 0 使用默认页大小。
	items, _, err = s.Messages.ListPage(ctx, "c1", 0, 0)
	if err != nil {
		t.Fatalf("ListPage() error = %v", err)
	}
	if len(items) != 50 {
		t.Fatalf("默认页大小 = %d, want 50", len(items))
	}
}
