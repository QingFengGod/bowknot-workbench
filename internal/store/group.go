package store

import (
	"context"

	"bowknot-workbench/internal/domain"
)

const groupColumns = `id, name, pinned, created_at`

type GroupStore struct {
	s *Store
}

func (g *GroupStore) List(ctx context.Context) ([]domain.Group, error) {
	if err := g.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := g.s.q.QueryContext(ctx,
		`SELECT `+groupColumns+` FROM agent_group ORDER BY pinned DESC, created_at ASC`,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.Group, 0)
	for rows.Next() {
		item, err := scanGroup(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr(err)
	}
	// 单连接下不能在 rows 未关闭时再发查询，先收齐分组再补成员。
	if err := rows.Close(); err != nil {
		return nil, wrapErr(err)
	}

	for i := range out {
		members, err := g.members(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].MemberIDs = members
	}
	return out, nil
}

func (g *GroupStore) Get(ctx context.Context, id string) (domain.Group, error) {
	if err := g.s.ensureOpen(); err != nil {
		return domain.Group{}, err
	}

	row := g.s.q.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM agent_group WHERE id = ?`, id)
	item, err := scanGroup(row)
	if err != nil {
		return domain.Group{}, wrapErr(err)
	}
	members, err := g.members(ctx, id)
	if err != nil {
		return domain.Group{}, err
	}
	item.MemberIDs = members
	return item, nil
}

func (g *GroupStore) Upsert(ctx context.Context, item domain.Group) error {
	if g.s.inTx {
		return g.upsert(ctx, item)
	}

	return g.s.Tx(ctx, func(tx *Store) error {
		return tx.Groups.upsert(ctx, item)
	})
}

func (g *GroupStore) upsert(ctx context.Context, item domain.Group) error {
	if err := g.s.ensureOpen(); err != nil {
		return err
	}

	if _, err := g.s.q.ExecContext(ctx,
		`INSERT INTO agent_group(`+groupColumns+`) VALUES(?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name = excluded.name, pinned = excluded.pinned`,
		item.ID, item.Name, boolInt(item.Pinned), item.CreatedAt,
	); err != nil {
		return wrapErr(err)
	}

	if _, err := g.s.q.ExecContext(ctx, `DELETE FROM agent_group_member WHERE group_id = ?`, item.ID); err != nil {
		return wrapErr(err)
	}
	for _, agentID := range uniqueStrings(item.MemberIDs) {
		if _, err := g.s.q.ExecContext(ctx,
			`INSERT INTO agent_group_member(group_id, agent_id) VALUES(?, ?)`,
			item.ID, agentID,
		); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

func (g *GroupStore) Delete(ctx context.Context, id string) error {
	if err := g.s.ensureOpen(); err != nil {
		return err
	}

	result, err := g.s.q.ExecContext(ctx, `DELETE FROM agent_group WHERE id = ?`, id)
	return ensureAffected(result, err)
}

func (g *GroupStore) members(ctx context.Context, groupID string) ([]string, error) {
	rows, err := g.s.q.QueryContext(ctx,
		`SELECT agent_id FROM agent_group_member WHERE group_id = ? ORDER BY rowid ASC`, groupID,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var agentID string
		if err := rows.Scan(&agentID); err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, agentID)
	}
	return out, wrapErr(rows.Err())
}

func scanGroup(row rowScanner) (domain.Group, error) {
	var (
		item   domain.Group
		pinned int
	)
	err := row.Scan(&item.ID, &item.Name, &pinned, &item.CreatedAt)
	item.Pinned = pinned != 0
	return item, err
}
