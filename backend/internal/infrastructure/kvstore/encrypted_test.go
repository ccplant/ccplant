package kvstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEncryptedStoreRoundTripAndMetadataFilter(t *testing.T) {
	ctx := context.Background()
	backend, err := NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "encrypted.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	store := encryptedTestStore(t, backend, "current", map[string]string{"current": randomEncodedKey(t)})
	t.Cleanup(func() { _ = store.Close() })

	userValue := secretDocument(t, "user", map[string]string{"scope": "user"}, "top-secret-user-value")
	teamValue := secretDocument(t, "team", map[string]string{"scope": "team"}, "top-secret-team-value")
	if _, err := store.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "user", Labels: map[string]string{"scope": "user"}, Value: userValue}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "team", Labels: map[string]string{"scope": "team"}, Value: teamValue}); err != nil {
		t.Fatal(err)
	}

	raw, err := backend.Get(ctx, KindSecret, "ns", "user")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw.Value, []byte("top-secret-user-value")) {
		t.Fatal("libSQL value contains plaintext")
	}
	if raw.Labels["scope"] != "user" {
		t.Fatalf("stored labels = %#v", raw.Labels)
	}

	records, err := store.List(ctx, Query{Kind: KindSecret, Namespace: "ns", LabelSelector: "scope=user"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Key != "user" || !bytes.Equal(records[0].Value, userValue) {
		t.Fatalf("filtered records = %#v", records)
	}
}

func TestLibSQLBackfillsMetadataForLegacyRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE agentapi_kv (
kind TEXT NOT NULL, namespace TEXT NOT NULL, key TEXT NOT NULL,
version INTEGER NOT NULL, value BLOB NOT NULL, updated_at TEXT NOT NULL,
PRIMARY KEY (kind, namespace, key))`); err != nil {
		t.Fatal(err)
	}
	value := secretDocument(t, "legacy", map[string]string{"scope": "user"}, "secret")
	if _, err := db.Exec(`INSERT INTO agentapi_kv(kind, namespace, key, version, value, updated_at) VALUES(?, ?, ?, 1, ?, '')`, KindSecret, "ns", "legacy", value); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewLibSQLStore(ctx, "file://"+path, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	records, err := store.List(ctx, Query{Kind: KindSecret, Namespace: "ns", LabelSelector: "scope=user"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Key != "legacy" {
		t.Fatalf("backfilled records = %#v", records)
	}
}

func TestEncryptedStoreDetectsIdentityAndMetadataTampering(t *testing.T) {
	ctx := context.Background()
	backend := newMemoryStore()
	store := encryptedTestStore(t, backend, "current", map[string]string{"current": randomEncodedKey(t)})
	value := secretDocument(t, "source", map[string]string{"scope": "user"}, "secret")
	if _, err := store.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "source", Labels: map[string]string{"scope": "user"}, Value: value}); err != nil {
		t.Fatal(err)
	}

	raw := backend.records[recordKey(KindSecret, "ns", "source")]
	raw.Key = "moved"
	backend.records[recordKey(KindSecret, "ns", "moved")] = raw
	if _, err := store.Get(ctx, KindSecret, "ns", "moved"); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("identity tamper error = %v", err)
	}

	raw = backend.records[recordKey(KindSecret, "ns", "source")]
	raw.Labels = map[string]string{"scope": "team"}
	backend.records[recordKey(KindSecret, "ns", "source")] = raw
	if _, err := store.Get(ctx, KindSecret, "ns", "source"); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("metadata tamper error = %v", err)
	}
}

func TestEncryptedStoreReadsOldKeyAndWritesActiveKey(t *testing.T) {
	ctx := context.Background()
	backend := newMemoryStore()
	oldKey, newKey := randomEncodedKey(t), randomEncodedKey(t)
	oldStore := encryptedTestStore(t, backend, "old", map[string]string{"old": oldKey})
	value := secretDocument(t, "item", nil, "secret")
	created, err := oldStore.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "item", Value: value})
	if err != nil {
		t.Fatal(err)
	}

	rotatedStore := encryptedTestStore(t, backend, "new", map[string]string{"old": oldKey, "new": newKey})
	got, err := rotatedStore.Get(ctx, KindSecret, "ns", "item")
	if err != nil || !bytes.Equal(got.Value, value) {
		t.Fatalf("read with old key: value=%q err=%v", got.Value, err)
	}
	got.Version = created.Version
	if _, err := rotatedStore.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	raw := backend.records[recordKey(KindSecret, "ns", "item")]
	envelope, err := parseEnvelope(raw.Value)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.KeyID != "new" {
		t.Fatalf("updated key ID = %q, want new", envelope.KeyID)
	}
}

func TestEncryptedStoreRejectsPlaintext(t *testing.T) {
	ctx := context.Background()
	backend := newMemoryStore()
	value := secretDocument(t, "legacy", map[string]string{"scope": "user"}, "legacy-secret")
	_, err := backend.Create(ctx, Record{
		Kind: KindSecret, Namespace: "ns", Key: "legacy",
		Labels: map[string]string{"scope": "user"}, Value: value,
	})
	if err != nil {
		t.Fatal(err)
	}
	keyring, err := NewLocalKeyring("current", map[string]string{"current": randomEncodedKey(t)})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewEncryptedStore(backend, keyring)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get(ctx, KindSecret, "ns", "legacy"); !errors.Is(err, ErrPlaintextInEncryptedStore) {
		t.Fatalf("plaintext mismatch error = %v", err)
	}
}

func TestPlaintextLibSQLStoreRejectsEncryptedValue(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "format-mismatch.db")
	backend, err := NewLibSQLStore(ctx, "file://"+path, "")
	if err != nil {
		t.Fatal(err)
	}
	store := encryptedTestStore(t, backend, "current", map[string]string{"current": randomEncodedKey(t)})
	value := secretDocument(t, "encrypted", nil, "secret")
	if _, err := store.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "encrypted", Value: value}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	plaintextStore, err := NewLibSQLStore(ctx, "file://"+path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer plaintextStore.Close()
	if _, err := plaintextStore.Get(ctx, KindSecret, "ns", "encrypted"); !errors.Is(err, ErrEncryptedInPlaintextStore) {
		t.Fatalf("encrypted mismatch error = %v", err)
	}
}

func TestEncryptedStoreRejectsMalformedEnvelope(t *testing.T) {
	ctx := context.Background()
	backend := newMemoryStore()
	value := []byte(`{"format":"agentapi-kv-envelope/v1","format":"legacy","metadata":{"labels":{}},"data":{"value":"plaintext"}}`)
	if _, err := backend.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "malformed", Value: value}); err != nil {
		t.Fatal(err)
	}
	keyring, err := NewLocalKeyring("current", map[string]string{"current": randomEncodedKey(t)})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewEncryptedStore(backend, keyring)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, KindSecret, "ns", "malformed"); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("malformed envelope error = %v", err)
	}
	if !bytes.Equal(backend.records[recordKey(KindSecret, "ns", "malformed")].Value, value) {
		t.Fatal("malformed envelope was rewritten as legacy plaintext")
	}
}

func TestRewrapAllPreservesCiphertextAndRemovesOldKeyDependency(t *testing.T) {
	ctx := context.Background()
	backend := newMemoryStore()
	oldKey, newKey := randomEncodedKey(t), randomEncodedKey(t)
	oldStore := encryptedTestStore(t, backend, "old", map[string]string{"old": oldKey})
	value := secretDocument(t, "item", map[string]string{"scope": "user"}, "secret")
	if _, err := oldStore.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "item", Value: value}); err != nil {
		t.Fatal(err)
	}
	before, err := parseEnvelope(backend.records[recordKey(KindSecret, "ns", "item")].Value)
	if err != nil {
		t.Fatal(err)
	}
	keyring, err := NewLocalKeyring("new", map[string]string{"old": oldKey, "new": newKey})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RewrapAll(ctx, backend, keyring, "ns", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != 1 || result.Rewrapped != 1 || result.Skipped != 0 {
		t.Fatalf("rewrap result = %#v", result)
	}
	after, err := parseEnvelope(backend.records[recordKey(KindSecret, "ns", "item")].Value)
	if err != nil {
		t.Fatal(err)
	}
	if after.KeyID != "new" || !bytes.Equal(before.Ciphertext, after.Ciphertext) || !bytes.Equal(before.Nonce, after.Nonce) {
		t.Fatal("rewrap changed encrypted document or did not select the new key")
	}
	newStore := encryptedTestStore(t, backend, "new", map[string]string{"new": newKey})
	got, err := newStore.Get(ctx, KindSecret, "ns", "item")
	if err != nil || !bytes.Equal(got.Value, value) {
		t.Fatalf("read after removing old key: value=%q err=%v", got.Value, err)
	}
}

func TestParseEnvelopeRejectsDuplicateAndUnknownFields(t *testing.T) {
	for _, input := range []string{
		`{"format":"agentapi-kv-envelope/v1","format":"agentapi-kv-envelope/v1","key_id":"k","wrapped_dek":"AA==","nonce":"AA==","ciphertext":"AA=="}`,
		`{"format":"agentapi-kv-envelope/v1","key_id":"k","wrapped_dek":"AA==","nonce":"AA==","ciphertext":"AA==","algorithm":"noop"}`,
	} {
		if _, err := parseEnvelope([]byte(input)); err == nil {
			t.Fatalf("parseEnvelope(%s) succeeded", input)
		}
	}
}

func encryptedTestStore(t *testing.T, backend Store, activeID string, keys map[string]string) Store {
	t.Helper()
	keyring, err := NewLocalKeyring(activeID, keys)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewEncryptedStore(backend, keyring)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func randomEncodedKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, dataKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func secretDocument(t *testing.T, name string, labels map[string]string, value string) []byte {
	t.Helper()
	document, err := json.Marshal(corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Data:       map[string][]byte{"value": []byte(value)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return document
}
