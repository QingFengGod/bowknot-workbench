package store

import (
	"context"
	"database/sql"
	"strings"

	"bowknot-workbench/internal/domain"
)

const memoryColumns = `id, scope, owner_id, kind, content, content_hash, keywords, occurred_at, day_key, source_conversation_id, source_message_id, source_role, confidence, importance, pinned, status, supersedes_id, created_at, updated_at, last_used_at`

// MemoryStore 负责原子记忆的读写；去重由唯一索引与调用方共同保证。
type MemoryStore struct {
	s *Store
}

// Insert 写入一条记忆；active 状态下的 (scope, owner, kind, content_hash)
// 重复会返回 ErrConflict，调用方应先查 GetActiveByHash。
func (m *MemoryStore) Insert(ctx context.Context, item domain.Memory) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	_, err := m.s.q.ExecContext(ctx,
		`INSERT INTO memory(`+memoryColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, string(item.Scope), item.OwnerID, string(item.Kind), item.Content,
		item.ContentHash, item.Keywords, item.OccurredAt, item.DayKey,
		nullString(item.SourceConversationID), nullString(item.SourceMessageID), item.SourceRole,
		item.Confidence, item.Importance, boolInt(item.Pinned), string(item.Status),
		nullString(item.SupersedesID), item.CreatedAt, item.UpdatedAt, item.LastUsedAt,
	)
	return wrapErr(err)
}

func (m *MemoryStore) Get(ctx context.Context, id string) (domain.Memory, error) {
	if err := m.s.ensureOpen(); err != nil {
		return domain.Memory{}, err
	}

	row := m.s.q.QueryRowContext(ctx, `SELECT `+memoryColumns+` FROM memory WHERE id = ?`, id)
	item, err := scanMemory(row)
	return item, wrapErr(err)
}

// GetActiveByHash 用于写入前去重：同作用域、同类型、同内容只保留一条 active 记忆。
func (m *MemoryStore) GetActiveByHash(ctx context.Context, scope domain.MemoryScope, ownerID string, kind domain.MemoryKind, contentHash string) (domain.Memory, error) {
	if err := m.s.ensureOpen(); err != nil {
		return domain.Memory{}, err
	}

	row := m.s.q.QueryRowContext(ctx,
		`SELECT `+memoryColumns+` FROM memory
		 WHERE scope = ? AND owner_id = ? AND kind = ? AND content_hash = ? AND status = 'active'
		 LIMIT 1`,
		string(scope), ownerID, string(kind), contentHash,
	)
	item, err := scanMemory(row)
	return item, wrapErr(err)
}

// ListActive 按置顶与更新时间倒序返回 active 记忆；limit <= 0 表示不限制。
func (m *MemoryStore) ListActive(ctx context.Context, scope domain.MemoryScope, ownerID string, limit int) ([]domain.Memory, error) {
	if err := m.s.ensureOpen(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = -1
	}

	rows, err := m.s.q.QueryContext(ctx,
		`SELECT `+memoryColumns+` FROM memory
		 WHERE scope = ? AND owner_id = ? AND status = 'active'
		 ORDER BY pinned DESC, updated_at DESC, rowid DESC
		 LIMIT ?`,
		string(scope), ownerID, limit,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

// ListByDay 返回某一天的全部 active 记忆，按发生时间排序，用于日摘要。
func (m *MemoryStore) ListByDay(ctx context.Context, scope domain.MemoryScope, ownerID, dayKey string) ([]domain.Memory, error) {
	if err := m.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := m.s.q.QueryContext(ctx,
		`SELECT `+memoryColumns+` FROM memory
		 WHERE scope = ? AND owner_id = ? AND day_key = ? AND status = 'active'
		 ORDER BY occurred_at ASC, rowid ASC`,
		string(scope), ownerID, dayKey,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

// ListByRange 返回发生时间落在 [fromMs, toMs] 内的 active 记忆，用于按周期线索定位原文。
func (m *MemoryStore) ListByRange(ctx context.Context, scope domain.MemoryScope, ownerID string, fromMs, toMs int64) ([]domain.Memory, error) {
	if err := m.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := m.s.q.QueryContext(ctx,
		`SELECT `+memoryColumns+` FROM memory
		 WHERE scope = ? AND owner_id = ? AND status = 'active' AND occurred_at BETWEEN ? AND ?
		 ORDER BY occurred_at ASC, rowid ASC`,
		string(scope), ownerID, fromMs, toMs,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

// MemoryOwner 是存在 active 记忆的作用域与归属，供摘要任务枚举。
type MemoryOwner struct {
	Scope   domain.MemoryScope
	OwnerID string
}

// ListOwners 返回存在 active 记忆的作用域与归属组合。
func (m *MemoryStore) ListOwners(ctx context.Context) ([]MemoryOwner, error) {
	if err := m.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := m.s.q.QueryContext(ctx,
		`SELECT DISTINCT scope, owner_id FROM memory WHERE status = 'active' ORDER BY scope, owner_id`,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]MemoryOwner, 0)
	for rows.Next() {
		var owner MemoryOwner
		var scope string
		if err := rows.Scan(&scope, &owner.OwnerID); err != nil {
			return nil, wrapErr(err)
		}
		owner.Scope = domain.MemoryScope(scope)
		out = append(out, owner)
	}
	return out, wrapErr(rows.Err())
}

// ListDays 返回存在 active 记忆的日期，按日期倒序，用于挑选需要生成摘要的周期。
func (m *MemoryStore) ListDays(ctx context.Context, scope domain.MemoryScope, ownerID string, limit int) ([]string, error) {
	if err := m.s.ensureOpen(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = -1
	}

	rows, err := m.s.q.QueryContext(ctx,
		`SELECT day_key FROM memory
		 WHERE scope = ? AND owner_id = ? AND status = 'active'
		 GROUP BY day_key ORDER BY day_key DESC LIMIT ?`,
		string(scope), ownerID, limit,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, day)
	}
	return out, wrapErr(rows.Err())
}

// Touch 在重复抽取或注入后刷新时间与置信度，不新增记录。
func (m *MemoryStore) Touch(ctx context.Context, id string, confidence float64, updatedAt, lastUsedAt int64) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	result, err := m.s.q.ExecContext(ctx,
		`UPDATE memory SET confidence = ?, updated_at = ?, last_used_at = ? WHERE id = ?`,
		confidence, updatedAt, lastUsedAt, id,
	)
	return ensureAffected(result, err)
}

// Update 保存用户编辑后的内容与展示属性。
func (m *MemoryStore) Update(ctx context.Context, item domain.Memory) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	result, err := m.s.q.ExecContext(ctx,
		`UPDATE memory SET kind = ?, content = ?, content_hash = ?, keywords = ?, importance = ?, pinned = ?, updated_at = ?
		 WHERE id = ?`,
		string(item.Kind), item.Content, item.ContentHash, item.Keywords,
		item.Importance, boolInt(item.Pinned), item.UpdatedAt, item.ID,
	)
	return ensureAffected(result, err)
}

func (m *MemoryStore) SetPinned(ctx context.Context, id string, pinned bool, updatedAt int64) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	result, err := m.s.q.ExecContext(ctx,
		`UPDATE memory SET pinned = ?, updated_at = ? WHERE id = ?`,
		boolInt(pinned), updatedAt, id,
	)
	return ensureAffected(result, err)
}

// Supersede 把被纠正的旧记忆标记为 superseded；旧记录保留用于追溯。
func (m *MemoryStore) Supersede(ctx context.Context, oldID string, updatedAt int64) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	result, err := m.s.q.ExecContext(ctx,
		`UPDATE memory SET status = 'superseded', updated_at = ? WHERE id = ? AND status = 'active'`,
		updatedAt, oldID,
	)
	return ensureAffected(result, err)
}

func (m *MemoryStore) Delete(ctx context.Context, id string) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	result, err := m.s.q.ExecContext(ctx, `DELETE FROM memory WHERE id = ?`, id)
	return ensureAffected(result, err)
}

// DeleteAll 清空全部记忆；来源消息与会话不受影响。
func (m *MemoryStore) DeleteAll(ctx context.Context) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	_, err := m.s.q.ExecContext(ctx, `DELETE FROM memory`)
	return wrapErr(err)
}

// MarkUsed 在记忆注入后异步刷新使用时间。
func (m *MemoryStore) MarkUsed(ctx context.Context, ids []string, at int64) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, at)
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := m.s.q.ExecContext(ctx,
		`UPDATE memory SET last_used_at = ? WHERE id IN (`+placeholders+`)`,
		args...,
	)
	return wrapErr(err)
}

func scanMemory(row rowScanner) (domain.Memory, error) {
	var (
		item      domain.Memory
		scope     string
		kind      string
		status    string
		sourceCID sql.NullString
		sourceMID sql.NullString
		superID   sql.NullString
		pinned    int
	)
	err := row.Scan(
		&item.ID, &scope, &item.OwnerID, &kind, &item.Content, &item.ContentHash,
		&item.Keywords, &item.OccurredAt, &item.DayKey, &sourceCID, &sourceMID,
		&item.SourceRole, &item.Confidence, &item.Importance, &pinned, &status,
		&superID, &item.CreatedAt, &item.UpdatedAt, &item.LastUsedAt,
	)
	item.Scope = domain.MemoryScope(scope)
	item.Kind = domain.MemoryKind(kind)
	item.Status = domain.MemoryStatus(status)
	item.SourceConversationID = sourceCID.String
	item.SourceMessageID = sourceMID.String
	item.SupersedesID = superID.String
	item.Pinned = pinned != 0
	return item, err
}

func scanMemories(rows *sql.Rows) ([]domain.Memory, error) {
	out := make([]domain.Memory, 0)
	for rows.Next() {
		item, err := scanMemory(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

// prefixColumns 给列名统一加表别名，用于 join 查询。
func prefixColumns(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}
