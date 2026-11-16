package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type rowScanner interface {
	Scan(dest ...any) error
}

type storeState struct {
	mu     sync.Mutex
	closed bool
}

// Store 是所有结构化数据的唯一入口。
// 事务中的 Store 共享同一个状态，但执行器替换为 *sql.Tx。
type Store struct {
	db     *sql.DB
	q      querier
	state  *storeState
	logger *slog.Logger
	inTx   bool

	Conversations *ConversationStore
	Messages      *MessageStore
	Runs          *RunStore
	Agents        *AgentStore
	Groups        *GroupStore
	Apps          *AppStore
	Plugins       *PluginStore
	ToolCalls     *ToolCallStore
	Memories      *MemoryStore
	MemoryDigests *MemoryDigestStore
	Meta          *MetaStore
}

type Options struct {
	Path        string
	BusyTimeout time.Duration
	Logger      *slog.Logger
	Seed        bool
}

// Open 打开数据库、执行迁移、收敛中断任务，并按需导入演示数据。
func Open(ctx context.Context, opts Options) (*Store, error) {
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return nil, errors.New("store: path is required")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("store: resolve path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("store: create data directory: %w", err)
	}

	if opts.BusyTimeout <= 0 {
		opts.BusyTimeout = 5 * time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	db, err := sql.Open("sqlite", dataSourceName(abs, opts.BusyTimeout))
	if err != nil {
		return nil, fmt.Errorf("store: open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping database: %w", err)
	}

	state := &storeState{}
	s := newStore(db, db, state, logger, false)

	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	interrupted, err := s.Runs.MarkInterrupted(ctx)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: mark interrupted runs: %w", err)
	}
	if interrupted > 0 {
		logger.Warn("收敛未完成的生成任务", "count", interrupted)
	}

	if opts.Seed {
		if err := s.seedDemo(ctx); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store: seed demo data: %w", err)
		}
	}

	return s, nil
}

// Close 关闭数据库；重复调用安全。
func (s *Store) Close(ctx context.Context) error {
	if s.inTx {
		return errors.New("store: cannot close a transaction")
	}

	s.state.mu.Lock()
	if s.state.closed {
		s.state.mu.Unlock()
		return nil
	}
	s.state.closed = true
	s.state.mu.Unlock()

	_, _ = s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return s.db.Close()
}

func (s *Store) ensureOpen() error {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.state.closed {
		return ErrClosed
	}
	return nil
}

func newStore(db *sql.DB, q querier, state *storeState, logger *slog.Logger, inTx bool) *Store {
	s := &Store{
		db:     db,
		q:      q,
		state:  state,
		logger: logger,
		inTx:   inTx,
	}
	s.Conversations = &ConversationStore{s: s}
	s.Messages = &MessageStore{s: s}
	s.Runs = &RunStore{s: s}
	s.Agents = &AgentStore{s: s}
	s.Groups = &GroupStore{s: s}
	s.Apps = &AppStore{s: s}
	s.Plugins = &PluginStore{s: s}
	s.ToolCalls = &ToolCallStore{s: s}
	s.Memories = &MemoryStore{s: s}
	s.MemoryDigests = &MemoryDigestStore{s: s}
	s.Meta = &MetaStore{s: s}
	return s
}

func dataSourceName(path string, busyTimeout time.Duration) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}

	params := url.Values{}
	params.Set("_txlock", "immediate")
	params.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	params.Add("_pragma", "foreign_keys(ON)")
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "synchronous(NORMAL)")
	return "file://" + slashed + "?" + params.Encode()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
