package store

import (
	"context"
	"strings"
	"time"

	"bowknot-workbench/internal/domain"
)

const taskColumns = `id, title, brief, status, assigner_kind, assigner_id, assignee_id,
	origin_conversation_id, origin_message_id, thread_conversation_id, parent_task_id, group_id,
	attempt, result_summary, result_message_id, error, created_at, started_at, finished_at, reported_at`

// TaskFilter 描述任务查询条件，零值字段表示不过滤。
type TaskFilter struct {
	AssigneeID           string
	Status               domain.TaskStatus
	OriginConversationID string
	// Search 按标题与任务描述模糊匹配。
	Search string
	Limit  int
}

type TaskStore struct {
	s *Store
}

func (t *TaskStore) Create(ctx context.Context, item domain.Task) error {
	if err := t.s.ensureOpen(); err != nil {
		return err
	}

	_, err := t.s.q.ExecContext(ctx,
		`INSERT INTO task(`+taskColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Title, item.Brief, string(item.Status), string(item.AssignerKind),
		item.AssignerID, item.AssigneeID, item.OriginConversationID, item.OriginMessageID,
		item.ThreadConversationID, item.ParentTaskID, item.GroupID, item.Attempt,
		item.ResultSummary, item.ResultMessageID, item.Error, item.CreatedAt,
		item.StartedAt, item.FinishedAt, item.ReportedAt,
	)
	return wrapErr(err)
}

func (t *TaskStore) Get(ctx context.Context, id string) (domain.Task, error) {
	if err := t.s.ensureOpen(); err != nil {
		return domain.Task{}, err
	}

	row := t.s.q.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM task WHERE id = ?`, id)
	item, err := scanTask(row)
	return item, wrapErr(err)
}

func (t *TaskStore) List(ctx context.Context, filter TaskFilter) ([]domain.Task, error) {
	if err := t.s.ensureOpen(); err != nil {
		return nil, err
	}

	query := `SELECT ` + taskColumns + ` FROM task WHERE 1 = 1`
	args := make([]any, 0, 4)
	if id := strings.TrimSpace(filter.AssigneeID); id != "" {
		query += ` AND assignee_id = ?`
		args = append(args, id)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, string(filter.Status))
	}
	if id := strings.TrimSpace(filter.OriginConversationID); id != "" {
		query += ` AND origin_conversation_id = ?`
		args = append(args, id)
	}
	if keyword := strings.TrimSpace(filter.Search); keyword != "" {
		query += ` AND (title LIKE ? OR brief LIKE ?)`
		like := "%" + keyword + "%"
		args = append(args, like, like)
	}
	query += ` ORDER BY created_at DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := t.s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.Task, 0)
	for rows.Next() {
		item, err := scanTask(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

// ListByIDs 批量读取任务，供消息列表关联任务摘要。
func (t *TaskStore) ListByIDs(ctx context.Context, ids []string) ([]domain.Task, error) {
	if err := t.s.ensureOpen(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if len(args) == 0 {
		return nil, nil
	}

	rows, err := t.s.q.QueryContext(ctx,
		`SELECT `+taskColumns+` FROM task WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.Task, 0, len(args))
	for rows.Next() {
		item, err := scanTask(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

// Claim 把排队任务迁移到运行中；返回 false 表示没有抢到迁移权。
func (t *TaskStore) Claim(ctx context.Context, id string, startedAt int64) (bool, error) {
	if err := t.s.ensureOpen(); err != nil {
		return false, err
	}

	result, err := t.s.q.ExecContext(ctx,
		`UPDATE task SET status = ?, started_at = ? WHERE id = ? AND status = ?`,
		string(domain.TaskRunning), startedAt, id, string(domain.TaskQueued),
	)
	return affected(result, err)
}

// Finish 写入终态；只有仍处于排队或运行中的任务能迁移，保证幂等。
func (t *TaskStore) Finish(ctx context.Context, id string, status domain.TaskStatus, resultSummary, resultMessageID, errText string, at int64) (bool, error) {
	if err := t.s.ensureOpen(); err != nil {
		return false, err
	}

	result, err := t.s.q.ExecContext(ctx,
		`UPDATE task SET status = ?, result_summary = ?, result_message_id = ?, error = ?, finished_at = ?
		 WHERE id = ? AND status IN (?, ?)`,
		string(status), resultSummary, resultMessageID, errText, at,
		id, string(domain.TaskQueued), string(domain.TaskRunning),
	)
	return affected(result, err)
}

// Requeue 把终态任务重新放回队列，复用同一线程。
func (t *TaskStore) Requeue(ctx context.Context, id string, at int64) (bool, error) {
	if err := t.s.ensureOpen(); err != nil {
		return false, err
	}

	result, err := t.s.q.ExecContext(ctx,
		`UPDATE task SET status = ?, attempt = attempt + 1, result_summary = '', result_message_id = '',
			error = '', started_at = 0, finished_at = 0, reported_at = 0, created_at = ?
		 WHERE id = ? AND status IN (?, ?, ?, ?)`,
		string(domain.TaskQueued), at,
		id, string(domain.TaskDone), string(domain.TaskFailed),
		string(domain.TaskCanceled), string(domain.TaskTimeout),
	)
	return affected(result, err)
}

// RetryLater 把运行中的任务退回排队，用于自动重试退避。
// 返回 false 表示任务已被其它路径写入终态。
func (t *TaskStore) RetryLater(ctx context.Context, id, errText string) (bool, error) {
	if err := t.s.ensureOpen(); err != nil {
		return false, err
	}

	result, err := t.s.q.ExecContext(ctx,
		`UPDATE task SET status = ?, attempt = attempt + 1, result_summary = '', result_message_id = '',
			error = ?, started_at = 0, finished_at = 0, reported_at = 0
		 WHERE id = ? AND status = ?`,
		string(domain.TaskQueued), errText, id, string(domain.TaskRunning),
	)
	return affected(result, err)
}

func (t *TaskStore) MarkReported(ctx context.Context, id string, at int64) error {
	if err := t.s.ensureOpen(); err != nil {
		return err
	}

	result, err := t.s.q.ExecContext(ctx, `UPDATE task SET reported_at = ? WHERE id = ?`, at, id)
	return ensureAffected(result, err)
}

// ConvergeInterrupted 把上次退出时仍在排队或运行的任务收敛为失败。
func (t *TaskStore) ConvergeInterrupted(ctx context.Context) (int, error) {
	if err := t.s.ensureOpen(); err != nil {
		return 0, err
	}

	result, err := t.s.q.ExecContext(ctx,
		`UPDATE task SET status = ?, error = ?, finished_at = ? WHERE status IN (?, ?)`,
		string(domain.TaskFailed), "应用重启导致任务中断，可在任务列表重试", time.Now().UnixMilli(),
		string(domain.TaskQueued), string(domain.TaskRunning),
	)
	if err != nil {
		return 0, wrapErr(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, wrapErr(err)
	}
	return int(count), nil
}

func scanTask(row rowScanner) (domain.Task, error) {
	var (
		item         domain.Task
		status       string
		assignerKind string
	)
	err := row.Scan(
		&item.ID, &item.Title, &item.Brief, &status, &assignerKind, &item.AssignerID,
		&item.AssigneeID, &item.OriginConversationID, &item.OriginMessageID,
		&item.ThreadConversationID, &item.ParentTaskID, &item.GroupID,
		&item.Attempt, &item.ResultSummary, &item.ResultMessageID, &item.Error,
		&item.CreatedAt, &item.StartedAt, &item.FinishedAt, &item.ReportedAt,
	)
	item.Status = domain.TaskStatus(status)
	item.AssignerKind = domain.AssignerKind(assignerKind)
	return item, err
}

// affected 把执行结果转换成「是否抢到迁移权」，错误原样返回。
func affected(result interface{ RowsAffected() (int64, error) }, err error) (bool, error) {
	if err != nil {
		return false, wrapErr(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, wrapErr(err)
	}
	return rows > 0, nil
}
