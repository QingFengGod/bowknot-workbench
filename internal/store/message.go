package store

import (
	"context"

	"bowknot-workbench/internal/domain"
)

const messageColumns = `id, conversation_id, seq, role, content, reasoning, status, error, sender_name, sender_avatar, emotion, memory_written, created_at, sender_kind, sender_id, msg_type, task_id, visibility`

type MessageStore struct {
	s *Store
}

func (m *MessageStore) Get(ctx context.Context, id string) (domain.Message, error) {
	if err := m.s.ensureOpen(); err != nil {
		return domain.Message{}, err
	}

	row := m.s.q.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM message WHERE id = ?`, id)
	item, err := scanMessage(row)
	return item, wrapErr(err)
}

func (m *MessageStore) List(ctx context.Context, conversationID string) ([]domain.Message, error) {
	if err := m.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := m.s.q.QueryContext(ctx,
		`SELECT `+messageColumns+` FROM message WHERE conversation_id = ? ORDER BY seq ASC`,
		conversationID,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.Message, 0)
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

// ListPage 按 seq 取一页消息：beforeSeq <= 0 表示从最新开始，
// 返回结果按 seq 升序排列，hasMore 表示还有更早的消息。
func (m *MessageStore) ListPage(ctx context.Context, conversationID string, beforeSeq int64, limit int) ([]domain.Message, bool, error) {
	if err := m.s.ensureOpen(); err != nil {
		return nil, false, err
	}
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT ` + messageColumns + ` FROM message WHERE conversation_id = ?`
	args := []any{conversationID}
	if beforeSeq > 0 {
		query += ` AND seq < ?`
		args = append(args, beforeSeq)
	}
	// 多取一条用于判断是否还有更早的消息。
	query += ` ORDER BY seq DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := m.s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, wrapErr(err)
	}
	defer rows.Close()

	desc := make([]domain.Message, 0, limit+1)
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, false, wrapErr(err)
		}
		desc = append(desc, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, wrapErr(err)
	}

	hasMore := len(desc) > limit
	if hasMore {
		desc = desc[:limit]
	}

	// 反转为升序，直接用于渲染。
	out := make([]domain.Message, len(desc))
	for i, item := range desc {
		out[len(desc)-1-i] = item
	}
	return out, hasMore, nil
}

// Append 在事务内分配 seq 并写入消息；返回带 seq 的副本。
func (m *MessageStore) Append(ctx context.Context, item domain.Message) (domain.Message, error) {
	if m.s.inTx {
		return m.append(ctx, item)
	}

	var out domain.Message
	err := m.s.Tx(ctx, func(tx *Store) error {
		var err error
		out, err = tx.Messages.append(ctx, item)
		return err
	})
	return out, err
}

func (m *MessageStore) append(ctx context.Context, item domain.Message) (domain.Message, error) {
	if err := m.s.ensureOpen(); err != nil {
		return domain.Message{}, err
	}

	var seq int64
	if err := m.s.q.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM message WHERE conversation_id = ?`,
		item.ConversationID,
	).Scan(&seq); err != nil {
		return domain.Message{}, wrapErr(err)
	}
	item.Seq = seq

	_, err := m.s.q.ExecContext(ctx,
		`INSERT INTO message(`+messageColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.ConversationID, item.Seq, string(item.Role), item.Content, item.Reasoning,
		string(item.Status), item.Error, item.SenderName, item.SenderAvatar,
		item.Emotion, boolInt(item.MemoryWritten), item.CreatedAt,
		item.SenderKind, item.SenderID, item.MessageType, item.TaskID, item.Visibility,
	)
	if err != nil {
		return domain.Message{}, wrapErr(err)
	}
	return item, nil
}

func (m *MessageStore) Update(ctx context.Context, item domain.Message) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	result, err := m.s.q.ExecContext(ctx,
		`UPDATE message SET content = ?, reasoning = ?, status = ?, error = ?, sender_name = ?, sender_avatar = ?, emotion = ?, memory_written = ? WHERE id = ?`,
		item.Content, item.Reasoning, string(item.Status), item.Error, item.SenderName,
		item.SenderAvatar, item.Emotion, boolInt(item.MemoryWritten), item.ID,
	)
	return ensureAffected(result, err)
}

// SetMemoryWritten 只更新记忆标记，避免覆盖流式写入的正文与状态。
func (m *MessageStore) SetMemoryWritten(ctx context.Context, id string, written bool) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	result, err := m.s.q.ExecContext(ctx,
		`UPDATE message SET memory_written = ? WHERE id = ?`,
		boolInt(written), id,
	)
	return ensureAffected(result, err)
}

func (m *MessageStore) Delete(ctx context.Context, id string) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}
	// tool_call 没有外键，删除消息时同步清理其调用记录。
	if _, err := m.s.q.ExecContext(ctx, `DELETE FROM tool_call WHERE message_id = ?`, id); err != nil {
		return wrapErr(err)
	}

	result, err := m.s.q.ExecContext(ctx, `DELETE FROM message WHERE id = ?`, id)
	return ensureAffected(result, err)
}

func scanMessage(row rowScanner) (domain.Message, error) {
	var (
		item        domain.Message
		role        string
		status      string
		memoryWrite int
	)
	err := row.Scan(
		&item.ID, &item.ConversationID, &item.Seq, &role, &item.Content, &item.Reasoning,
		&status, &item.Error, &item.SenderName, &item.SenderAvatar,
		&item.Emotion, &memoryWrite, &item.CreatedAt,
		&item.SenderKind, &item.SenderID, &item.MessageType, &item.TaskID, &item.Visibility,
	)
	item.Role = domain.Role(role)
	item.Status = domain.MessageStatus(status)
	item.MemoryWritten = memoryWrite != 0
	return item, err
}
