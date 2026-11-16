package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"bowknot-workbench/internal/domain"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), Options{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s
}

func conversation(id string, updatedAt int64) domain.Conversation {
	return domain.Conversation{
		ID:        id,
		Kind:      domain.KindAssistant,
		OwnerID:   "main",
		Title:     id,
		Summary:   "summary",
		CreatedAt: updatedAt - 100,
		UpdatedAt: updatedAt,
	}
}

func message(id, conversationID string, role domain.Role) domain.Message {
	return domain.Message{
		ID:             id,
		ConversationID: conversationID,
		Role:           role,
		Content:        id,
		Status:         domain.MessageDone,
		CreatedAt:      1000,
	}
}

func TestOpenAndMigrationIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	first, err := Open(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	var version int
	if err := first.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if version != 4 {
		t.Fatalf("schema version = %d, want 4", version)
	}
	var tables int
	if err := first.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'conversation'`).Scan(&tables); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if tables != 1 {
		t.Fatalf("conversation table count = %d, want 1", tables)
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	second, err := Open(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer second.Close(context.Background())

	var count int
	if err := second.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 4 {
		t.Fatalf("applied migrations = %d, want 4", count)
	}
}

func TestTxRollbackAndNested(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	err := s.Tx(ctx, func(tx *Store) error {
		if err := tx.Conversations.Create(ctx, conversation("rollback", 10)); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("Tx() = nil, want rollback error")
	}

	list, err := s.Conversations.List(ctx, domain.KindAssistant, "main")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("conversations = %d, want 0", len(list))
	}

	err = s.Tx(ctx, func(tx *Store) error {
		return tx.Tx(ctx, func(*Store) error { return nil })
	})
	if err == nil {
		t.Fatal("nested Tx() = nil, want error")
	}
}

func TestConversationCRUD(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Conversations.Create(ctx, conversation("older", 10)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	pinned := conversation("pinned", 5)
	pinned.Pinned = true
	if err := s.Conversations.Create(ctx, pinned); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := s.Conversations.Create(ctx, conversation("older", 20)); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Create() error = %v, want ErrConflict", err)
	}

	list, err := s.Conversations.List(ctx, domain.KindAssistant, "main")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != "pinned" {
		t.Fatalf("List() = %+v, want pinned first", list)
	}

	if err := s.Conversations.UpdateMeta(ctx, "older", "新标题", "新摘要", true, 99); err != nil {
		t.Fatalf("UpdateMeta() error = %v", err)
	}
	got, err := s.Conversations.Get(ctx, "older")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != "新标题" || got.Summary != "新摘要" || !got.Pinned || got.UpdatedAt != 99 {
		t.Fatalf("Get() = %+v", got)
	}

	if err := s.Conversations.Touch(ctx, "older", 120); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	got, err = s.Conversations.Get(ctx, "older")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.UpdatedAt != 120 {
		t.Fatalf("UpdatedAt = %d, want 120", got.UpdatedAt)
	}

	if err := s.Conversations.Delete(ctx, "older"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := s.Conversations.Get(ctx, "older"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after delete error = %v, want ErrNotFound", err)
	}
}

func TestMessageAppendSeqAndCascade(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Conversations.Create(ctx, conversation("c1", 10)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	first, err := s.Messages.Append(ctx, message("m1", "c1", domain.RoleUser))
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if first.Seq != 1 {
		t.Fatalf("first seq = %d, want 1", first.Seq)
	}
	second, err := s.Messages.Append(ctx, message("m2", "c1", domain.RoleAssistant))
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if second.Seq != 2 {
		t.Fatalf("second seq = %d, want 2", second.Seq)
	}

	if _, err := s.Messages.Append(ctx, message("m1", "c1", domain.RoleUser)); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Append() error = %v, want ErrConflict", err)
	}

	second.Content = "updated"
	second.Status = domain.MessageFailed
	second.Error = "boom"
	if err := s.Messages.Update(ctx, second); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	list, err := s.Messages.List(ctx, "c1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[1].Content != "updated" || list[1].Status != domain.MessageFailed {
		t.Fatalf("List() = %+v", list)
	}

	if err := s.Runs.Start(ctx, domain.Run{ID: "r1", ConversationID: "c1", MessageID: "m2", StartedAt: 2000}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := s.Conversations.Delete(ctx, "c1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	remaining, err := s.Messages.List(ctx, "c1")
	if err != nil {
		t.Fatalf("List() after cascade error = %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("messages = %d, want 0 after cascade", len(remaining))
	}
	if _, err := s.Runs.Get(ctx, "r1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after cascade error = %v, want ErrNotFound", err)
	}
}

func TestRuns(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Conversations.Create(ctx, conversation("c1", 10)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	streaming := message("m1", "c1", domain.RoleAssistant)
	streaming.Status = domain.MessageStreaming
	if _, err := s.Messages.Append(ctx, streaming); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := s.Runs.Start(ctx, domain.Run{ID: "r1", ConversationID: "c1", MessageID: "m1", StartedAt: 100}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	count, err := s.Runs.MarkInterrupted(ctx)
	if err != nil {
		t.Fatalf("MarkInterrupted() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("interrupted = %d, want 1", count)
	}
	run, err := s.Runs.Get(ctx, "r1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if run.Status != domain.RunStopped || run.FinishedAt == 0 {
		t.Fatalf("run = %+v, want stopped with finishedAt", run)
	}
	list, err := s.Messages.List(ctx, "c1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if list[0].Status != domain.MessageStopped {
		t.Fatalf("message status = %q, want stopped", list[0].Status)
	}

	if err := s.Runs.Start(ctx, domain.Run{ID: "r2", ConversationID: "c1", MessageID: "m1", StartedAt: 200}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := s.Runs.Finish(ctx, "r2", domain.RunFailed, 300); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	run, err = s.Runs.Get(ctx, "r2")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if run.Status != domain.RunFailed || run.FinishedAt != 300 {
		t.Fatalf("run = %+v", run)
	}
}

func TestAgentsAndGroups(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	agents := []domain.Agent{
		{ID: "a2", Name: "A2", CreatedAt: 1, UpdatedAt: 10},
		{ID: "a1", Name: "A1", Pinned: true, CreatedAt: 2, UpdatedAt: 20},
	}
	for _, item := range agents {
		if err := s.Agents.Upsert(ctx, item); err != nil {
			t.Fatalf("Upsert() error = %v", err)
		}
	}
	list, err := s.Agents.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != "a1" {
		t.Fatalf("List() = %+v, want pinned a1 first", list)
	}

	update := agents[0]
	update.Name = "A2-new"
	update.Pinned = true
	update.UpdatedAt = 30
	if err := s.Agents.Upsert(ctx, update); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	got, err := s.Agents.Get(ctx, "a2")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != "A2-new" || !got.Pinned {
		t.Fatalf("agent = %+v", got)
	}

	group := domain.Group{ID: "g1", Name: "Group", MemberIDs: []string{"a1", "a2", "a1"}, CreatedAt: 5}
	if err := s.Groups.Upsert(ctx, group); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	groupGet, err := s.Groups.Get(ctx, "g1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(groupGet.MemberIDs) != 2 {
		t.Fatalf("members = %v, want deduplicated 2", groupGet.MemberIDs)
	}

	group.MemberIDs = []string{"a2"}
	if err := s.Groups.Upsert(ctx, group); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	groupGet, err = s.Groups.Get(ctx, "g1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(groupGet.MemberIDs) != 1 || groupGet.MemberIDs[0] != "a2" {
		t.Fatalf("members = %v, want [a2]", groupGet.MemberIDs)
	}

	if err := s.Groups.Delete(ctx, "g1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := s.Groups.Get(ctx, "g1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after delete error = %v, want ErrNotFound", err)
	}
	if err := s.Agents.Delete(ctx, "a1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := s.Agents.Get(ctx, "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after delete error = %v, want ErrNotFound", err)
	}
}

func TestApps(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	app := domain.App{
		ID: "app1", Name: "app1", DisplayName: "应用一", Version: "1.0.0",
		InstallDir: `D:\apps\app1`, Entry: "index.html", InstalledAt: 100, Tools: []domain.Tool{
			{Name: "a", Description: "A", Enabled: true},
			{Name: "b", Description: "B", Dangerous: true, Enabled: true},
		},
	}
	if err := s.Apps.Insert(ctx, app); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if err := s.Apps.Insert(ctx, app); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Insert() error = %v, want ErrConflict", err)
	}

	got, err := s.Apps.Get(ctx, "app1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.Tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(got.Tools))
	}
	if got.Entry != "index.html" || got.InstallDir != `D:\apps\app1` {
		t.Fatalf("entry/installDir = %q/%q", got.Entry, got.InstallDir)
	}

	if err := s.Apps.SetToolEnabled(ctx, "app1", "b", false); err != nil {
		t.Fatalf("SetToolEnabled() error = %v", err)
	}
	got, err = s.Apps.Get(ctx, "app1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Tools[1].Enabled {
		t.Fatal("tool b still enabled")
	}

	if err := s.Apps.ReplaceTools(ctx, "app1", []domain.Tool{{Name: "c", Enabled: true}}); err != nil {
		t.Fatalf("ReplaceTools() error = %v", err)
	}
	got, err = s.Apps.Get(ctx, "app1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "c" {
		t.Fatalf("tools = %+v, want [c]", got.Tools)
	}

	if err := s.Apps.SetDataSize(ctx, "app1", 2048); err != nil {
		t.Fatalf("SetDataSize() error = %v", err)
	}
	if err := s.Apps.MarkUsed(ctx, "app1", 999); err != nil {
		t.Fatalf("MarkUsed() error = %v", err)
	}
	got, err = s.Apps.Get(ctx, "app1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.DataSize != 2048 || got.LastUsedAt != 999 {
		t.Fatalf("app = %+v", got)
	}

	if err := s.Apps.Delete(ctx, "app1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	var tools int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM app_tool WHERE app_id = ?`, "app1").Scan(&tools); err != nil {
		t.Fatalf("count app_tool: %v", err)
	}
	if tools != 0 {
		t.Fatalf("app_tool rows = %d, want 0", tools)
	}
}

func TestPlugins(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	plugin := domain.Plugin{
		ID: "p1", Name: "插件一", Version: "1.0.0", InstalledAt: 100,
		Tools:    []domain.Tool{{Name: "run", Description: "Run"}},
		AgentIDs: []string{"a1", "a2", "a1"},
	}
	if err := s.Plugins.Insert(ctx, plugin); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	got, err := s.Plugins.Get(ctx, "p1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.Tools) != 1 || len(got.AgentIDs) != 2 {
		t.Fatalf("plugin = %+v", got)
	}

	if err := s.Plugins.SetGrants(ctx, "p1", []string{"a2"}); err != nil {
		t.Fatalf("SetGrants() error = %v", err)
	}
	got, err = s.Plugins.Get(ctx, "p1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.AgentIDs) != 1 || got.AgentIDs[0] != "a2" {
		t.Fatalf("grants = %v, want [a2]", got.AgentIDs)
	}

	if err := s.Plugins.ReplaceTools(ctx, "p1", []domain.Tool{{Name: "other", Dangerous: true}}); err != nil {
		t.Fatalf("ReplaceTools() error = %v", err)
	}
	got, err = s.Plugins.Get(ctx, "p1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "other" || !got.Tools[0].Dangerous {
		t.Fatalf("tools = %+v", got.Tools)
	}

	if err := s.Plugins.SetGrants(ctx, "missing", []string{"a1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetGrants(missing) error = %v, want ErrNotFound", err)
	}

	if err := s.Plugins.Delete(ctx, "p1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	var tools int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM plugin_tool WHERE plugin_id = ?`, "p1").Scan(&tools); err != nil {
		t.Fatalf("count plugin_tool: %v", err)
	}
	if tools != 0 {
		t.Fatalf("plugin_tool rows = %d, want 0", tools)
	}
}

func TestMeta(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.Meta.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
	if err := s.Meta.Set(ctx, "k", "v1"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := s.Meta.Set(ctx, "k", "v2"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got, err := s.Meta.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "v2" {
		t.Fatalf("value = %q, want v2", got)
	}
	if err := s.Meta.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := s.Meta.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after delete error = %v, want ErrNotFound", err)
	}
}

func TestClosed(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := s.Conversations.List(ctx, domain.KindAssistant, "main"); !errors.Is(err, ErrClosed) {
		t.Fatalf("List() error = %v, want ErrClosed", err)
	}
	if err := s.Tx(ctx, func(*Store) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("Tx() error = %v, want ErrClosed", err)
	}
}

func TestSeedDemo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	first, err := Open(context.Background(), Options{Path: path, Seed: true})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	ctx := context.Background()

	assistant, err := first.Conversations.List(ctx, domain.KindAssistant, "main")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(assistant) != 5 {
		t.Fatalf("assistant conversations = %d, want 5", len(assistant))
	}
	apps, err := first.Apps.List(ctx)
	if err != nil {
		t.Fatalf("Apps.List() error = %v", err)
	}
	if len(apps) != 5 {
		t.Fatalf("apps = %d, want 5", len(apps))
	}
	plugins, err := first.Plugins.List(ctx)
	if err != nil {
		t.Fatalf("Plugins.List() error = %v", err)
	}
	if len(plugins) != 5 {
		t.Fatalf("plugins = %d, want 5", len(plugins))
	}
	agents, err := first.Agents.List(ctx)
	if err != nil {
		t.Fatalf("Agents.List() error = %v", err)
	}
	if len(agents) != 4 {
		t.Fatalf("agents = %d, want 4", len(agents))
	}
	groups, err := first.Groups.List(ctx)
	if err != nil {
		t.Fatalf("Groups.List() error = %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	groupMessages, err := first.Messages.List(ctx, "group-weekly")
	if err != nil {
		t.Fatalf("Messages.List() error = %v", err)
	}
	if len(groupMessages) != 4 {
		t.Fatalf("group messages = %d, want 4", len(groupMessages))
	}

	if err := first.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	second, err := Open(context.Background(), Options{Path: path, Seed: true})
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer second.Close(ctx)

	assistant, err = second.Conversations.List(ctx, domain.KindAssistant, "main")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(assistant) != 5 {
		t.Fatalf("assistant conversations after reopen = %d, want 5", len(assistant))
	}
	version, err := second.Meta.Get(ctx, seedMetaKey)
	if err != nil {
		t.Fatalf("Meta.Get() error = %v", err)
	}
	if version != seedVersion {
		t.Fatalf("seed version = %q, want %q", version, seedVersion)
	}
}
