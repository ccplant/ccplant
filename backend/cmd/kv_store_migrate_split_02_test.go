package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

func TestOnlineMigrationDoesNotRecreateRecordDeletedAfterScan(t *testing.T) {
	ctx := context.Background()
	backend := newMemoryKVStore()
	value := []byte(`{"metadata":{"name":"memory","namespace":"test","labels":{"agentapi.proxy/type":"memory"}},"data":{"memory.json":"source"}}`)
	created, err := backend.Create(ctx, kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: "test", Key: "memory", Value: value})
	if err != nil {
		t.Fatal(err)
	}
	source := &deleteAfterScanStore{memoryKVStore: backend, delete: created}
	destination := newMemoryKVStore()

	result, err := migrateKVStores(ctx, source, destination, kvStoreMigrateOptions{namespace: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 || len(result.Entries) != 1 || result.Entries[0].Status != "source-deleted" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := destination.Get(ctx, created.Kind, created.Namespace, created.Key); !errors.Is(err, kvstore.ErrNotFound) {
		t.Fatalf("deleted source was recreated in destination: %v", err)
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

type deleteAfterScanStore struct {
	*memoryKVStore
	delete kvstore.Record
	done   bool
}

func (s *deleteAfterScanStore) Scan(ctx context.Context, query kvstore.ScanQuery) ([]kvstore.Record, error) {
	records, err := s.memoryKVStore.Scan(ctx, query)
	if err == nil && !s.done && query.Kind == s.delete.Kind {
		s.done = true
		_ = s.memoryKVStore.Delete(ctx, s.delete.Kind, s.delete.Namespace, s.delete.Key, s.delete.Version)
	}
	return records, err
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
