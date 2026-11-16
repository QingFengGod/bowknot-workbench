package store

import (
	"context"
	"database/sql"
	"errors"

	"bowknot-workbench/internal/domain"
)

const memoryDigestColumns = `id, scope, owner_id, level, period_start, period_end, summary, keywords, memory_count, source_version, dirty, created_at, updated_at`

// MemoryDigestStore 管理日/周/月摘要及其关联的原子记忆。
type MemoryDigestStore struct {
	s *Store
}

// Upsert 写入或覆盖一个周期的摘要，并重建摘要与记忆的关联。
func (d *MemoryDigestStore) Upsert(ctx context.Context, item domain.MemoryDigest, memoryIDs []string) error {
	if d.s.inTx {
		return d.upsert(ctx, item, memoryIDs)
	}
	return d.s.Tx(ctx, func(tx *Store) error {
		return tx.MemoryDigests.upsert(ctx, item, memoryIDs)
	})
}

func (d *MemoryDigestStore) upsert(ctx context.Context, item domain.MemoryDigest, memoryIDs []string) error {
	if err := d.s.ensureOpen(); err != nil {
		return err
	}

	var existingID string
	err := d.s.q.QueryRowContext(ctx,
		`SELECT id FROM memory_digest WHERE scope = ? AND owner_id = ? AND level = ? AND period_start = ?`,
		string(item.Scope), item.OwnerID, string(item.Level), item.PeriodStart,
	).Scan(&existingID)
	switch {
	case err == nil:
		item.ID = existingID
		_, err = d.s.q.ExecContext(ctx,
			`UPDATE memory_digest SET period_end = ?, summary = ?, keywords = ?, memory_count = ?, source_version = ?, dirty = 0, updated_at = ?
			 WHERE id = ?`,
			item.PeriodEnd, item.Summary, item.Keywords, item.MemoryCount, item.SourceVersion, item.UpdatedAt, item.ID,
		)
	case errors.Is(err, sql.ErrNoRows):
		_, err = d.s.q.ExecContext(ctx,
			`INSERT INTO memory_digest(`+memoryDigestColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.ID, string(item.Scope), item.OwnerID, string(item.Level), item.PeriodStart,
			item.PeriodEnd, item.Summary, item.Keywords, item.MemoryCount, item.SourceVersion,
			boolInt(item.Dirty), item.CreatedAt, item.UpdatedAt,
		)
	default:
		return wrapErr(err)
	}
	if err != nil {
		return wrapErr(err)
	}

	if _, err := d.s.q.ExecContext(ctx, `DELETE FROM memory_digest_item WHERE digest_id = ?`, item.ID); err != nil {
		return wrapErr(err)
	}
	for _, memoryID := range uniqueStrings(memoryIDs) {
		if _, err := d.s.q.ExecContext(ctx,
			`INSERT INTO memory_digest_item(digest_id, memory_id) VALUES(?, ?)`,
			item.ID, memoryID,
		); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

func (d *MemoryDigestStore) Get(ctx context.Context, scope domain.MemoryScope, ownerID string, level domain.DigestLevel, periodStart string) (domain.MemoryDigest, error) {
	if err := d.s.ensureOpen(); err != nil {
		return domain.MemoryDigest{}, err
	}

	row := d.s.q.QueryRowContext(ctx,
		`SELECT `+memoryDigestColumns+` FROM memory_digest
		 WHERE scope = ? AND owner_id = ? AND level = ? AND period_start = ?`,
		string(scope), ownerID, string(level), periodStart,
	)
	item, err := scanDigest(row)
	return item, wrapErr(err)
}

// List 按周期倒序返回摘要；limit <= 0 表示不限制。
func (d *MemoryDigestStore) List(ctx context.Context, scope domain.MemoryScope, ownerID string, level domain.DigestLevel, limit int) ([]domain.MemoryDigest, error) {
	if err := d.s.ensureOpen(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = -1
	}

	rows, err := d.s.q.QueryContext(ctx,
		`SELECT `+memoryDigestColumns+` FROM memory_digest
		 WHERE scope = ? AND owner_id = ? AND level = ?
		 ORDER BY period_start DESC LIMIT ?`,
		string(scope), ownerID, string(level), limit,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.MemoryDigest, 0)
	for rows.Next() {
		item, err := scanDigest(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

// Items 回查摘要关联的原子记忆，供「按需展开」使用。
func (d *MemoryDigestStore) Items(ctx context.Context, digestID string) ([]domain.Memory, error) {
	if err := d.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := d.s.q.QueryContext(ctx,
		`SELECT `+prefixColumns("m", memoryColumns)+` FROM memory m
		 JOIN memory_digest_item i ON i.memory_id = m.id
		 WHERE i.digest_id = ? AND m.status = 'active'
		 ORDER BY m.occurred_at ASC, m.rowid ASC`,
		digestID,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

// MarkDirty 标记某周期摘要过期；摘要不存在时忽略。
func (d *MemoryDigestStore) MarkDirty(ctx context.Context, scope domain.MemoryScope, ownerID string, level domain.DigestLevel, periodStart string) error {
	if err := d.s.ensureOpen(); err != nil {
		return err
	}

	_, err := d.s.q.ExecContext(ctx,
		`UPDATE memory_digest SET dirty = 1 WHERE scope = ? AND owner_id = ? AND level = ? AND period_start = ?`,
		string(scope), ownerID, string(level), periodStart,
	)
	return wrapErr(err)
}

// DeleteAll 清空全部摘要，用于清空记忆后的重置。
func (d *MemoryDigestStore) DeleteAll(ctx context.Context) error {
	if err := d.s.ensureOpen(); err != nil {
		return err
	}

	_, err := d.s.q.ExecContext(ctx, `DELETE FROM memory_digest`)
	return wrapErr(err)
}

func scanDigest(row rowScanner) (domain.MemoryDigest, error) {
	var (
		item  domain.MemoryDigest
		scope string
		level string
		dirty int
	)
	err := row.Scan(
		&item.ID, &scope, &item.OwnerID, &level, &item.PeriodStart, &item.PeriodEnd,
		&item.Summary, &item.Keywords, &item.MemoryCount, &item.SourceVersion,
		&dirty, &item.CreatedAt, &item.UpdatedAt,
	)
	item.Scope = domain.MemoryScope(scope)
	item.Level = domain.DigestLevel(level)
	item.Dirty = dirty != 0
	return item, err
}
