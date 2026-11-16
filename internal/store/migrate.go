package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  applied_at INTEGER NOT NULL
);`

type migration struct {
	version int
	name    string
}

// migrate 按版本号顺序执行尚未应用的迁移；已应用的版本会跳过。
func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}

	entries, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("store: list migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, name := range entries {
		base := strings.TrimPrefix(name, "migrations/")
		prefix, _, ok := strings.Cut(base, "_")
		if !ok {
			return fmt.Errorf("store: invalid migration name %q", base)
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return fmt.Errorf("store: invalid migration version %q: %w", base, err)
		}
		migrations = append(migrations, migration{version: version, name: name})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	for _, item := range migrations {
		var applied int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, item.version).Scan(&applied); err != nil {
			return fmt.Errorf("store: check migration %d: %w", item.version, err)
		}
		if applied > 0 {
			continue
		}

		body, err := migrationFiles.ReadFile(item.name)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", item.name, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: begin migration %d: %w", item.version, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: apply migration %d: %w", item.version, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			item.version, time.Now().UnixMilli(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: record migration %d: %w", item.version, err)
		}
		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: commit migration %d: %w", item.version, err)
		}
	}

	// 数据库由更新版本的程序写过时拒绝打开，避免旧程序继续写入造成不可逆损坏。
	var maxApplied int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&maxApplied); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if latest := migrations[len(migrations)-1].version; maxApplied > latest {
		return fmt.Errorf("store: database schema version %d is newer than supported %d, please upgrade the app", maxApplied, latest)
	}

	return nil
}
