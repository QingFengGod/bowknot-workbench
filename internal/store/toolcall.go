package store

import (
	"context"

	"bowknot-workbench/internal/domain"
)

const toolCallColumns = `id, conversation_id, message_id, run_id, tool_id, agent_id, arguments, result, status, error, created_at, finished_at`

type ToolCallStore struct {
	s *Store
}

// Insert 写入一条工具调用记录，初始状态通常为 running。
func (t *ToolCallStore) Insert(ctx context.Context, item domain.ToolCall) error {
	if err := t.s.ensureOpen(); err != nil {
		return err
	}

	_, err := t.s.q.ExecContext(ctx,
		`INSERT INTO tool_call(`+toolCallColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.ConversationID, item.MessageID, item.RunID, item.ToolID,
		item.AgentID, item.Arguments, item.Result, string(item.Status), item.Error,
		item.CreatedAt, item.FinishedAt,
	)
	return wrapErr(err)
}

// Finish 写入执行结果与最终状态。
func (t *ToolCallStore) Finish(ctx context.Context, id, result string, status domain.ToolCallStatus, errText string, finishedAt int64) error {
	if err := t.s.ensureOpen(); err != nil {
		return err
	}

	rows, err := t.s.q.ExecContext(ctx,
		`UPDATE tool_call SET result = ?, status = ?, error = ?, finished_at = ? WHERE id = ?`,
		result, string(status), errText, finishedAt, id,
	)
	return ensureAffected(rows, err)
}

// ListByConversation 按时间顺序返回一个会话内的全部工具调用。
func (t *ToolCallStore) ListByConversation(ctx context.Context, conversationID string) ([]domain.ToolCall, error) {
	if err := t.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := t.s.q.QueryContext(ctx,
		`SELECT `+toolCallColumns+` FROM tool_call WHERE conversation_id = ? ORDER BY created_at ASC, rowid ASC`,
		conversationID,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.ToolCall, 0)
	for rows.Next() {
		item, err := scanToolCall(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

// ListByMessage 返回一条消息触发的全部工具调用。
func (t *ToolCallStore) ListByMessage(ctx context.Context, messageID string) ([]domain.ToolCall, error) {
	if err := t.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := t.s.q.QueryContext(ctx,
		`SELECT `+toolCallColumns+` FROM tool_call WHERE message_id = ? ORDER BY created_at ASC, rowid ASC`,
		messageID,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.ToolCall, 0)
	for rows.Next() {
		item, err := scanToolCall(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

func scanToolCall(row rowScanner) (domain.ToolCall, error) {
	var (
		item   domain.ToolCall
		status string
	)
	err := row.Scan(
		&item.ID, &item.ConversationID, &item.MessageID, &item.RunID, &item.ToolID,
		&item.AgentID, &item.Arguments, &item.Result, &status, &item.Error,
		&item.CreatedAt, &item.FinishedAt,
	)
	item.Status = domain.ToolCallStatus(status)
	return item, err
}
