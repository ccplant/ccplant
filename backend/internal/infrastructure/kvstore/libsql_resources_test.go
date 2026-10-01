package kvstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLibSQLStoreRoutesResourcesToDedicatedTables(t *testing.T) {
	ctx := context.Background()
	store, err := NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "resources.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	created, err := store.Create(ctx, Record{
		Kind: KindSecret, Namespace: "ns", Key: "settings-alice",
		Labels: map[string]string{"agentapi.proxy/settings": "true"}, Value: []byte(`{"setting":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var dedicated, fallback int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agentapi_settings`).Scan(&dedicated); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agentapi_kv`).Scan(&fallback); err != nil {
		t.Fatal(err)
	}
	if dedicated != 1 || fallback != 0 {
		t.Fatalf("dedicated rows = %d, fallback rows = %d", dedicated, fallback)
	}
	got, err := store.Get(ctx, KindSecret, "ns", "settings-alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != created.Version || string(got.Value) != string(created.Value) {
		t.Fatalf("Get() = %#v, want %#v", got, created)
	}
	listed, err := store.List(ctx, Query{Kind: KindSecret, Namespace: "ns", LabelSelector: "agentapi.proxy/settings=true"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Key != "settings-alice" {
		t.Fatalf("List() = %#v", listed)
	}
}

func TestLibSQLStoreMigratesClassifiedFallbackRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "migration.db")
	store, err := NewLibSQLStore(ctx, "file://"+path, "")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := marshalRecordMetadata(map[string]string{"agentapi.proxy/webhook": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO agentapi_kv
(kind, namespace, key, version, value, updated_at, owner_scope, metadata)
VALUES (?, ?, ?, 3, ?, '', '', ?)`, KindSecret, "ns", "legacy-webhook", []byte(`{"legacy":true}`), metadata); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = NewLibSQLStore(ctx, "file://"+path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	var dedicated, fallback int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agentapi_webhooks`).Scan(&dedicated); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agentapi_kv`).Scan(&fallback); err != nil {
		t.Fatal(err)
	}
	if dedicated != 1 || fallback != 0 {
		t.Fatalf("dedicated rows = %d, fallback rows = %d", dedicated, fallback)
	}
	got, err := store.Get(ctx, KindSecret, "ns", "legacy-webhook")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 {
		t.Fatalf("Version = %d, want 3", got.Version)
	}
}
