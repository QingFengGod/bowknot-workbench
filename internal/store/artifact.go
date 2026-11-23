package store

import (
	"context"
	"strings"

	"bowknot-workbench/internal/domain"
)

const artifactColumns = `id, conversation_id, task_id, message_id, title, kind, mime, filename,
	relative_path, uri, size, summary, created_at, source_path`

// ArtifactFilter 描述产物查询条件，零值字段表示不过滤。
type ArtifactFilter struct {
	Kind  domain.ArtifactKind
	Kinds []domain.ArtifactKind
	// ConversationID 按会话过滤。
	ConversationID string
	TaskID         string
	// OwnerID 按会话归属过滤（主助手或员工）。
	OwnerID string
	// Search 按标题模糊匹配。
	Search string
	Limit  int
}

type ArtifactStore struct {
	s *Store
}

func (a *ArtifactStore) Create(ctx context.Context, item domain.Artifact) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	_, err := a.s.q.ExecContext(ctx,
		`INSERT INTO artifact(`+artifactColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.ConversationID, item.TaskID, item.MessageID, item.Title, string(item.Kind),
		item.MIME, item.Filename, item.RelativePath, item.URI, item.Size, item.Summary, item.CreatedAt,
		item.SourcePath,
	)
	return wrapErr(err)
}

func (a *ArtifactStore) Get(ctx context.Context, id string) (domain.Artifact, error) {
	if err := a.s.ensureOpen(); err != nil {
		return domain.Artifact{}, err
	}

	row := a.s.q.QueryRowContext(ctx, `SELECT `+artifactColumns+` FROM artifact WHERE id = ?`, id)
	item, err := scanArtifact(row)
	return item, wrapErr(err)
}

func (a *ArtifactStore) List(ctx context.Context, filter ArtifactFilter) ([]domain.Artifact, error) {
	if err := a.s.ensureOpen(); err != nil {
		return nil, err
	}

	query := `SELECT ` + artifactColumns + ` FROM artifact WHERE 1 = 1`
	args := make([]any, 0, 8)
	if len(filter.Kinds) > 0 {
		placeholders := make([]string, 0, len(filter.Kinds))
		for _, kind := range filter.Kinds {
			placeholders = append(placeholders, "?")
			args = append(args, string(kind))
		}
		query += ` AND kind IN (` + strings.Join(placeholders, ",") + `)`
	} else if filter.Kind != "" {
		query += ` AND kind = ?`
		args = append(args, string(filter.Kind))
	}
	if id := strings.TrimSpace(filter.ConversationID); id != "" {
		query += ` AND conversation_id = ?`
		args = append(args, id)
	}
	if id := strings.TrimSpace(filter.TaskID); id != "" {
		query += ` AND task_id = ?`
		args = append(args, id)
	}
	if id := strings.TrimSpace(filter.OwnerID); id != "" {
		query += ` AND conversation_id IN (SELECT id FROM conversation WHERE owner_id = ?)`
		args = append(args, id)
	}
	if keyword := strings.TrimSpace(filter.Search); keyword != "" {
		query += ` AND title LIKE ?`
		args = append(args, "%"+keyword+"%")
	}
	query += ` ORDER BY created_at DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := a.s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.Artifact, 0)
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

// ListByMessageIDs 批量读取消息关联的产物。
func (a *ArtifactStore) ListByMessageIDs(ctx context.Context, ids []string) ([]domain.Artifact, error) {
	return a.listByColumn(ctx, "message_id", ids)
}

// ListByTaskIDs 批量读取任务关联的产物，用于来源会话的任务卡片。
func (a *ArtifactStore) ListByTaskIDs(ctx context.Context, ids []string) ([]domain.Artifact, error) {
	return a.listByColumn(ctx, "task_id", ids)
}

func (a *ArtifactStore) listByColumn(ctx context.Context, column string, ids []string) ([]domain.Artifact, error) {
	if err := a.s.ensureOpen(); err != nil {
		return nil, err
	}

	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if len(args) == 0 {
		return nil, nil
	}

	rows, err := a.s.q.QueryContext(ctx,
		`SELECT `+artifactColumns+` FROM artifact WHERE `+column+` IN (`+strings.Join(placeholders, ",")+`) ORDER BY created_at ASC`,
		args...)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.Artifact, 0, len(args))
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

func (a *ArtifactStore) Delete(ctx context.Context, id string) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	result, err := a.s.q.ExecContext(ctx, `DELETE FROM artifact WHERE id = ?`, id)
	return ensureAffected(result, err)
}

func scanArtifact(row rowScanner) (domain.Artifact, error) {
	var (
		item domain.Artifact
		kind string
	)
	err := row.Scan(
		&item.ID, &item.ConversationID, &item.TaskID, &item.MessageID, &item.Title, &kind,
		&item.MIME, &item.Filename, &item.RelativePath, &item.URI, &item.Size, &item.Summary, &item.CreatedAt,
		&item.SourcePath,
	)
	item.Kind = domain.ArtifactKind(kind)
	return item, err
}
