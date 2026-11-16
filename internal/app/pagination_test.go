package app

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/store"
)

func appendTestMessages(t *testing.T, st *store.Store, conversationID string, total int) {
	t.Helper()
	ctx := context.Background()
	if err := st.Tx(ctx, func(tx *store.Store) error {
		for i := 0; i < total; i++ {
			if _, err := tx.Messages.Append(ctx, domain.Message{
				ID:             fmt.Sprintf("%s-%d", conversationID, i),
				ConversationID: conversationID,
				Role:           domain.RoleUser,
				Content:        fmt.Sprintf("第 %d 条", i),
				Status:         domain.MessageDone,
				CreatedAt:      int64(i),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("写入消息失败: %v", err)
	}
}

func TestAssistantMessagesPage(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newFastAssistant(st)

	session := svc.CreateSession()
	if session.ID == "" {
		t.Fatal("创建会话失败")
	}

	const total = 120
	appendTestMessages(t, st, session.ID, total)

	page := svc.MessagesPage(session.ID, 0, 50)
	if len(page.Items) != 50 || !page.HasMore {
		t.Fatalf("首屏 = %d 条, hasMore = %v, want 50/true", len(page.Items), page.HasMore)
	}
	if page.Items[0].Seq != total-49 || page.Items[49].Seq != total {
		t.Fatalf("首屏 seq 范围 = [%d, %d], want [%d, %d]",
			page.Items[0].Seq, page.Items[49].Seq, total-49, total)
	}
	if page.Items[49].Content != fmt.Sprintf("第 %d 条", total-1) {
		t.Fatalf("最后一条内容 = %q", page.Items[49].Content)
	}

	older := svc.MessagesPage(session.ID, page.Items[0].Seq, 50)
	if len(older.Items) != 50 || !older.HasMore {
		t.Fatalf("上一页 = %d 条, hasMore = %v, want 50/true", len(older.Items), older.HasMore)
	}
	if older.Items[len(older.Items)-1].Seq != page.Items[0].Seq-1 {
		t.Fatalf("上一页末尾 seq = %d, want %d",
			older.Items[len(older.Items)-1].Seq, page.Items[0].Seq-1)
	}

	rest := svc.MessagesPage(session.ID, older.Items[0].Seq, 50)
	if len(rest.Items) != 20 || rest.HasMore {
		t.Fatalf("最早一页 = %d 条, hasMore = %v, want 20/false", len(rest.Items), rest.HasMore)
	}
	if rest.Items[0].Seq != 1 {
		t.Fatalf("最早一页起始 seq = %d, want 1", rest.Items[0].Seq)
	}
}

func TestAgentMessagesPageResolvesGroupPrefix(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "test.db"), false)
	svc := newFastAgent(st)

	const storeID = "group-g1"
	if err := st.Conversations.Create(context.Background(), domain.Conversation{
		ID:        storeID,
		Kind:      domain.KindGroup,
		OwnerID:   "g1",
		Title:     "群聊",
		CreatedAt: 1,
		UpdatedAt: 2,
	}); err != nil {
		t.Fatalf("创建群聊会话失败: %v", err)
	}

	appendTestMessages(t, st, storeID, 60)

	page := svc.MessagesPage("group:g1", 0, 50)
	if len(page.Items) != 50 || !page.HasMore {
		t.Fatalf("群聊首屏 = %d 条, hasMore = %v, want 50/true", len(page.Items), page.HasMore)
	}
	if page.Items[0].ConversationID != "group:g1" {
		t.Fatalf("会话标识 = %q, want group:g1", page.Items[0].ConversationID)
	}
}
