package store

import (
	"context"
	"database/sql"
	"errors"
)

type MetaStore struct {
	s *Store
}

func (m *MetaStore) Get(ctx context.Context, key string) (string, error) {
	if err := m.s.ensureOpen(); err != nil {
		return "", err
	}

	var value string
	err := m.s.q.QueryRowContext(ctx, `SELECT value FROM app_meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return value, wrapErr(err)
}

func (m *MetaStore) Set(ctx context.Context, key, value string) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	_, err := m.s.q.ExecContext(ctx,
		`INSERT INTO app_meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return wrapErr(err)
}

func (m *MetaStore) Delete(ctx context.Context, key string) error {
	if err := m.s.ensureOpen(); err != nil {
		return err
	}

	result, err := m.s.q.ExecContext(ctx, `DELETE FROM app_meta WHERE key = ?`, key)
	return ensureAffected(result, err)
}
