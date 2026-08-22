package kvstore

import (
	"bytes"
	"context"
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
	writerKeyring, err := newPersistentBranchKMSKeyring("current", keys, provider, registry, nil, time.Hour, 8)
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

	readerKeyring, err := newPersistentBranchKMSKeyring("current", keys, provider, registry, nil, time.Hour, 8)
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

func TestCloudProviderUsesSamePersistentBranchArchitecture(t *testing.T) {
	provider := newCountingBranchProvider("cloud-kms")
	registry := NewMemoryBranchKeyRegistry()
	keyring, err := newPersistentBranchKMSKeyring("current", map[string]string{"current": "projects/p/locations/l/keyRings/r/cryptoKeys/k"}, provider, registry, nil, time.Hour, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := keyring.GenerateDataKey(context.Background(), Record{Kind: KindSecret, Namespace: "ns", Key: "key"}); err != nil {
		t.Fatal(err)
	}
	record, err := registry.GetActiveBranchKey(context.Background(), "cloud-kms", "current")
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
	defer store.Close()
	got, err := store.GetActiveBranchKey(ctx, want.Provider, want.KeyID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != want.Generation || got.KMSKeyRef != want.KMSKeyRef || !bytes.Equal(got.WrappedKey, want.WrappedKey) {
		t.Fatalf("persisted branch key = %#v, want %#v", got, want)
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
	branchKeyring, err := newPersistentBranchKMSKeyring("current", keys, &awsBranchKMSProvider{client: client}, NewMemoryBranchKeyRegistry(), &KMSKeyring{activeID: "current", keys: keys, client: client}, time.Hour, 8)
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
