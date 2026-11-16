package store

import (
	"context"

	"bowknot-workbench/internal/domain"
)

const messageColumns = `id, conversation_id, seq, role, content, status, error, sender_name, sender_avatar, emotion, memory_written, created_at`

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
		`INSERT INTO message(`+messageColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.ConversationID, item.Seq, string(item.Role), item.Content,
		string(item.Status), item.Error, item.SenderName, item.SenderAvatar,
		item.Emotion, boolInt(item.MemoryWritten), item.CreatedAt,
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
		`UPDATE message SET content = ?, status = ?, error = ?, sender_name = ?, sender_avatar = ?, emotion = ?, memory_written = ? WHERE id = ?`,
		item.Content, string(item.Status), item.Error, item.SenderName,
		item.SenderAvatar, item.Emotion, boolInt(item.MemoryWritten), item.ID,
	)
	return ensureAffected(result, err)
}

func (m *MessageStore) Delete(ctx context.Context, id string) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
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
		&item.ID, &item.ConversationID, &item.Seq, &role, &item.Content,
		&status, &item.Error, &item.SenderName, &item.SenderAvatar,
		&item.Emotion, &memoryWrite, &item.CreatedAt,
	)
	item.Role = domain.Role(role)
	item.Status = domain.MessageStatus(status)
	item.MemoryWritten = memoryWrite != 0
	return item, err
}
