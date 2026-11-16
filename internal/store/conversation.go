package store

import (
	"context"

	"bowknot-workbench/internal/domain"
)

const conversationColumns = `id, kind, owner_id, title, summary, pinned, created_at, updated_at`

type ConversationStore struct {
	s *Store
}

func (c *ConversationStore) List(ctx context.Context, kind domain.ConversationKind, ownerID string) ([]domain.Conversation, error) {
	if err := c.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := c.s.q.QueryContext(ctx,
		`SELECT `+conversationColumns+` FROM conversation WHERE kind = ? AND owner_id = ? ORDER BY pinned DESC, updated_at DESC`,
		string(kind), ownerID,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.Conversation, 0)
	for rows.Next() {
		item, err := scanConversation(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

func (c *ConversationStore) Get(ctx context.Context, id string) (domain.Conversation, error) {
	if err := c.s.ensureOpen(); err != nil {
		return domain.Conversation{}, err
	}

	row := c.s.q.QueryRowContext(ctx,
		`SELECT `+conversationColumns+` FROM conversation WHERE id = ?`, id,
	)
	item, err := scanConversation(row)
	return item, wrapErr(err)
}

func (c *ConversationStore) Create(ctx context.Context, item domain.Conversation) error {
	if err := c.s.ensureOpen(); err != nil {
		return err
	}

	_, err := c.s.q.ExecContext(ctx,
		`INSERT INTO conversation(`+conversationColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, string(item.Kind), item.OwnerID, item.Title, item.Summary,
		boolInt(item.Pinned), item.CreatedAt, item.UpdatedAt,
	)
	return wrapErr(err)
}

func (c *ConversationStore) UpdateMeta(ctx context.Context, id, title, summary string, pinned bool, updatedAt int64) error {
	if err := c.s.ensureOpen(); err != nil {
		return err
	}

	result, err := c.s.q.ExecContext(ctx,
		`UPDATE conversation SET title = ?, summary = ?, pinned = ?, updated_at = ? WHERE id = ?`,
		title, summary, boolInt(pinned), updatedAt, id,
	)
	return ensureAffected(result, err)
}

func (c *ConversationStore) Touch(ctx context.Context, id string, updatedAt int64) error {
	if err := c.s.ensureOpen(); err != nil {
		return err
	}

	result, err := c.s.q.ExecContext(ctx,
		`UPDATE conversation SET updated_at = ? WHERE id = ?`, updatedAt, id,
	)
	return ensureAffected(result, err)
}

func (c *ConversationStore) Delete(ctx context.Context, id string) error {
	if err := c.s.ensureOpen(); err != nil {
		return err
	}

	result, err := c.s.q.ExecContext(ctx, `DELETE FROM conversation WHERE id = ?`, id)
	return ensureAffected(result, err)
}

func scanConversation(row rowScanner) (domain.Conversation, error) {
	var (
		item   domain.Conversation
		kind   string
		pinned int
	)
	err := row.Scan(
		&item.ID, &kind, &item.OwnerID, &item.Title, &item.Summary,
		&pinned, &item.CreatedAt, &item.UpdatedAt,
	)
	item.Kind = domain.ConversationKind(kind)
	item.Pinned = pinned != 0
	return item, err
}
