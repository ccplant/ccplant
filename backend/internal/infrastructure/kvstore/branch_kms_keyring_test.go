package kvstore

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/kms"
)

type countingKMS struct {
	mu            sync.Mutex
	generateCalls int
	encryptCalls  int
	decryptCalls  int
	plaintext     map[string][]byte
}

func newCountingKMS() *countingKMS { return &countingKMS{plaintext: make(map[string][]byte)} }

func (f *countingKMS) GenerateDataKey(_ context.Context, _ *kms.GenerateDataKeyInput, _ ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.generateCalls++
	plain := bytes.Repeat([]byte{byte(f.generateCalls)}, dataKeySize)
	ciphertext := []byte(fmt.Sprintf("generated-%d", f.generateCalls))
	f.plaintext[string(ciphertext)] = append([]byte(nil), plain...)
	return &kms.GenerateDataKeyOutput{Plaintext: plain, CiphertextBlob: ciphertext}, nil
}

func (f *countingKMS) Encrypt(_ context.Context, input *kms.EncryptInput, _ ...func(*kms.Options)) (*kms.EncryptOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.encryptCalls++
	ciphertext := []byte(fmt.Sprintf("encrypted-%d", f.encryptCalls))
	f.plaintext[string(ciphertext)] = append([]byte(nil), input.Plaintext...)
	return &kms.EncryptOutput{CiphertextBlob: ciphertext}, nil
}

func (f *countingKMS) Decrypt(_ context.Context, input *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decryptCalls++
	plain, ok := f.plaintext[string(input.CiphertextBlob)]
	if !ok {
		return nil, fmt.Errorf("unknown ciphertext")
	}
	return &kms.DecryptOutput{Plaintext: append([]byte(nil), plain...)}, nil
}

func TestBranchKMSKeyringAmortizesKMSCallsAcrossRecords(t *testing.T) {
	ctx := context.Background()
	client := newCountingKMS()
	keys := map[string]string{"current": "arn:aws:kms:region:account:key/current"}
	writerKeyring, err := newBranchKMSKeyring("current", keys, client, time.Hour, 8)
	if err != nil {
		t.Fatal(err)
	}
	backend := newMemoryStore()
	writer, err := NewEncryptedStore(backend, writerKeyring)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string][]byte{
		"one": secretDocument(t, "one", nil, "secret-one"),
		"two": secretDocument(t, "two", nil, "secret-two"),
	}
	for key, value := range values {
		if _, err := writer.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: key, Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	if client.generateCalls != 1 {
		t.Fatalf("GenerateDataKey calls = %d, want 1", client.generateCalls)
	}

	// A fresh process decrypts the shared branch key once, then serves all
	// records from its bounded branch-key cache.
	readerKeyring, err := newBranchKMSKeyring("current", keys, client, time.Hour, 8)
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
	if client.decryptCalls != 1 {
		t.Fatalf("Decrypt calls = %d, want 1", client.decryptCalls)
	}
}

func TestBranchKMSKeyringReadsDirectKMSValues(t *testing.T) {
	ctx := context.Background()
	client := newCountingKMS()
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
	branchKeyring, err := newBranchKMSKeyring("current", keys, client, time.Hour, 8)
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
