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

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

const (
	branchWrappedDEKFormatV1 = "agentapi-kv-branch-wrapped-dek/v1"
	branchWrappedDEKFormatV2 = "agentapi-kv-branch-wrapped-dek/v2"
)

type branchWrappedDEKJSON struct {
	Format           string `json:"format"`
	Provider         string `json:"provider,omitempty"`
	KMSKeyRef        string `json:"kms_key_ref,omitempty"`
	Generation       int64  `json:"generation,omitempty"`
	WrappedBranchKey string `json:"wrapped_branch_key"`
	WrappedDEK       string `json:"wrapped_dek"`
}

type branchWrappedDEK struct {
	Provider, KMSKeyRef string
	Generation          int64
	WrappedBranchKey    []byte
	WrappedDEK          []byte
	LegacyV1            bool
}

type cachedBranchKey struct {
	plaintext []byte
	expiresAt time.Time
}

// BranchKMSKeyring persists only a KMS-wrapped branch key. Instances share its
// active generation and wrap unique per-record DEKs locally.
type BranchKMSKeyring struct {
	activeID   string
	keys       map[string]string
	provider   branchKMSProvider
	registry   BranchKeyRegistry
	direct     *KMSKeyring
	cacheTTL   time.Duration
	cacheMax   int
	now        func() time.Time
	mu         sync.Mutex
	active     cachedBranchKey
	activeMeta BranchKeyRecord
	cache      map[[32]byte]cachedBranchKey
}

func NewBranchKMSKeyring(ctx context.Context, activeID, region string, keys map[string]string, registry BranchKeyRegistry, cacheTTL time.Duration, cacheMax int) (*BranchKMSKeyring, error) {
	if registry == nil {
		return nil, errors.New("branch key registry is required")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration for KV branch encryption: %w", err)
	}
	client := kms.NewFromConfig(cfg)
	return newPersistentBranchKMSKeyring(activeID, keys, &awsBranchKMSProvider{client: client}, registry,
		&KMSKeyring{activeID: activeID, keys: keys, client: client}, cacheTTL, cacheMax)
}

func NewCloudBranchKMSKeyring(ctx context.Context, activeID string, keys map[string]string, registry BranchKeyRegistry, cacheTTL time.Duration, cacheMax int) (*BranchKMSKeyring, error) {
	if registry == nil {
		return nil, errors.New("branch key registry is required")
	}
	provider, err := newCloudKMSProvider(ctx)
	if err != nil {
		return nil, err
	}
	return newPersistentBranchKMSKeyring(activeID, keys, provider, registry, nil, cacheTTL, cacheMax)
}

func newPersistentBranchKMSKeyring(activeID string, keys map[string]string, provider branchKMSProvider, registry BranchKeyRegistry, direct *KMSKeyring, cacheTTL time.Duration, cacheMax int) (*BranchKMSKeyring, error) {
	if provider == nil || registry == nil {
		return nil, errors.New("KMS provider and branch key registry are required")
	}
	if _, ok := keys[activeID]; activeID == "" || !ok {
		return nil, fmt.Errorf("active KV encryption key %q is not in the KMS keyring", activeID)
	}
	for id, keyRef := range keys {
		if id == "" || len(id) > 128 || keyRef == "" {
			return nil, fmt.Errorf("invalid KMS keyring entry %q", id)
		}
	}
	if cacheTTL <= 0 {
		cacheTTL = 15 * time.Minute
	}
	if cacheMax <= 0 {
		cacheMax = 128
	}
	return &BranchKMSKeyring{activeID: activeID, keys: keys, provider: provider, registry: registry, direct: direct,
		cacheTTL: cacheTTL, cacheMax: cacheMax, now: time.Now, cache: make(map[[32]byte]cachedBranchKey)}, nil
}

func (k *BranchKMSKeyring) ActiveKeyID() string { return k.activeID }

func (k *BranchKMSKeyring) GenerateDataKey(ctx context.Context, record Record) ([]byte, []byte, error) {
	branch, metadata, err := k.activeBranch(ctx)
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
	encoded, err := marshalBranchWrappedDEK(metadata, wrapped)
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
	branch, metadata, err := k.activeBranch(ctx)
	if err != nil {
		return nil, err
	}
	defer clear(branch)
	wrapped, err := wrapDEK(branch, dek, record)
	if err != nil {
		return nil, err
	}
	return marshalBranchWrappedDEK(metadata, wrapped)
}

func (k *BranchKMSKeyring) UnwrapDataKey(ctx context.Context, keyID string, wrapped []byte, record Record) ([]byte, error) {
	value, recognized, err := parseBranchWrappedDEK(wrapped)
	if err != nil {
		return nil, ErrDecrypt
	}
	if !recognized {
		if k.direct == nil {
			return nil, ErrDecrypt
		}
		return k.direct.UnwrapDataKey(ctx, keyID, wrapped, record)
	}
	if expected, ok := k.keys[keyID]; !ok || (!value.LegacyV1 && (value.Provider != k.provider.Name() || value.KMSKeyRef != expected)) {
		return nil, ErrDecrypt
	}
	branch, err := k.branchForCiphertext(ctx, keyID, value)
	if err != nil {
		return nil, ErrDecrypt
	}
	defer clear(branch)
	return unwrapDEK(branch, value.WrappedDEK, record)
}

func (k *BranchKMSKeyring) activeBranch(ctx context.Context) ([]byte, BranchKeyRecord, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	if len(k.active.plaintext) == dataKeySize && now.Before(k.active.expiresAt) {
		return append([]byte(nil), k.active.plaintext...), cloneBranchKeyRecord(k.activeMeta), nil
	}
	k.clearActiveLocked()
	record, err := k.registry.GetActiveBranchKey(ctx, k.provider.Name(), k.activeID)
	var plaintext []byte
	if errors.Is(err, ErrBranchKeyNotFound) {
		record, plaintext, err = k.createPersistentBranch(ctx, now)
	}
	if err != nil {
		return nil, BranchKeyRecord{}, err
	}
	if record.KMSKeyRef != k.keys[k.activeID] {
		return nil, BranchKeyRecord{}, ErrDecrypt
	}
	if plaintext == nil {
		plaintext, err = k.provider.Decrypt(ctx, record.KMSKeyRef, record.WrappedKey, persistentBranchContext(record))
		if err != nil {
			return nil, BranchKeyRecord{}, err
		}
		log.Printf("[KV_ENCRYPTION] loaded persistent %s branch key generation %d for key ID %q", record.Provider, record.Generation, record.KeyID)
	}
	k.active = cachedBranchKey{plaintext: plaintext, expiresAt: now.Add(k.cacheTTL)}
	k.activeMeta = cloneBranchKeyRecord(record)
	k.putCacheLocked(record.Provider, record.KMSKeyRef, record.Generation, record.WrappedKey, k.active)
	return append([]byte(nil), plaintext...), cloneBranchKeyRecord(record), nil
}

func (k *BranchKMSKeyring) createPersistentBranch(ctx context.Context, now time.Time) (BranchKeyRecord, []byte, error) {
	generation, err := k.registry.NextBranchKeyGeneration(ctx, k.provider.Name(), k.activeID)
	if err != nil {
		return BranchKeyRecord{}, nil, err
	}
	plaintext := make([]byte, dataKeySize)
	if _, err := io.ReadFull(rand.Reader, plaintext); err != nil {
		return BranchKeyRecord{}, nil, fmt.Errorf("generate branch key: %w", err)
	}
	record := BranchKeyRecord{Provider: k.provider.Name(), KeyID: k.activeID, Generation: generation,
		KMSKeyRef: k.keys[k.activeID], CreatedAt: now.UTC()}
	record.WrappedKey, err = k.provider.Encrypt(ctx, record.KMSKeyRef, plaintext, persistentBranchContext(record))
	if err != nil {
		clear(plaintext)
		return BranchKeyRecord{}, nil, err
	}
	if err := k.registry.CreateActiveBranchKey(ctx, record); err != nil {
		clear(plaintext)
		winner, getErr := k.registry.GetActiveBranchKey(ctx, k.provider.Name(), k.activeID)
		if getErr != nil {
			return BranchKeyRecord{}, nil, errors.Join(err, getErr)
		}
		return winner, nil, nil
	}
	log.Printf("[KV_ENCRYPTION] created persistent %s branch key generation %d for key ID %q", record.Provider, record.Generation, record.KeyID)
	return record, plaintext, nil
}

func (k *BranchKMSKeyring) branchForCiphertext(ctx context.Context, keyID string, value branchWrappedDEK) ([]byte, error) {
	provider, keyRef, generation := value.Provider, value.KMSKeyRef, value.Generation
	contextValues := persistentBranchContext(BranchKeyRecord{Provider: provider, KeyID: keyID, Generation: generation, KMSKeyRef: keyRef})
	if value.LegacyV1 {
		if k.provider.Name() != "aws-kms" {
			return nil, ErrDecrypt
		}
		provider, keyRef, generation = "aws-kms", k.keys[keyID], 0
		contextValues = legacyBranchKMSContext(keyID)
	}
	hash := branchCacheHash(provider, keyRef, generation, value.WrappedBranchKey)
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
	plaintext, err := k.provider.Decrypt(ctx, keyRef, value.WrappedBranchKey, contextValues)
	if err != nil {
		return nil, err
	}
	entry := cachedBranchKey{plaintext: plaintext, expiresAt: now.Add(k.cacheTTL)}
	k.putCacheLocked(provider, keyRef, generation, value.WrappedBranchKey, entry)
	clear(plaintext)
	log.Printf("[KV_ENCRYPTION] decrypted %s branch key generation %d after cache miss for key ID %q", provider, generation, keyID)
	return append([]byte(nil), k.cache[hash].plaintext...), nil
}

func (k *BranchKMSKeyring) putCacheLocked(provider, keyRef string, generation int64, wrapped []byte, entry cachedBranchKey) {
	hash := branchCacheHash(provider, keyRef, generation, wrapped)
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
	k.cache[hash] = cachedBranchKey{plaintext: append([]byte(nil), entry.plaintext...), expiresAt: entry.expiresAt}
}

func branchCacheHash(provider, keyRef string, generation int64, wrapped []byte) [32]byte {
	input, _ := json.Marshal(struct {
		Provider   string `json:"provider"`
		KeyRef     string `json:"key_ref"`
		Generation int64  `json:"generation"`
		Wrapped    string `json:"wrapped"`
	}{provider, keyRef, generation, base64.StdEncoding.EncodeToString(wrapped)})
	return sha256.Sum256(input)
}

func (k *BranchKMSKeyring) clearActiveLocked() {
	if len(k.active.plaintext) > 0 {
		clear(k.active.plaintext)
	}
	k.active = cachedBranchKey{}
	k.activeMeta = BranchKeyRecord{}
}

func (k *BranchKMSKeyring) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.clearActiveLocked()
	for hash, entry := range k.cache {
		clear(entry.plaintext)
		delete(k.cache, hash)
	}
}

func marshalBranchWrappedDEK(branch BranchKeyRecord, wrappedDEK []byte) ([]byte, error) {
	return json.Marshal(branchWrappedDEKJSON{Format: branchWrappedDEKFormatV2, Provider: branch.Provider,
		KMSKeyRef: branch.KMSKeyRef, Generation: branch.Generation,
		WrappedBranchKey: base64.StdEncoding.EncodeToString(branch.WrappedKey),
		WrappedDEK:       base64.StdEncoding.EncodeToString(wrappedDEK)})
}

func parseBranchWrappedDEK(data []byte) (branchWrappedDEK, bool, error) {
	var marker struct {
		Format string `json:"format"`
	}
	if json.Unmarshal(data, &marker) != nil || (marker.Format != branchWrappedDEKFormatV1 && marker.Format != branchWrappedDEKFormatV2) {
		return branchWrappedDEK{}, false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var raw branchWrappedDEKJSON
	if decoder.Decode(&raw) != nil || (raw.Format != branchWrappedDEKFormatV1 && raw.Format != branchWrappedDEKFormatV2) {
		return branchWrappedDEK{}, true, ErrDecrypt
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return branchWrappedDEK{}, true, ErrDecrypt
	}
	wrappedBranch, err := base64.StdEncoding.Strict().DecodeString(raw.WrappedBranchKey)
	if err != nil || len(wrappedBranch) == 0 {
		return branchWrappedDEK{}, true, ErrDecrypt
	}
	wrappedDEK, err := base64.StdEncoding.Strict().DecodeString(raw.WrappedDEK)
	if err != nil || len(wrappedDEK) == 0 {
		return branchWrappedDEK{}, true, ErrDecrypt
	}
	legacy := raw.Format == branchWrappedDEKFormatV1
	if !legacy && (raw.Provider == "" || raw.KMSKeyRef == "" || raw.Generation < 1) {
		return branchWrappedDEK{}, true, ErrDecrypt
	}
	return branchWrappedDEK{Provider: raw.Provider, KMSKeyRef: raw.KMSKeyRef, Generation: raw.Generation,
		WrappedBranchKey: wrappedBranch, WrappedDEK: wrappedDEK, LegacyV1: legacy}, true, nil
}

func persistentBranchContext(record BranchKeyRecord) map[string]string {
	return map[string]string{"application": "agentapi-kv", "purpose": "persistent-branch-key",
		"provider": record.Provider, "key_id": record.KeyID, "generation": fmt.Sprintf("%d", record.Generation)}
}

func legacyBranchKMSContext(keyID string) map[string]string {
	return map[string]string{"application": "agentapi-kv", "purpose": "branch-key", "key_id": keyID}
}

func cloneBranchKeyRecord(record BranchKeyRecord) BranchKeyRecord {
	record.WrappedKey = append([]byte(nil), record.WrappedKey...)
	return record
}
