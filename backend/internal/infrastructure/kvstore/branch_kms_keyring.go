package kvstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
)

const branchWrappedDEKFormat = "agentapi-kv-branch-wrapped-dek/v1"

type branchWrappedDEKJSON struct {
	Format           string `json:"format"`
	WrappedBranchKey string `json:"wrapped_branch_key"`
	WrappedDEK       string `json:"wrapped_dek"`
}

type cachedBranchKey struct {
	plaintext []byte
	expiresAt time.Time
}

// BranchKMSKeyring uses KMS for a short-lived branch key and wraps unique
// per-record DEKs locally. One KMS call is needed per branch key and cache TTL,
// rather than one call per record operation.
type BranchKMSKeyring struct {
	activeID  string
	keys      map[string]string
	client    kmsAPI
	cacheTTL  time.Duration
	cacheMax  int
	now       func() time.Time
	mu        sync.Mutex
	active    cachedBranchKey
	activeRaw []byte
	cache     map[[32]byte]cachedBranchKey
	direct    *KMSKeyring
}

func NewBranchKMSKeyring(ctx context.Context, activeID, region string, keys map[string]string, cacheTTL time.Duration, cacheMax int) (*BranchKMSKeyring, error) {
	direct, err := NewKMSKeyring(ctx, activeID, region, keys)
	if err != nil {
		return nil, err
	}
	return newBranchKMSKeyring(activeID, keys, direct.client, cacheTTL, cacheMax)
}

func newBranchKMSKeyring(activeID string, keys map[string]string, client kmsAPI, cacheTTL time.Duration, cacheMax int) (*BranchKMSKeyring, error) {
	if client == nil {
		return nil, errors.New("KMS client is required")
	}
	if _, ok := keys[activeID]; activeID == "" || !ok {
		return nil, fmt.Errorf("active KV encryption key %q is not in the KMS keyring", activeID)
	}
	if cacheTTL <= 0 {
		cacheTTL = 15 * time.Minute
	}
	if cacheMax <= 0 {
		cacheMax = 128
	}
	direct := &KMSKeyring{activeID: activeID, keys: keys, client: client}
	return &BranchKMSKeyring{
		activeID: activeID, keys: keys, client: client, cacheTTL: cacheTTL,
		cacheMax: cacheMax, now: time.Now, cache: make(map[[32]byte]cachedBranchKey), direct: direct,
	}, nil
}

func (k *BranchKMSKeyring) ActiveKeyID() string { return k.activeID }

func (k *BranchKMSKeyring) GenerateDataKey(ctx context.Context, record Record) ([]byte, []byte, error) {
	branch, wrappedBranch, err := k.activeBranch(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer clear(branch)
	dek := make([]byte, dataKeySize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, nil, fmt.Errorf("generate KV data key: %w", err)
	}
	wrapped, err := wrapDEK(branch, dek, record)
	if err != nil {
		clear(dek)
		return nil, nil, err
	}
	encoded, err := marshalBranchWrappedDEK(wrappedBranch, wrapped)
	if err != nil {
		clear(dek)
		return nil, nil, err
	}
	return dek, encoded, nil
}

func (k *BranchKMSKeyring) WrapDataKey(ctx context.Context, keyID string, dek []byte, record Record) ([]byte, error) {
	if keyID != k.activeID {
		return nil, ErrDecrypt
	}
	branch, wrappedBranch, err := k.activeBranch(ctx)
	if err != nil {
		return nil, err
	}
	defer clear(branch)
	wrapped, err := wrapDEK(branch, dek, record)
	if err != nil {
		return nil, err
	}
	return marshalBranchWrappedDEK(wrappedBranch, wrapped)
}

func (k *BranchKMSKeyring) UnwrapDataKey(ctx context.Context, keyID string, wrapped []byte, record Record) ([]byte, error) {
	wrappedBranch, wrappedDEK, recognized, err := parseBranchWrappedDEK(wrapped)
	if err != nil {
		return nil, ErrDecrypt
	}
	if !recognized {
		// Compatibility with values written by the direct aws-kms provider.
		return k.direct.UnwrapDataKey(ctx, keyID, wrapped, record)
	}
	branch, err := k.branchForCiphertext(ctx, keyID, wrappedBranch)
	if err != nil {
		return nil, ErrDecrypt
	}
	defer clear(branch)
	return unwrapDEK(branch, wrappedDEK, record)
}

func (k *BranchKMSKeyring) activeBranch(ctx context.Context) ([]byte, []byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	if len(k.active.plaintext) == dataKeySize && now.Before(k.active.expiresAt) {
		return append([]byte(nil), k.active.plaintext...), append([]byte(nil), k.activeRaw...), nil
	}
	if len(k.active.plaintext) > 0 {
		clear(k.active.plaintext)
	}
	output, err := k.client.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId: aws.String(k.keys[k.activeID]), KeySpec: types.DataKeySpecAes256,
		EncryptionContext: branchKMSContext(k.activeID),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("generate KMS branch key: %w", err)
	}
	if output == nil || len(output.Plaintext) != dataKeySize || len(output.CiphertextBlob) == 0 {
		if output != nil {
			clear(output.Plaintext)
		}
		return nil, nil, errors.New("KMS returned an invalid branch key")
	}
	k.active = cachedBranchKey{plaintext: output.Plaintext, expiresAt: now.Add(k.cacheTTL)}
	k.activeRaw = append(k.activeRaw[:0], output.CiphertextBlob...)
	k.putCacheLocked(k.activeID, k.activeRaw, k.active)
	log.Printf("[KV_ENCRYPTION] generated KMS branch key for key ID %q", k.activeID)
	return append([]byte(nil), k.active.plaintext...), append([]byte(nil), k.activeRaw...), nil
}

func (k *BranchKMSKeyring) branchForCiphertext(ctx context.Context, keyID string, wrapped []byte) ([]byte, error) {
	keyARN, ok := k.keys[keyID]
	if !ok {
		return nil, ErrDecrypt
	}
	hash := branchCacheHash(keyID, wrapped)
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	if entry, ok := k.cache[hash]; ok && now.Before(entry.expiresAt) {
		return append([]byte(nil), entry.plaintext...), nil
	}
	if entry, ok := k.cache[hash]; ok {
		clear(entry.plaintext)
		delete(k.cache, hash)
	}
	output, err := k.client.Decrypt(ctx, &kms.DecryptInput{
		KeyId: aws.String(keyARN), CiphertextBlob: wrapped,
		EncryptionContext: branchKMSContext(keyID),
	})
	if err != nil || output == nil || len(output.Plaintext) != dataKeySize {
		if output != nil {
			clear(output.Plaintext)
		}
		return nil, ErrDecrypt
	}
	entry := cachedBranchKey{plaintext: output.Plaintext, expiresAt: now.Add(k.cacheTTL)}
	k.putCacheLocked(keyID, wrapped, entry)
	clear(output.Plaintext)
	log.Printf("[KV_ENCRYPTION] decrypted KMS branch key after cache miss for key ID %q", keyID)
	return append([]byte(nil), k.cache[hash].plaintext...), nil
}

func (k *BranchKMSKeyring) putCacheLocked(keyID string, wrapped []byte, entry cachedBranchKey) {
	hash := branchCacheHash(keyID, wrapped)
	if old, ok := k.cache[hash]; ok && !bytes.Equal(old.plaintext, entry.plaintext) {
		clear(old.plaintext)
	}
	for len(k.cache) >= k.cacheMax {
		for candidate, old := range k.cache {
			clear(old.plaintext)
			delete(k.cache, candidate)
			break
		}
	}
	k.cache[hash] = cachedBranchKey{
		plaintext: append([]byte(nil), entry.plaintext...),
		expiresAt: entry.expiresAt,
	}
}

func branchCacheHash(keyID string, wrapped []byte) [32]byte {
	input := make([]byte, 0, len(keyID)+1+len(wrapped))
	input = append(input, keyID...)
	input = append(input, 0)
	input = append(input, wrapped...)
	return sha256.Sum256(input)
}

func (k *BranchKMSKeyring) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.active.plaintext) > 0 {
		clear(k.active.plaintext)
	}
	for hash, entry := range k.cache {
		clear(entry.plaintext)
		delete(k.cache, hash)
	}
	k.activeRaw = nil
}

func marshalBranchWrappedDEK(wrappedBranch, wrappedDEK []byte) ([]byte, error) {
	return json.Marshal(branchWrappedDEKJSON{
		Format: branchWrappedDEKFormat,
		WrappedBranchKey: base64.StdEncoding.EncodeToString(wrappedBranch),
		WrappedDEK: base64.StdEncoding.EncodeToString(wrappedDEK),
	})
}

func parseBranchWrappedDEK(data []byte) (wrappedBranch, wrappedDEK []byte, recognized bool, err error) {
	var marker struct{ Format string `json:"format"` }
	if json.Unmarshal(data, &marker) != nil || marker.Format != branchWrappedDEKFormat {
		return nil, nil, false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var raw branchWrappedDEKJSON
	if decoder.Decode(&raw) != nil || raw.Format != branchWrappedDEKFormat {
		return nil, nil, true, ErrDecrypt
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, nil, true, ErrDecrypt
	}
	branch, err := base64.StdEncoding.Strict().DecodeString(raw.WrappedBranchKey)
	if err != nil || len(branch) == 0 {
		return nil, nil, true, ErrDecrypt
	}
	dek, err := base64.StdEncoding.Strict().DecodeString(raw.WrappedDEK)
	if err != nil || len(dek) == 0 {
		return nil, nil, true, ErrDecrypt
	}
	return branch, dek, true, nil
}

func branchKMSContext(keyID string) map[string]string {
	return map[string]string{"application": "agentapi-kv", "purpose": "branch-key", "key_id": keyID}
}
