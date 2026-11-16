package store

import (
	"context"
	"database/sql"
	"time"

	"bowknot-workbench/internal/domain"
)

const runColumns = `id, conversation_id, message_id, status, started_at, finished_at`

type RunStore struct {
	s *Store
}

func (r *RunStore) Start(ctx context.Context, item domain.Run) error {
	if err := r.s.ensureOpen(); err != nil {
		return err
	}
	if item.Status == "" {
		item.Status = domain.RunRunning
	}

	_, err := r.s.q.ExecContext(ctx,
		`INSERT INTO run(`+runColumns+`) VALUES(?, ?, ?, ?, ?, ?)`,
		item.ID, item.ConversationID, item.MessageID, string(item.Status),
		item.StartedAt, item.FinishedAt,
	)
	return wrapErr(err)
}

func (r *RunStore) Get(ctx context.Context, id string) (domain.Run, error) {
	if err := r.s.ensureOpen(); err != nil {
		return domain.Run{}, err
	}

	row := r.s.q.QueryRowContext(ctx, `SELECT `+runColumns+` FROM run WHERE id = ?`, id)
	var (
		item       domain.Run
		status     string
		finishedAt sql.NullInt64
	)
	err := row.Scan(&item.ID, &item.ConversationID, &item.MessageID, &status, &item.StartedAt, &finishedAt)
	item.Status = domain.RunStatus(status)
	if finishedAt.Valid {
		item.FinishedAt = finishedAt.Int64
	}
	return item, wrapErr(err)
}

func (r *RunStore) Finish(ctx context.Context, id string, status domain.RunStatus, finishedAt int64) error {
	if err := r.s.ensureOpen(); err != nil {
		return err
	}

	result, err := r.s.q.ExecContext(ctx,
		`UPDATE run SET status = ?, finished_at = ? WHERE id = ?`,
		string(status), finishedAt, id,
	)
	return ensureAffected(result, err)
}

// MarkInterrupted 把上次退出时仍在运行的生成任务与消息收敛为 stopped。
func (r *RunStore) MarkInterrupted(ctx context.Context) (int, error) {
	if r.s.inTx {
		return r.markInterrupted(ctx)
	}

	var count int
	err := r.s.Tx(ctx, func(tx *Store) error {
		var err error
		count, err = tx.Runs.markInterrupted(ctx)
		return err
	})
	return count, err
}

func (r *RunStore) markInterrupted(ctx context.Context) (int, error) {
	if err := r.s.ensureOpen(); err != nil {
		return 0, err
	}

	now := time.Now().UnixMilli()
	result, err := r.s.q.ExecContext(ctx,
		`UPDATE run SET status = ?, finished_at = ? WHERE status = ?`,
		string(domain.RunStopped), now, string(domain.RunRunning),
	)
	if err != nil {
		return 0, wrapErr(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, wrapErr(err)
	}

	if _, err := r.s.q.ExecContext(ctx,
		`UPDATE message SET status = ? WHERE status = ?`,
		string(domain.MessageStopped), string(domain.MessageStreaming),
	); err != nil {
		return int(count), wrapErr(err)
	}
	return int(count), nil
}
