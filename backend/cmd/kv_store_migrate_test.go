package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCollectApplicationKVRecordsIncludesAllAgentAPIOwnedResources(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test", Labels: map[string]string{"agentapi.proxy/settings": "true"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "logical-pool", Namespace: "test", Labels: map[string]string{"agentapi.proxy/session-runner-resource": "logical-pool"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "subscriptions", Namespace: "test", Labels: map[string]string{"app.kubernetes.io/component": "notification-subscription", "agentapi.proxy/user-id": "user"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "future-secret", Namespace: "test", Labels: map[string]string{"agentapi.proxy/future-resource": "v1"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "github-connection", Namespace: "test", Labels: map[string]string{"agentapi.ccplant.io/github-connection": "true"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "github-identity", Namespace: "test", Labels: map[string]string{"agentapi.ccplant.io/github-identity": "true"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "github-principal", Namespace: "test", Labels: map[string]string{"agentapi.ccplant.io/github-principal": "true"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "github-oauth-state", Namespace: "test", Labels: map[string]string{"agentapi.ccplant.io/github-oauth-state": "true"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "github-broker-lease", Namespace: "test", Labels: map[string]string{"agentapi.ccplant.io/github-broker-lease": "true"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "helm-release", Namespace: "test", Labels: map[string]string{"owner": "helm"}}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "memory", Namespace: "test", Labels: map[string]string{"agentapi.proxy/type": "memory"}}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-shares", Namespace: "test"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "future-config", Namespace: "test", Labels: map[string]string{"agentapi.proxy/future-resource": "v1"}}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "server-config", Namespace: "test", Labels: map[string]string{"app.kubernetes.io/name": "agentapi-proxy"}}},
	)

	records, err := collectApplicationKVRecords(context.Background(), client, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 10 {
		t.Fatalf("got %d records, want 10: %#v", len(records), records)
	}
	got := make(map[string]bool, len(records))
	for _, record := range records {
		got[string(record.Kind)+"/"+record.Key] = true
	}
	for _, identity := range []string{
		"configmap/agentapi-session-shares", "configmap/future-config", "configmap/memory",
		"secret/future-secret", "secret/github-connection", "secret/github-identity", "secret/github-principal",
		"secret/logical-pool", "secret/settings", "secret/subscriptions",
	} {
		if !got[identity] {
			t.Errorf("missing application record %s: %#v", identity, records)
		}
	}
	for _, identity := range []string{
		"secret/github-broker-lease", "secret/github-oauth-state", "secret/helm-release", "configmap/server-config",
	} {
		if got[identity] {
			t.Errorf("included non-application record %s", identity)
		}
	}
}

func TestMigrateKubernetesKVIsIdempotent(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test", Labels: map[string]string{"agentapi.proxy/settings": "true"}}, Data: map[string][]byte{"settings.json": []byte(`{"name":"user"}`)}},
	)
	store := newMemoryKVStore()
	options := kvStoreMigrateOptions{namespace: "test"}

	first, err := migrateKubernetesKV(context.Background(), client, store, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Copied != 1 || first.Skipped != 0 {
		t.Fatalf("unexpected first result: %#v", first)
	}
	second, err := migrateKubernetesKV(context.Background(), client, store, options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Copied != 0 || second.Skipped != 1 {
		t.Fatalf("unexpected second result: %#v", second)
	}
}

func TestMigrateKubernetesKVConflictAndOverwrite(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "memory", Namespace: "test", Labels: map[string]string{"agentapi.proxy/type": "memory"}}, Data: map[string]string{"memory.json": "source"}},
	)
	store := newMemoryKVStore()
	_, err := store.Create(context.Background(), kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: "test", Key: "memory", Value: []byte("destination")})
	if err != nil {
		t.Fatal(err)
	}

	result, err := migrateKubernetesKV(context.Background(), client, store, kvStoreMigrateOptions{namespace: "test"})
	if err == nil || result.Conflicts != 1 {
		t.Fatalf("expected one conflict, got result=%#v err=%v", result, err)
	}
	result, err = migrateKubernetesKV(context.Background(), client, store, kvStoreMigrateOptions{namespace: "test", overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 {
		t.Fatalf("expected one update, got %#v", result)
	}
}

func TestMigrateKubernetesKVDryRunDoesNotWrite(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-schedules", Namespace: "test"}},
	)
	store := newMemoryKVStore()
	result, err := migrateKubernetesKV(context.Background(), client, store, kvStoreMigrateOptions{namespace: "test", dryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 || result.Entries[0].Status != "would-copy" {
		t.Fatalf("unexpected dry-run result: %#v", result)
	}
	if _, err := store.Get(context.Background(), kvstore.KindSecret, "test", "agentapi-schedules"); !errors.Is(err, kvstore.ErrNotFound) {
		t.Fatalf("dry-run wrote a record: %v", err)
	}
}

func TestMigrateKubernetesKVToLocalLibSQLFile(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test", Labels: map[string]string{"agentapi.proxy/settings": "true"}}, Data: map[string][]byte{"settings.json": []byte(`{"name":"local-e2e"}`)}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "memory", Namespace: "test", Labels: map[string]string{"agentapi.proxy/type": "memory"}}, Data: map[string]string{"memory.json": `{"title":"migrate me"}`}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "notification-subscriptions-user", Namespace: "test", Labels: map[string]string{"app.kubernetes.io/component": "notification-subscription"}}},
	)
	databaseURL := "file://" + filepath.Join(t.TempDir(), "migration.db")
	store, err := kvstore.NewLibSQLStore(ctx, databaseURL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	result, err := migrateKubernetesKV(ctx, client, store, kvStoreMigrateOptions{namespace: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != 2 || result.Copied != 2 {
		t.Fatalf("unexpected migration result: %#v", result)
	}
	for _, identity := range []struct {
		kind kvstore.Kind
		key  string
	}{{kvstore.KindSecret, "settings"}, {kvstore.KindConfigMap, "memory"}} {
		if _, err := store.Get(ctx, identity.kind, "test", identity.key); err != nil {
			t.Fatalf("read migrated %s/%s: %v", identity.kind, identity.key, err)
		}
	}
	if _, err := store.Get(ctx, kvstore.KindSecret, "test", "notification-subscriptions-user"); !errors.Is(err, kvstore.ErrNotFound) {
		t.Fatalf("non-AgentAPI-owned Secret was migrated: %v", err)
	}
	second, err := migrateKubernetesKV(ctx, client, store, kvStoreMigrateOptions{namespace: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Skipped != 2 {
		t.Fatalf("expected idempotent skips, got %#v", second)
	}
}

func TestMigrateKubernetesKVEncryptsEveryDedicatedResourceTable(t *testing.T) {
	ctx := context.Background()
	resources := []struct {
		table, name, labelKey, labelValue string
	}{
		{"agentapi_settings", "settings", "agentapi.proxy/settings", "true"},
		{"agentapi_credentials", "credentials", "agentapi.proxy/credentials", "true"},
		{"agentapi_shares", "shares", "agentapi.proxy/shares", "true"},
		{"agentapi_team_configs", "team-config", "agentapi.proxy/team-config", "true"},
		{"agentapi_personal_api_keys", "personal-api-key", "agentapi.proxy/personal-api-key", "true"},
		{"agentapi_api_tokens", "api-token", "agentapi.proxy/api-token", "true"},
		{"agentapi_local_users", "local-user", "agentapi.proxy/local-user", "true"},
		{"agentapi_sandbox_policies", "sandbox-policy", "agentapi.proxy/type", "sandbox-policy"},
		{"agentapi_sandbox_domains", "sandbox-domains", "agentapi.proxy/type", "sandbox-domains"},
		{"agentapi_session_routes", "session-route", "agentapi.proxy/session-route", "true"},
		{"agentapi_user_files", "user-files", "agentapi.proxy/user-files", "true"},
		{"agentapi_session_profiles", "session-profile", "agentapi.proxy/session-profile", "true"},
		{"agentapi_slackbots", "slackbot", "agentapi.proxy/slackbot", "true"},
		{"agentapi_webhooks", "webhook", "agentapi.proxy/webhook", "true"},
		{"agentapi_user_team_mappings", "user-team-mapping", "agentapi.proxy/type", "user-team-mapping"},
		{"agentapi_codex_auth_attempts", "codex-auth", "agentapi.proxy/codex-device-auth-attempt", "true"},
		{"agentapi_codex_auth_locks", "codex-auth-lock", "agentapi.proxy/codex-device-auth-attempt", "lock"},
		{"agentapi_schedules", "schedule", "agentapi.proxy/schedule", "true"},
		{"agentapi_system_settings", "agentapi-admin-system-settings-test", "agentapi.proxy/system-settings", "true"},
	}
	objects := make([]*corev1.Secret, 0, len(resources))
	for _, resource := range resources {
		objects = append(objects, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: resource.name, Namespace: "source", Labels: map[string]string{resource.labelKey: resource.labelValue}},
			Data:       map[string][]byte{"payload": []byte("plaintext-" + resource.table)},
		})
	}
	client := fake.NewSimpleClientset()
	for _, object := range objects {
		if _, err := client.CoreV1().Secrets("source").Create(ctx, object, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	databasePath := filepath.Join(t.TempDir(), "encrypted-migration.db")
	backend, err := kvstore.NewLibSQLStore(ctx, "file://"+databasePath, "")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := encryptedMigrationDestination(ctx, backend, true, "local", "migration-key", "", `{"migration-key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := migrateKubernetesKV(ctx, client, destination, kvStoreMigrateOptions{namespace: "source", destinationNamespace: "destination"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != len(resources) || result.Copied != len(resources) {
		t.Fatalf("migration result = %#v, want %d encrypted copies", result, len(resources))
	}

	for _, resource := range resources {
		got, err := destination.Get(ctx, kvstore.KindSecret, "destination", resource.name)
		if err != nil {
			t.Fatalf("decrypt migrated %s: %v", resource.table, err)
		}
		var secret corev1.Secret
		if err := json.Unmarshal(got.Value, &secret); err != nil {
			t.Fatal(err)
		}
		if string(secret.Data["payload"]) != "plaintext-"+resource.table {
			t.Fatalf("%s decrypted payload = %q", resource.table, secret.Data["payload"])
		}
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, resource := range resources {
		var raw []byte
		query := "SELECT value FROM " + resource.table + " WHERE namespace = ? AND key = ?"
		if err := db.QueryRowContext(ctx, query, "destination", resource.name).Scan(&raw); err != nil {
			t.Fatalf("read raw %s value: %v", resource.table, err)
		}
		var envelope struct {
			Format string `json:"format"`
			KeyID  string `json:"key_id"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("%s stored plaintext/non-envelope value: %v", resource.table, err)
		}
		if envelope.Format != "agentapi-kv-envelope/v1" || envelope.KeyID != "migration-key" {
			t.Fatalf("%s envelope = %#v", resource.table, envelope)
		}
	}
	var fallbackCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM agentapi_kv WHERE namespace = ?", "destination").Scan(&fallbackCount); err != nil {
		t.Fatal(err)
	}
	if fallbackCount != 0 {
		t.Fatalf("fallback contains %d migrated dedicated resources", fallbackCount)
	}
}

func TestEncryptedMigrationDestinationRejectsUnsupportedProvider(t *testing.T) {
	store := newMemoryKVStore()
	_, err := encryptedMigrationDestination(context.Background(), store, true, "unknown-kms", "active", "", `{"active":"key-ref"}`)
	if err == nil || !strings.Contains(err.Error(), "unsupported KV encryption provider") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

func TestEncryptedMigrationDestinationRequiresKeysForEncryptedBackend(t *testing.T) {
	store := newMemoryKVStore()
	_, err := encryptedMigrationDestination(context.Background(), store, true, "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "libsql-encrypted destination requires") {
		t.Fatalf("expected missing encryption configuration error, got %v", err)
	}
	plain, err := encryptedMigrationDestination(context.Background(), store, false, "", "", "", "")
	if err != nil || plain != store {
		t.Fatalf("plain destination = %#v, err=%v", plain, err)
	}
}

func TestBuildMigrationStoreAcceptsEncryptedLibSQLBackend(t *testing.T) {
	store, err := buildMigrationStore(context.Background(), migrationStoreConfig{
		backend: "libsql-encrypted", databaseURL: "file://" + filepath.Join(t.TempDir(), "encrypted.db"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
}

func TestMigrateConfiguredStorePair(t *testing.T) {
	ctx := context.Background()
	source, destination := newMemoryKVStore(), newMemoryKVStore()
	value := []byte(`{"metadata":{"name":"memory","namespace":"test","labels":{"agentapi.proxy/type":"memory"}},"data":{"memory.json":"source"}}`)
	if _, err := source.Create(ctx, kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: "test", Key: "memory", Value: value}); err != nil {
		t.Fatal(err)
	}
	result, err := migrateKVStores(ctx, source, destination, kvStoreMigrateOptions{namespace: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 {
		t.Fatalf("result = %#v", result)
	}
	got, err := destination.Get(ctx, kvstore.KindConfigMap, "test", "memory")
	if err != nil || string(got.Value) != string(value) {
		t.Fatalf("destination record = %#v, err=%v", got, err)
	}
}

func TestMigrateConfiguredStorePairCanonicalizesStaleLabels(t *testing.T) {
	ctx := context.Background()
	source, backend := newMemoryKVStore(), newMemoryKVStore()
	value := []byte(`{"metadata":{"name":"pool","namespace":"test","labels":{"agentapi.proxy/session-runner-resource":"logical-pool"}},"data":{}}`)
	if _, err := source.Create(ctx, kvstore.Record{
		Kind: kvstore.KindSecret, Namespace: "test", Key: "pool", Value: value,
		Labels: map[string]string{"agentapi.proxy/session-runner-resource": "stale"},
	}); err != nil {
		t.Fatal(err)
	}
	keyring, err := kvstore.NewLocalKeyring("active", map[string]string{
		"active": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	if err != nil {
		t.Fatal(err)
	}
	destination, err := kvstore.NewEncryptedStore(backend, keyring)
	if err != nil {
		t.Fatal(err)
	}
	result, err := migrateKVStores(ctx, source, destination, kvStoreMigrateOptions{namespace: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 {
		t.Fatalf("result = %#v", result)
	}
	got, err := destination.Get(ctx, kvstore.KindSecret, "test", "pool")
	if err != nil {
		t.Fatal(err)
	}
	if got.Labels["agentapi.proxy/session-runner-resource"] != "logical-pool" {
		t.Fatalf("labels = %#v", got.Labels)
	}
}

func TestMigrateConfiguredStorePairRewritesDestinationNamespace(t *testing.T) {
	ctx := context.Background()
	source, destination := newMemoryKVStore(), newMemoryKVStore()
	value := []byte(`{"metadata":{"name":"memory","namespace":"logical","labels":{"agentapi.proxy/type":"memory"}},"data":{"memory.json":"source"}}`)
	if _, err := source.Create(ctx, kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: "logical", Key: "memory", Value: value}); err != nil {
		t.Fatal(err)
	}
	result, err := migrateKVStores(ctx, source, destination, kvStoreMigrateOptions{namespace: "logical", destinationNamespace: "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 || result.Entries[0].Namespace != "runtime" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := destination.Get(ctx, kvstore.KindConfigMap, "runtime", "memory"); err != nil {
		t.Fatalf("destination record: %v", err)
	}
	if _, err := destination.Get(ctx, kvstore.KindConfigMap, "logical", "memory"); !errors.Is(err, kvstore.ErrNotFound) {
		t.Fatalf("logical namespace record err=%v, want not found", err)
	}
}

func TestMigrationStoreConfigsUseReplicatedEnvironment(t *testing.T) {
	options := kvStoreMigrateOptions{
		primaryBackend:       "kubernetes",
		secondaryBackend:     "libsql",
		secondaryDatabaseURL: "http://libsql:8080",
		secondaryAuthToken:   "token",
	}
	primary, secondary, err := options.storeConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if primary.backend != "kubernetes" || secondary.backend != "libsql" || secondary.databaseURL != "http://libsql:8080" || secondary.authToken != "token" {
		t.Fatalf("primary=%#v secondary=%#v", primary, secondary)
	}
}

func TestMigrationStoreConfigsPreserveLegacyFlags(t *testing.T) {
	primary, secondary, err := (kvStoreMigrateOptions{legacyDatabaseURL: "file:///tmp/legacy.db", legacyAuthToken: "token"}).storeConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if primary.backend != "kubernetes" || secondary.backend != "libsql" || secondary.databaseURL != "file:///tmp/legacy.db" {
		t.Fatalf("primary=%#v secondary=%#v", primary, secondary)
	}
}

type memoryKVStore struct {
	records map[string]kvstore.Record
}

func newMemoryKVStore() *memoryKVStore { return &memoryKVStore{records: map[string]kvstore.Record{}} }
func (s *memoryKVStore) Close() error  { return nil }

func memoryKVKey(kind kvstore.Kind, namespace, key string) string {
	return string(kind) + "/" + namespace + "/" + key
}

func (s *memoryKVStore) Create(_ context.Context, record kvstore.Record) (kvstore.Record, error) {
	key := memoryKVKey(record.Kind, record.Namespace, record.Key)
	if _, ok := s.records[key]; ok {
		return kvstore.Record{}, kvstore.ErrConflict
	}
	record.Version = 1
	s.records[key] = record
	return record, nil
}

func (s *memoryKVStore) Update(_ context.Context, record kvstore.Record) (kvstore.Record, error) {
	key := memoryKVKey(record.Kind, record.Namespace, record.Key)
	existing, ok := s.records[key]
	if !ok || existing.Version != record.Version {
		return kvstore.Record{}, kvstore.ErrConflict
	}
	record.Version++
	s.records[key] = record
	return record, nil
}

func (s *memoryKVStore) Get(_ context.Context, kind kvstore.Kind, namespace, key string) (kvstore.Record, error) {
	record, ok := s.records[memoryKVKey(kind, namespace, key)]
	if !ok {
		return kvstore.Record{}, kvstore.ErrNotFound
	}
	return record, nil
}

func (s *memoryKVStore) Delete(_ context.Context, kind kvstore.Kind, namespace, key string, version int64) error {
	mapKey := memoryKVKey(kind, namespace, key)
	record, ok := s.records[mapKey]
	if !ok {
		return kvstore.ErrNotFound
	}
	if record.Version != version {
		return kvstore.ErrConflict
	}
	delete(s.records, mapKey)
	return nil
}

func (s *memoryKVStore) List(_ context.Context, query kvstore.Query) ([]kvstore.Record, error) {
	return s.recordsFor(query.Kind, query.Namespace), nil
}

func (s *memoryKVStore) Scan(_ context.Context, query kvstore.ScanQuery) ([]kvstore.Record, error) {
	return s.recordsFor(query.Kind, query.Namespace), nil
}

func (s *memoryKVStore) recordsFor(kind kvstore.Kind, namespace string) []kvstore.Record {
	var records []kvstore.Record
	for _, record := range s.records {
		if record.Kind == kind && record.Namespace == namespace {
			records = append(records, record)
		}
	}
	return records
}
