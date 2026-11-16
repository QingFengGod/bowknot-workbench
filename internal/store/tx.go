package store

import (
	"context"
	"fmt"
)

// Tx 在同一个事务中执行 fn；事务内的 Store 提供完全相同的子 store。
func (s *Store) Tx(ctx context.Context, fn func(tx *Store) error) error {
	if s.inTx {
		return fmt.Errorf("store: nested transactions are not supported")
	}
	if err := s.ensureOpen(); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	txStore := newStore(s.db, tx, s.state, s.logger, true)
	if err := fn(txStore); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	committed = true
	return nil
}
