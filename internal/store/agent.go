package store

import (
	"context"

	"bowknot-workbench/internal/domain"
)

const agentColumns = `id, name, description, avatar_text, persona, model_id, pinned, created_at, updated_at`

type AgentStore struct {
	s *Store
}

func (a *AgentStore) List(ctx context.Context) ([]domain.Agent, error) {
	if err := a.s.ensureOpen(); err != nil {
		return nil, err
	}

	rows, err := a.s.q.QueryContext(ctx,
		`SELECT `+agentColumns+` FROM agent ORDER BY pinned DESC, created_at ASC`,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	out := make([]domain.Agent, 0)
	for rows.Next() {
		item, err := scanAgent(rows)
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, item)
	}
	return out, wrapErr(rows.Err())
}

func (a *AgentStore) Get(ctx context.Context, id string) (domain.Agent, error) {
	if err := a.s.ensureOpen(); err != nil {
		return domain.Agent{}, err
	}

	row := a.s.q.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agent WHERE id = ?`, id)
	item, err := scanAgent(row)
	return item, wrapErr(err)
}

func (a *AgentStore) Upsert(ctx context.Context, item domain.Agent) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	_, err := a.s.q.ExecContext(ctx,
		`INSERT INTO agent(`+agentColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name = excluded.name,
		   description = excluded.description,
		   avatar_text = excluded.avatar_text,
		   persona = excluded.persona,
		   model_id = excluded.model_id,
		   pinned = excluded.pinned,
		   updated_at = excluded.updated_at`,
		item.ID, item.Name, item.Description, item.AvatarText, item.Persona,
		item.ModelID, boolInt(item.Pinned), item.CreatedAt, item.UpdatedAt,
	)
	return wrapErr(err)
}

func (a *AgentStore) Delete(ctx context.Context, id string) error {
	if err := a.s.ensureOpen(); err != nil {
		return err
	}

	result, err := a.s.q.ExecContext(ctx, `DELETE FROM agent WHERE id = ?`, id)
	return ensureAffected(result, err)
}

func scanAgent(row rowScanner) (domain.Agent, error) {
	var (
		item   domain.Agent
		pinned int
	)
	err := row.Scan(
		&item.ID, &item.Name, &item.Description, &item.AvatarText,
		&item.Persona, &item.ModelID, &pinned, &item.CreatedAt, &item.UpdatedAt,
	)
	item.Pinned = pinned != 0
	return item, err
}
