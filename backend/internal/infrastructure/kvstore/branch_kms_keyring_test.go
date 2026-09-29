package kvstore

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/kms"
)

type countingBranchProvider struct {
	mu                         sync.Mutex
	name                       string
	encryptCalls, decryptCalls int
	plaintext                  map[string][]byte
}

func newCountingBranchProvider(name string) *countingBranchProvider {
	return &countingBranchProvider{name: name, plaintext: make(map[string][]byte)}
}
func (f *countingBranchProvider) Name() string { return f.name }
func (f *countingBranchProvider) Encrypt(_ context.Context, _ string, plaintext []byte, _ map[string]string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.encryptCalls++
	ciphertext := []byte(fmt.Sprintf("branch-%d", f.encryptCalls))
	f.plaintext[string(ciphertext)] = append([]byte(nil), plaintext...)
	return ciphertext, nil
}
func (f *countingBranchProvider) Decrypt(_ context.Context, _ string, ciphertext []byte, _ map[string]string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decryptCalls++
	plaintext, ok := f.plaintext[string(ciphertext)]
	if !ok {
		return nil, ErrDecrypt
	}
	return append([]byte(nil), plaintext...), nil
}

type directKMS struct{ *countingBranchProvider }

func (f *directKMS) GenerateDataKey(ctx context.Context, input *kms.GenerateDataKeyInput, _ ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error) {
	plaintext := bytes.Repeat([]byte{42}, dataKeySize)
	ciphertext, err := f.countingBranchProvider.Encrypt(ctx, "", plaintext, input.EncryptionContext)
	return &kms.GenerateDataKeyOutput{Plaintext: plaintext, CiphertextBlob: ciphertext}, err
}
func (f *directKMS) Encrypt(ctx context.Context, input *kms.EncryptInput, _ ...func(*kms.Options)) (*kms.EncryptOutput, error) {
	ciphertext, err := f.countingBranchProvider.Encrypt(ctx, "", input.Plaintext, input.EncryptionContext)
	return &kms.EncryptOutput{CiphertextBlob: ciphertext}, err
}
func (f *directKMS) Decrypt(ctx context.Context, input *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	plaintext, err := f.countingBranchProvider.Decrypt(ctx, "", input.CiphertextBlob, input.EncryptionContext)
	return &kms.DecryptOutput{Plaintext: plaintext}, err
}

func TestPersistentBranchKMSKeyringAmortizesCallsAcrossRecordsAndProcesses(t *testing.T) {
	ctx := context.Background()
	provider := newCountingBranchProvider("aws-kms")
	registry := NewMemoryBranchKeyRegistry()
	keys := map[string]string{"current": "arn:aws:kms:region:account:key/current"}
	writerKeyring, err := newPersistentBranchKMSKeyring("current", keys, provider, registry, nil, time.Hour, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	backend := newMemoryStore()
	writer, err := NewEncryptedStore(backend, writerKeyring)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string][]byte{"one": secretDocument(t, "one", nil, "secret-one"), "two": secretDocument(t, "two", nil, "secret-two")}
	for key, value := range values {
		if _, err := writer.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: key, Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	if provider.encryptCalls != 1 || provider.decryptCalls != 0 {
		t.Fatalf("initial KMS calls: encrypt=%d decrypt=%d, want 1/0", provider.encryptCalls, provider.decryptCalls)
	}

	readerKeyring, err := newPersistentBranchKMSKeyring("current", keys, provider, registry, nil, time.Hour, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewEncryptedStore(backend, readerKeyring)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range values {
		got, err := reader.Get(ctx, KindSecret, "ns", key)
		if err != nil || !bytes.Equal(got.Value, want) {
			t.Fatalf("Get(%s) value=%q err=%v", key, got.Value, err)
		}
	}
	if provider.encryptCalls != 1 || provider.decryptCalls != 1 {
		t.Fatalf("post-restart KMS calls: encrypt=%d decrypt=%d, want 1/1", provider.encryptCalls, provider.decryptCalls)
	}
}

func TestScopedBranchKMSKeyringSeparatesOwnersAndReadsLegacy(t *testing.T) {
	ctx := context.Background()
	provider := newCountingBranchProvider("aws-kms")
	registry := NewMemoryBranchKeyRegistry()
	keys := map[string]string{"current": "arn:aws:kms:region:account:key/current"}
	backend := newMemoryStore()

	legacyKeyring, err := newPersistentBranchKMSKeyring("current", keys, provider, registry, nil, time.Hour, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	legacyStore, _ := NewEncryptedStore(backend, legacyKeyring)
	legacyValue := secretDocument(t, "legacy", map[string]string{"agentapi.proxy/settings-name": "legacy-user"}, "legacy-secret")
	if _, err := legacyStore.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "legacy", Value: legacyValue}); err != nil {
		t.Fatal(err)
	}

	scopedKeyring, err := newPersistentBranchKMSKeyring("current", keys, provider, registry, nil, time.Hour, 8, true)
	if err != nil {
		t.Fatal(err)
	}
	scopedStore, _ := NewEncryptedStore(backend, scopedKeyring)
	for _, owner := range []string{"alice", "bob"} {
		value := secretDocument(t, owner, map[string]string{"agentapi.proxy/settings-name": owner}, "secret-"+owner)
		if _, err := scopedStore.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: owner, Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	if provider.encryptCalls != 3 { // one legacy branch plus one branch per owner
		t.Fatalf("KMS encrypt calls = %d, want 3", provider.encryptCalls)
	}
	for _, key := range []string{"legacy", "alice", "bob"} {
		if _, err := scopedStore.Get(ctx, KindSecret, "ns", key); err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
	}
	aliceEnvelope, err := parseEnvelope(backend.records[recordKey(KindSecret, "ns", "alice")].Value)
	if err != nil {
		t.Fatal(err)
	}
	aliceBranch, recognized, err := parseBranchWrappedDEK(aliceEnvelope.WrappedDEK)
	if err != nil || !recognized || aliceBranch.Scope == "" {
		t.Fatalf("scoped branch = %#v recognized=%t err=%v", aliceBranch, recognized, err)
	}
	compatibleReader, _ := newPersistentBranchKMSKeyring("current", keys, provider, registry, nil, time.Hour, 8, false)
	compatibleStore, _ := NewEncryptedStore(backend, compatibleReader)
	if _, err := compatibleStore.Get(ctx, KindSecret, "ns", "alice"); err != nil {
		t.Fatalf("legacy-configured reader did not accept v3: %v", err)
	}
}

func TestScopedBranchKMSKeyringRewrapsLegacyWithoutChangingCiphertext(t *testing.T) {
	ctx := context.Background()
	provider := newCountingBranchProvider("aws-kms")
	registry := NewMemoryBranchKeyRegistry()
	keys := map[string]string{"current": "arn:aws:kms:region:account:key/current"}
	backend := newMemoryStore()
	legacy, _ := newPersistentBranchKMSKeyring("current", keys, provider, registry, nil, time.Hour, 8, false)
	legacyStore, _ := NewEncryptedStore(backend, legacy)
	value := secretDocument(t, "item", map[string]string{"agentapi.proxy/settings-name": "alice"}, "secret")
	if _, err := legacyStore.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "item", Value: value}); err != nil {
		t.Fatal(err)
	}
	before, _ := parseEnvelope(backend.records[recordKey(KindSecret, "ns", "item")].Value)

	scoped, _ := newPersistentBranchKMSKeyring("current", keys, provider, registry, nil, time.Hour, 8, true)
	result, err := RewrapAll(ctx, backend, scoped, "ns", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rewrapped != 1 {
		t.Fatalf("rewrapped = %d, want 1", result.Rewrapped)
	}
	after, _ := parseEnvelope(backend.records[recordKey(KindSecret, "ns", "item")].Value)
	if !bytes.Equal(before.Ciphertext, after.Ciphertext) || !bytes.Equal(before.Nonce, after.Nonce) {
		t.Fatal("rewrap changed value ciphertext")
	}
	branch, recognized, err := parseBranchWrappedDEK(after.WrappedDEK)
	if err != nil || !recognized || branch.Scope == "" {
		t.Fatalf("rewrapped branch = %#v recognized=%t err=%v", branch, recognized, err)
	}
}

func TestCloudProviderUsesSamePersistentBranchArchitecture(t *testing.T) {
	provider := newCountingBranchProvider("cloud-kms")
	registry := NewMemoryBranchKeyRegistry()
	keyring, err := newPersistentBranchKMSKeyring("current", map[string]string{"current": "projects/p/locations/l/keyRings/r/cryptoKeys/k"}, provider, registry, nil, time.Hour, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := keyring.GenerateDataKey(context.Background(), Record{Kind: KindSecret, Namespace: "ns", Key: "key"}); err != nil {
		t.Fatal(err)
	}
	record, err := registry.GetActiveBranchKey(context.Background(), "cloud-kms", "current", "")
	if err != nil {
		t.Fatal(err)
	}
	if record.Generation != 1 || provider.encryptCalls != 1 {
		t.Fatalf("generation=%d encryptCalls=%d", record.Generation, provider.encryptCalls)
	}
}

func TestLibSQLBranchKeyRegistryPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	url := "file://" + filepath.Join(t.TempDir(), "branch-keys.db")
	store, err := NewLibSQLStore(ctx, url, "")
	if err != nil {
		t.Fatal(err)
	}
	want := BranchKeyRecord{Provider: "aws-kms", KeyID: "current", Generation: 1,
		KMSKeyRef: "arn:aws:kms:region:account:key/current", WrappedKey: []byte("ciphertext"), CreatedAt: time.Now().UTC()}
	if err := store.CreateActiveBranchKey(ctx, want); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = NewLibSQLStore(ctx, url, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	got, err := store.GetActiveBranchKey(ctx, want.Provider, want.KeyID, want.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != want.Generation || got.KMSKeyRef != want.KMSKeyRef || !bytes.Equal(got.WrappedKey, want.WrappedKey) {
		t.Fatalf("persisted branch key = %#v, want %#v", got, want)
	}
}

func TestLibSQLBranchKeyRegistryMigratesLegacySchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-branch-keys.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE agentapi_kv_branch_keys (
provider TEXT NOT NULL, key_id TEXT NOT NULL, generation INTEGER NOT NULL,
kms_key_ref TEXT NOT NULL, wrapped_key BLOB NOT NULL, status TEXT NOT NULL,
created_at TEXT NOT NULL, PRIMARY KEY (provider, key_id, generation))`)
	if err == nil {
		_, err = db.Exec(`INSERT INTO agentapi_kv_branch_keys VALUES
('aws-kms','current',1,'arn:legacy',X'01','active','2026-01-01T00:00:00Z')`)
	}
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	store, err := NewLibSQLStore(ctx, "file://"+path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	legacy, err := store.GetActiveBranchKey(ctx, "aws-kms", "current", "")
	if err != nil || legacy.Generation != 1 {
		t.Fatalf("legacy record after migration = %#v, err=%v", legacy, err)
	}
	for _, scope := range []string{"owner:a", "owner:b"} {
		if err := store.CreateActiveBranchKey(ctx, BranchKeyRecord{Provider: "aws-kms", KeyID: "current", Scope: scope,
			Generation: 1, KMSKeyRef: "arn:new", WrappedKey: []byte(scope), CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("create scope %s after migration: %v", scope, err)
		}
	}
}

func TestBranchKMSKeyringReadsDirectKMSValues(t *testing.T) {
	ctx := context.Background()
	client := &directKMS{newCountingBranchProvider("aws-kms")}
	keys := map[string]string{"current": "arn:aws:kms:region:account:key/current"}
	backend := newMemoryStore()
	direct, err := NewEncryptedStore(backend, &KMSKeyring{activeID: "current", keys: keys, client: client})
	if err != nil {
		t.Fatal(err)
	}
	value := secretDocument(t, "legacy-direct", nil, "secret")
	if _, err := direct.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "legacy-direct", Value: value}); err != nil {
		t.Fatal(err)
	}
	branchKeyring, err := newPersistentBranchKMSKeyring("current", keys, &awsBranchKMSProvider{client: client}, NewMemoryBranchKeyRegistry(), &KMSKeyring{activeID: "current", keys: keys, client: client}, time.Hour, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	branch, err := NewEncryptedStore(backend, branchKeyring)
	if err != nil {
		t.Fatal(err)
	}
	got, err := branch.Get(ctx, KindSecret, "ns", "legacy-direct")
	if err != nil || !bytes.Equal(got.Value, value) {
		t.Fatalf("read direct KMS value: value=%q err=%v", got.Value, err)
	}
}
