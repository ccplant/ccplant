package kvstore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/labels"
)

func TestLibSQLColdSessionRunnerUpdateRoutesFromKeyAndLabels(t *testing.T) {
	ctx := context.Background()
	store, err := NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "cold-update.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	record, err := store.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "agentapi-session-runner-update", Labels: map[string]string{
		"agentapi.proxy/session-runner-resource": "runner",
	}, Value: []byte("before")})
	if err != nil {
		t.Fatal(err)
	}
	store.tableLocations.Delete(libSQLLocationKey(record.Kind, record.Namespace, record.Key))
	record.Value = []byte("after")
	updated, err := store.Update(ctx, record)
	if err != nil || updated.Version != 2 {
		t.Fatalf("Update() = %#v, %v", updated, err)
	}
	got, err := store.Get(ctx, record.Kind, record.Namespace, record.Key)
	if err != nil || string(got.Value) != "after" {
		t.Fatalf("Get() = %#v, %v", got, err)
	}
}

func TestLibSQLColdSessionRunnerDeleteRoutesFromKey(t *testing.T) {
	ctx := context.Background()
	store, err := NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "cold-delete.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	record, err := store.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "agentapi-session-allocation-pool-delete", Labels: map[string]string{
		"agentapi.proxy/session-runner-resource": "allocation",
	}, Value: []byte("value")})
	if err != nil {
		t.Fatal(err)
	}
	store.tableLocations.Delete(libSQLLocationKey(record.Kind, record.Namespace, record.Key))
	if err := store.Delete(ctx, record.Kind, record.Namespace, record.Key, record.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, record.Kind, record.Namespace, record.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after Delete error = %v, want ErrNotFound", err)
	}
}

func TestLibSQLListQueryUsesKeyPrefixRange(t *testing.T) {
	selector := labels.Everything()
	statement, args := libSQLListQuery(libSQLFallbackTable, Query{
		Kind: KindSecret, Namespace: "ns", KeyPrefix: "agentapi-session-runner-allocation-",
	}, selector)
	if strings.Contains(statement, "substr(key") || !strings.Contains(statement, "key >= ? AND key < ?") {
		t.Fatalf("query does not use key range: %s", statement)
	}
	if got, want := args[len(args)-2], "agentapi-session-runner-allocation-"; got != want {
		t.Fatalf("lower bound = %v, want %v", got, want)
	}
	if got, want := args[len(args)-1], "agentapi-session-runner-allocation."; got != want {
		t.Fatalf("upper bound = %v, want %v", got, want)
	}
}

func TestLibSQLSessionRunnerResourcesUseDedicatedIndexedTable(t *testing.T) {
	ctx := context.Background()
	store, err := NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "runner.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	labels := map[string]string{
		"agentapi.proxy/session-runner-resource":  "allocation",
		"agentapi.proxy/session-runner-pool-hash": "pool-hash",
		"agentapi.proxy/session-runner-status":    "pending",
		"agentapi.proxy/session-runner-dind":      "false",
	}
	if _, err := store.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "allocation", Labels: labels, Value: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	var pool, status, dind string
	if err := store.db.QueryRowContext(ctx, `SELECT runner_pool, runner_status, runner_dind
FROM agentapi_session_allocations WHERE key = 'allocation'`).Scan(&pool, &status, &dind); err != nil {
		t.Fatal(err)
	}
	if pool != "pool-hash" || status != "pending" || dind != "false" {
		t.Fatalf("query columns = %q/%q/%q", pool, status, dind)
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
