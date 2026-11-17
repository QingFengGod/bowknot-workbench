package store

import (
	"context"
	"encoding/json"

	"bowknot-workbench/internal/domain"
)

const pluginColumns = `id, name, version, description, icon_text, source_type, source_value, install_dir, installed_at, runtime, permissions`

type PluginStore struct {
	s *Store
}

func (p *PluginStore) List(ctx context.Context) ([]domain.Plugin, error) {
	if err := p.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := p.s.q.QueryContext(ctx,
		`SELECT `+pluginColumns+` FROM plugin ORDER BY installed_at DESC`,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.Plugin, 0)
	for rows.Next() {
		item, err := scanPlugin(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr(err)
	}
	// 单连接下不能在 rows 未关闭时再发查询，先收齐插件再补关联。
	if err := rows.Close(); err != nil {
		return nil, wrapErr(err)
	}

	for i := range out {
		if err := p.loadRelations(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (p *PluginStore) Get(ctx context.Context, id string) (domain.Plugin, error) {
	if err := p.s.ensureOpen(); err != nil {
		return domain.Plugin{}, err
	}

	row := p.s.q.QueryRowContext(ctx, `SELECT `+pluginColumns+` FROM plugin WHERE id = ?`, id)
	item, err := scanPlugin(row)
	if err != nil {
		return domain.Plugin{}, wrapErr(err)
	}
	if err := p.loadRelations(ctx, &item); err != nil {
		return domain.Plugin{}, err
	}
	return item, nil
}

func (p *PluginStore) Insert(ctx context.Context, item domain.Plugin) error {
	if p.s.inTx {
		return p.insert(ctx, item)
	}

	return p.s.Tx(ctx, func(tx *Store) error {
		return tx.Plugins.insert(ctx, item)
	})
}

func (p *PluginStore) insert(ctx context.Context, item domain.Plugin) error {
	if err := p.s.ensureOpen(); err != nil {
		return err
	}

	_, err := p.s.q.ExecContext(ctx,
		`INSERT INTO plugin(`+pluginColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Name, item.Version, item.Description, item.IconText,
		item.SourceType, item.SourceValue, item.InstallDir, item.InstalledAt,
		marshalPluginRuntime(item.Runtime), marshalPluginPermissions(item.Permissions),
	)
	if err != nil {
		return wrapErr(err)
	}
	if err := p.insertTools(ctx, item.ID, item.Tools); err != nil {
		return err
	}
	return p.insertGrants(ctx, item.ID, item.AgentIDs)
}

// Upsert 写入插件记录，已存在时更新包元信息；工具与授权由调用方单独维护。
func (p *PluginStore) Upsert(ctx context.Context, item domain.Plugin) error {
	if p.s.inTx {
		return p.upsert(ctx, item)
	}
	return p.s.Tx(ctx, func(tx *Store) error {
		return tx.Plugins.upsert(ctx, item)
	})
}

func (p *PluginStore) upsert(ctx context.Context, item domain.Plugin) error {
	if err := p.s.ensureOpen(); err != nil {
		return err
	}

	_, err := p.s.q.ExecContext(ctx,
		`INSERT INTO plugin(`+pluginColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			version = excluded.version,
			description = excluded.description,
			icon_text = excluded.icon_text,
			source_type = excluded.source_type,
			source_value = excluded.source_value,
			install_dir = excluded.install_dir,
			installed_at = excluded.installed_at,
			runtime = excluded.runtime,
			permissions = excluded.permissions`,
		item.ID, item.Name, item.Version, item.Description, item.IconText,
		item.SourceType, item.SourceValue, item.InstallDir, item.InstalledAt,
		marshalPluginRuntime(item.Runtime), marshalPluginPermissions(item.Permissions),
	)
	return wrapErr(err)
}

func (p *PluginStore) Delete(ctx context.Context, id string) error {
	if err := p.s.ensureOpen(); err != nil {
		return err
	}

	result, err := p.s.q.ExecContext(ctx, `DELETE FROM plugin WHERE id = ?`, id)
	return ensureAffected(result, err)
}

func (p *PluginStore) ReplaceTools(ctx context.Context, pluginID string, tools []domain.Tool) error {
	if p.s.inTx {
		return p.replaceTools(ctx, pluginID, tools)
	}

	return p.s.Tx(ctx, func(tx *Store) error {
		return tx.Plugins.replaceTools(ctx, pluginID, tools)
	})
}

func (p *PluginStore) replaceTools(ctx context.Context, pluginID string, tools []domain.Tool) error {
	if err := p.s.ensureOpen(); err != nil {
		return err
	}

	var exists int
	if err := p.s.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM plugin WHERE id = ?`, pluginID).Scan(&exists); err != nil {
		return wrapErr(err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	if _, err := p.s.q.ExecContext(ctx, `DELETE FROM plugin_tool WHERE plugin_id = ?`, pluginID); err != nil {
		return wrapErr(err)
	}
	return p.insertTools(ctx, pluginID, tools)
}

func (p *PluginStore) SetGrants(ctx context.Context, pluginID string, agentIDs []string) error {
	if p.s.inTx {
		return p.setGrants(ctx, pluginID, agentIDs)
	}

	return p.s.Tx(ctx, func(tx *Store) error {
		return tx.Plugins.setGrants(ctx, pluginID, agentIDs)
	})
}

func (p *PluginStore) setGrants(ctx context.Context, pluginID string, agentIDs []string) error {
	if err := p.s.ensureOpen(); err != nil {
		return err
	}

	var exists int
	if err := p.s.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM plugin WHERE id = ?`, pluginID).Scan(&exists); err != nil {
		return wrapErr(err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	if _, err := p.s.q.ExecContext(ctx, `DELETE FROM plugin_grant WHERE plugin_id = ?`, pluginID); err != nil {
		return wrapErr(err)
	}
	return p.insertGrants(ctx, pluginID, agentIDs)
}

func (p *PluginStore) loadRelations(ctx context.Context, item *domain.Plugin) error {
	tools, err := p.tools(ctx, item.ID)
	if err != nil {
		return err
	}
	grants, err := p.grants(ctx, item.ID)
	if err != nil {
		return err
	}
	item.Tools = tools
	item.AgentIDs = grants
	return nil
}

func (p *PluginStore) tools(ctx context.Context, pluginID string) ([]domain.Tool, error) {
	rows, err := p.s.q.QueryContext(ctx,
		`SELECT name, description, parameters, timeout_ms, dangerous FROM plugin_tool WHERE plugin_id = ? ORDER BY rowid ASC`, pluginID,
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
		)
		if err := rows.Scan(&item.Name, &item.Description, &item.Parameters, &item.TimeoutMS, &dangerous); err != nil {
			return nil, wrapErr(err)
		}
		item.Dangerous = dangerous != 0
		item.Enabled = true
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

func (p *PluginStore) grants(ctx context.Context, pluginID string) ([]string, error) {
	rows, err := p.s.q.QueryContext(ctx,
		`SELECT agent_id FROM plugin_grant WHERE plugin_id = ? ORDER BY rowid ASC`, pluginID,
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

func (p *PluginStore) insertTools(ctx context.Context, pluginID string, tools []domain.Tool) error {
	for _, tool := range tools {
		if _, err := p.s.q.ExecContext(ctx,
			`INSERT INTO plugin_tool(plugin_id, name, description, parameters, timeout_ms, dangerous) VALUES(?, ?, ?, ?, ?, ?)`,
			pluginID, tool.Name, tool.Description, tool.Parameters, tool.TimeoutMS, boolInt(tool.Dangerous),
		); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

func (p *PluginStore) insertGrants(ctx context.Context, pluginID string, agentIDs []string) error {
	for _, agentID := range uniqueStrings(agentIDs) {
		if _, err := p.s.q.ExecContext(ctx,
			`INSERT INTO plugin_grant(plugin_id, agent_id) VALUES(?, ?)`,
			pluginID, agentID,
		); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

func scanPlugin(row rowScanner) (domain.Plugin, error) {
	var item domain.Plugin
	var runtimeJSON, permissionsJSON string
	err := row.Scan(
		&item.ID, &item.Name, &item.Version, &item.Description, &item.IconText,
		&item.SourceType, &item.SourceValue, &item.InstallDir, &item.InstalledAt,
		&runtimeJSON, &permissionsJSON,
	)
	if err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(runtimeJSON), &item.Runtime); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(permissionsJSON), &item.Permissions); err != nil {
		return item, err
	}
	return item, nil
}

// marshalPluginRuntime 把运行方式序列化为 JSON 文本，空值写成空对象
// 以保证读回时能稳定反序列化。
func marshalPluginRuntime(item domain.Runtime) string {
	data, err := json.Marshal(item)
	if err != nil || len(data) == 0 {
		return "{}"
	}
	return string(data)
}

func marshalPluginPermissions(item domain.Permissions) string {
	data, err := json.Marshal(item)
	if err != nil || len(data) == 0 {
		return "{}"
	}
	return string(data)
}
