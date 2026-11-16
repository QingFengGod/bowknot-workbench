package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
	ErrClosed   = errors.New("store: closed")
)

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}

	message := err.Error()
	if strings.Contains(message, "UNIQUE constraint failed") || strings.Contains(message, "FOREIGN KEY constraint failed") {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}

func ensureAffected(result sql.Result, err error) error {
	if err != nil {
		return wrapErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return wrapErr(err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
