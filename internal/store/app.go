package store

import (
	"context"

	"bowknot-workbench/internal/domain"
)

const appColumns = `id, name, display_name, version, description, icon_text, source_type, source_value, install_dir, entry, installed_at, last_used_at, data_size`

type AppStore struct {
	s *Store
}

func (a *AppStore) List(ctx context.Context) ([]domain.App, error) {
	if err := a.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := a.s.q.QueryContext(ctx,
		`SELECT `+appColumns+` FROM app ORDER BY installed_at DESC`,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.App, 0)
	for rows.Next() {
		item, err := scanApp(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr(err)
	}
	// 单连接下不能在 rows 未关闭时再发查询，先收齐基础行再补工具。
	if err := rows.Close(); err != nil {
		return nil, wrapErr(err)
	}

	for i := range out {
		tools, err := a.tools(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Tools = tools
	}
	return out, nil
}

func (a *AppStore) Get(ctx context.Context, id string) (domain.App, error) {
	if err := a.s.ensureOpen(); err != nil {
		return domain.App{}, err
	}

	row := a.s.q.QueryRowContext(ctx, `SELECT `+appColumns+` FROM app WHERE id = ?`, id)
	item, err := scanApp(row)
	if err != nil {
		return domain.App{}, wrapErr(err)
	}
	tools, err := a.tools(ctx, id)
	if err != nil {
		return domain.App{}, err
	}
	item.Tools = tools
	return item, nil
}

func (a *AppStore) Insert(ctx context.Context, item domain.App) error {
	if a.s.inTx {
		return a.insert(ctx, item)
	}

	return a.s.Tx(ctx, func(tx *Store) error {
		return tx.Apps.insert(ctx, item)
	})
}

func (a *AppStore) insert(ctx context.Context, item domain.App) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	_, err := a.s.q.ExecContext(ctx,
		`INSERT INTO app(`+appColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Name, item.DisplayName, item.Version, item.Description,
		item.IconText, item.SourceType, item.SourceValue, item.InstallDir, item.Entry,
		item.InstalledAt, item.LastUsedAt, item.DataSize,
	)
	if err != nil {
		return wrapErr(err)
	}
	return a.insertTools(ctx, item.ID, item.Tools)
}

// Upsert 写入应用记录，已存在时更新包元信息；工具与用量字段由调用方单独维护。
func (a *AppStore) Upsert(ctx context.Context, item domain.App) error {
	if a.s.inTx {
		return a.upsert(ctx, item)
	}
	return a.s.Tx(ctx, func(tx *Store) error {
		return tx.Apps.upsert(ctx, item)
	})
}

func (a *AppStore) upsert(ctx context.Context, item domain.App) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	_, err := a.s.q.ExecContext(ctx,
		`INSERT INTO app(`+appColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			display_name = excluded.display_name,
			version = excluded.version,
			description = excluded.description,
			icon_text = excluded.icon_text,
			source_type = excluded.source_type,
			source_value = excluded.source_value,
			install_dir = excluded.install_dir,
			entry = excluded.entry,
			installed_at = excluded.installed_at`,
		item.ID, item.Name, item.DisplayName, item.Version, item.Description,
		item.IconText, item.SourceType, item.SourceValue, item.InstallDir, item.Entry,
		item.InstalledAt, item.LastUsedAt, item.DataSize,
	)
	return wrapErr(err)
}

func (a *AppStore) Delete(ctx context.Context, id string) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	result, err := a.s.q.ExecContext(ctx, `DELETE FROM app WHERE id = ?`, id)
	return ensureAffected(result, err)
}

func (a *AppStore) ReplaceTools(ctx context.Context, appID string, tools []domain.Tool) error {
	if a.s.inTx {
		return a.replaceTools(ctx, appID, tools)
	}

	return a.s.Tx(ctx, func(tx *Store) error {
		return tx.Apps.replaceTools(ctx, appID, tools)
	})
}

func (a *AppStore) replaceTools(ctx context.Context, appID string, tools []domain.Tool) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	var exists int
	if err := a.s.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM app WHERE id = ?`, appID).Scan(&exists); err != nil {
		return wrapErr(err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	if _, err := a.s.q.ExecContext(ctx, `DELETE FROM app_tool WHERE app_id = ?`, appID); err != nil {
		return wrapErr(err)
	}
	return a.insertTools(ctx, appID, tools)
}

func (a *AppStore) SetToolEnabled(ctx context.Context, appID, toolName string, enabled bool) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	result, err := a.s.q.ExecContext(ctx,
		`UPDATE app_tool SET enabled = ? WHERE app_id = ? AND name = ?`,
		boolInt(enabled), appID, toolName,
	)
	return ensureAffected(result, err)
}

func (a *AppStore) SetDataSize(ctx context.Context, id string, size int64) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	result, err := a.s.q.ExecContext(ctx, `UPDATE app SET data_size = ? WHERE id = ?`, size, id)
	return ensureAffected(result, err)
}

func (a *AppStore) MarkUsed(ctx context.Context, id string, at int64) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	result, err := a.s.q.ExecContext(ctx, `UPDATE app SET last_used_at = ? WHERE id = ?`, at, id)
	return ensureAffected(result, err)
}

func (a *AppStore) tools(ctx context.Context, appID string) ([]domain.Tool, error) {
	rows, err := a.s.q.QueryContext(ctx,
		`SELECT name, description, dangerous, enabled FROM app_tool WHERE app_id = ? ORDER BY rowid ASC`, appID,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.Tool, 0)
	for rows.Next() {
		var (
			item      domain.Tool
			dangerous int
			enabled   int
		)
		if err := rows.Scan(&item.Name, &item.Description, &dangerous, &enabled); err != nil {
			return nil, wrapErr(err)
		}
		item.Dangerous = dangerous != 0
		item.Enabled = enabled != 0
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

func (a *AppStore) insertTools(ctx context.Context, appID string, tools []domain.Tool) error {
	for _, tool := range tools {
		if _, err := a.s.q.ExecContext(ctx,
			`INSERT INTO app_tool(app_id, name, description, dangerous, enabled) VALUES(?, ?, ?, ?, ?)`,
			appID, tool.Name, tool.Description, boolInt(tool.Dangerous), boolInt(tool.Enabled),
		); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

func scanApp(row rowScanner) (domain.App, error) {
	var item domain.App
	err := row.Scan(
		&item.ID, &item.Name, &item.DisplayName, &item.Version, &item.Description,
		&item.IconText, &item.SourceType, &item.SourceValue, &item.InstallDir,
		&item.Entry, &item.InstalledAt, &item.LastUsedAt, &item.DataSize,
	)
	return item, err
}
