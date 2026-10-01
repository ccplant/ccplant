package kvstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/labels"
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
	columns, err := libSQLTableColumns(ctx, store.db, "agentapi_settings")
	if err != nil {
		t.Fatal(err)
	}
	if columns["kind"] {
		t.Fatal("dedicated resource table still contains redundant kind column")
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

func TestLibSQLStoreRemovesKindFromExistingDedicatedTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-kind.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacyDDL := `(
kind TEXT NOT NULL, namespace TEXT NOT NULL, key TEXT NOT NULL,
version INTEGER NOT NULL, value BLOB NOT NULL, updated_at TEXT NOT NULL,
owner_scope TEXT NOT NULL DEFAULT '',
metadata TEXT NOT NULL DEFAULT '{"format":"agentapi-kv-metadata/v1","labels":{}}' CHECK (json_valid(metadata)),
PRIMARY KEY (kind, namespace, key))`
	if _, err := db.Exec("CREATE TABLE agentapi_kv " + legacyDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE agentapi_webhooks " + legacyDDL); err != nil {
		t.Fatal(err)
	}
	metadata, err := marshalRecordMetadata(map[string]string{
		"agentapi.proxy/webhook":         "true",
		"agentapi.proxy/webhook-user-id": "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agentapi_webhooks
(kind, namespace, key, version, value, updated_at, metadata)
VALUES ('secret', 'ns', 'webhook', 4, '{}', '', ?)`, metadata); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewLibSQLStore(ctx, "file://"+path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	columns, err := libSQLTableColumns(ctx, store.db, "agentapi_webhooks")
	if err != nil {
		t.Fatal(err)
	}
	if columns["kind"] {
		t.Fatal("kind column was not removed")
	}
	got, err := store.Get(ctx, KindSecret, "ns", "webhook")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 4 || got.Labels["agentapi.proxy/webhook-user-id"] != "alice" {
		t.Fatalf("migrated record = %#v", got)
	}
	if _, err := store.Get(ctx, KindConfigMap, "ns", "webhook"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get with wrong kind error = %v, want ErrNotFound", err)
	}
}

func TestLibSQLStoreMaterializesIndexedQueryColumns(t *testing.T) {
	ctx := context.Background()
	store, err := NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "columns.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	recordLabels := map[string]string{
		"agentapi.proxy/session-route":                     "true",
		"agentapi.proxy/session-route-user-id":             "alice",
		"agentapi.proxy/session-route-team-id-hash":        "team-hash",
		"agentapi.proxy/session-route-scope":               "team",
		"agentapi.proxy/session-route-tag-slack_channel":   "C123",
		"agentapi.proxy/session-route-tag-slack_thread_ts": "123.456",
	}
	if _, err := store.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "route", Labels: recordLabels, Value: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	var userID, teamID, scope, channel, thread string
	if err := store.db.QueryRowContext(ctx, `SELECT user_principal_id, team_principal_id, resource_scope,
slack_channel, slack_thread_ts FROM agentapi_session_routes WHERE key = 'route'`).
		Scan(&userID, &teamID, &scope, &channel, &thread); err != nil {
		t.Fatal(err)
	}
	if userID != "alice" || teamID != "team-hash" || scope != "team" || channel != "C123" || thread != "123.456" {
		t.Fatalf("query columns = %q, %q, %q, %q, %q", userID, teamID, scope, channel, thread)
	}

	selector, err := labels.Parse("agentapi.proxy/session-route-user-id=alice,agentapi.proxy/session-route-scope=team")
	if err != nil {
		t.Fatal(err)
	}
	statement, _ := libSQLListQuery("agentapi_session_routes", Query{Kind: KindSecret, Namespace: "ns"}, selector)
	if strings.Contains(statement, "json_extract") || !strings.Contains(statement, "user_principal_id IN") || !strings.Contains(statement, "resource_scope IN") {
		t.Fatalf("query does not use materialized columns: %s", statement)
	}

	var indexCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index'
AND name IN ('agentapi_session_routes_user_principal_id_lookup', 'agentapi_session_routes_team_principal_id_lookup',
'agentapi_session_routes_resource_scope_lookup', 'agentapi_session_routes_slack_channel_lookup',
'agentapi_session_routes_slack_thread_ts_lookup')`).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 5 {
		t.Fatalf("route query indexes = %d, want 5", indexCount)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index'
AND name = 'agentapi_settings_user_principal_id_lookup'`).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 0 {
		t.Fatal("created an unused settings principal index")
	}
}

func TestLibSQLStoreBackfillsQueryColumnsFromLegacySchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-columns.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE agentapi_kv (
kind TEXT NOT NULL, namespace TEXT NOT NULL, key TEXT NOT NULL,
version INTEGER NOT NULL, value BLOB NOT NULL, updated_at TEXT NOT NULL,
owner_scope TEXT NOT NULL DEFAULT '', metadata TEXT NOT NULL,
PRIMARY KEY (kind, namespace, key))`); err != nil {
		t.Fatal(err)
	}
	metadata, err := marshalRecordMetadata(map[string]string{
		"agentapi.proxy/api-token":       "true",
		"agentapi.proxy/api-token-scope": "user",
		"agentapi.proxy/api-token-owner": "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agentapi_kv
(kind, namespace, key, version, value, updated_at, metadata) VALUES ('secret', 'ns', 'token', 1, '{}', '', ?)`, metadata); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewLibSQLStore(ctx, "file://"+path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	var owner, scope string
	if err := store.db.QueryRowContext(ctx, `SELECT owner_principal_id, resource_scope
FROM agentapi_api_tokens WHERE key = 'token'`).Scan(&owner, &scope); err != nil {
		t.Fatal(err)
	}
	if owner != "alice" || scope != "user" {
		t.Fatalf("backfilled owner/scope = %q/%q", owner, scope)
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
