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
	branchWrappedDEKFormatV3 = "agentapi-kv-branch-wrapped-dek/v3"
	legacyBranchScope        = ""
)

type branchWrappedDEKJSON struct {
	Format           string `json:"format"`
	Provider         string `json:"provider,omitempty"`
	KMSKeyRef        string `json:"kms_key_ref,omitempty"`
	Generation       int64  `json:"generation,omitempty"`
	Scope            string `json:"scope,omitempty"`
	WrappedBranchKey string `json:"wrapped_branch_key"`
	WrappedDEK       string `json:"wrapped_dek"`
}

type branchWrappedDEK struct {
	Provider, KMSKeyRef string
	Generation          int64
	Scope               string
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
	active     map[string]cachedBranchKey
	activeMeta map[string]BranchKeyRecord
	cache      map[[32]byte]cachedBranchKey
	scoped     bool
}

func NewBranchKMSKeyring(ctx context.Context, activeID, region string, keys map[string]string, registry BranchKeyRegistry, cacheTTL time.Duration, cacheMax int) (*BranchKMSKeyring, error) {
	return newBranchKMSKeyring(ctx, activeID, region, keys, registry, cacheTTL, cacheMax, false)
}

func NewScopedBranchKMSKeyring(ctx context.Context, activeID, region string, keys map[string]string, registry BranchKeyRegistry, cacheTTL time.Duration, cacheMax int) (*BranchKMSKeyring, error) {
	return newBranchKMSKeyring(ctx, activeID, region, keys, registry, cacheTTL, cacheMax, true)
}

func newBranchKMSKeyring(ctx context.Context, activeID, region string, keys map[string]string, registry BranchKeyRegistry, cacheTTL time.Duration, cacheMax int, scoped bool) (*BranchKMSKeyring, error) {
	if registry == nil {
		return nil, errors.New("branch key registry is required")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration for KV branch encryption: %w", err)
	}
	client := kms.NewFromConfig(cfg)
	return newPersistentBranchKMSKeyring(activeID, keys, &awsBranchKMSProvider{client: client}, registry,
		&KMSKeyring{activeID: activeID, keys: keys, client: client}, cacheTTL, cacheMax, scoped)
}

func NewCloudBranchKMSKeyring(ctx context.Context, activeID string, keys map[string]string, registry BranchKeyRegistry, cacheTTL time.Duration, cacheMax int) (*BranchKMSKeyring, error) {
	return newCloudBranchKMSKeyring(ctx, activeID, keys, registry, cacheTTL, cacheMax, false)
}

func NewScopedCloudBranchKMSKeyring(ctx context.Context, activeID string, keys map[string]string, registry BranchKeyRegistry, cacheTTL time.Duration, cacheMax int) (*BranchKMSKeyring, error) {
	return newCloudBranchKMSKeyring(ctx, activeID, keys, registry, cacheTTL, cacheMax, true)
}

func newCloudBranchKMSKeyring(ctx context.Context, activeID string, keys map[string]string, registry BranchKeyRegistry, cacheTTL time.Duration, cacheMax int, scoped bool) (*BranchKMSKeyring, error) {
	if registry == nil {
		return nil, errors.New("branch key registry is required")
	}
	provider, err := newCloudKMSProvider(ctx)
	if err != nil {
		return nil, err
	}
	return newPersistentBranchKMSKeyring(activeID, keys, provider, registry, nil, cacheTTL, cacheMax, scoped)
}

func newPersistentBranchKMSKeyring(activeID string, keys map[string]string, provider branchKMSProvider, registry BranchKeyRegistry, direct *KMSKeyring, cacheTTL time.Duration, cacheMax int, scoped bool) (*BranchKMSKeyring, error) {
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
		cacheTTL: cacheTTL, cacheMax: cacheMax, now: time.Now, cache: make(map[[32]byte]cachedBranchKey),
		active: make(map[string]cachedBranchKey), activeMeta: make(map[string]BranchKeyRecord), scoped: scoped}, nil
}

func (k *BranchKMSKeyring) ActiveKeyID() string { return k.activeID }

// NeedsRewrap reports whether a stored DEK should move to the active scoped
// branch. Legacy/direct formats remain readable, but scoped deployments can
// migrate them without changing the encrypted value itself.
func (k *BranchKMSKeyring) NeedsRewrap(keyID string, wrapped []byte, record Record) bool {
	if keyID != k.activeID {
		return true
	}
	if !k.scoped {
		return false
	}
	value, recognized, err := parseBranchWrappedDEK(wrapped)
	return err != nil || !recognized || value.Scope != k.scopeForRecord(record) || value.Scope == legacyBranchScope
}

func (k *BranchKMSKeyring) GenerateDataKey(ctx context.Context, record Record) ([]byte, []byte, error) {
	branch, metadata, err := k.activeBranch(ctx, k.scopeForRecord(record))
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
	branch, metadata, err := k.activeBranch(ctx, k.scopeForRecord(record))
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
	if value.Scope != legacyBranchScope && value.Scope != scopedBranchScope(record) {
		return nil, ErrDecrypt
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

func (k *BranchKMSKeyring) activeBranch(ctx context.Context, scope string) ([]byte, BranchKeyRecord, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	if active := k.active[scope]; len(active.plaintext) == dataKeySize && now.Before(active.expiresAt) {
		return append([]byte(nil), active.plaintext...), cloneBranchKeyRecord(k.activeMeta[scope]), nil
	}
	k.clearActiveLocked(scope)
	record, err := k.registry.GetActiveBranchKey(ctx, k.provider.Name(), k.activeID, scope)
	var plaintext []byte
	if errors.Is(err, ErrBranchKeyNotFound) {
		record, plaintext, err = k.createPersistentBranch(ctx, now, scope)
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
	for len(k.active) >= k.cacheMax {
		for candidate := range k.active {
			k.clearActiveLocked(candidate)
			break
		}
	}
	k.active[scope] = cachedBranchKey{plaintext: plaintext, expiresAt: now.Add(k.cacheTTL)}
	k.activeMeta[scope] = cloneBranchKeyRecord(record)
	k.putCacheLocked(record.Provider, record.KMSKeyRef, record.Generation, record.Scope, record.WrappedKey, k.active[scope])
	return append([]byte(nil), plaintext...), cloneBranchKeyRecord(record), nil
}

func (k *BranchKMSKeyring) createPersistentBranch(ctx context.Context, now time.Time, scope string) (BranchKeyRecord, []byte, error) {
	generation, err := k.registry.NextBranchKeyGeneration(ctx, k.provider.Name(), k.activeID, scope)
	if err != nil {
		return BranchKeyRecord{}, nil, err
	}
	plaintext := make([]byte, dataKeySize)
	if _, err := io.ReadFull(rand.Reader, plaintext); err != nil {
		return BranchKeyRecord{}, nil, fmt.Errorf("generate branch key: %w", err)
	}
	record := BranchKeyRecord{Provider: k.provider.Name(), KeyID: k.activeID, Scope: scope, Generation: generation,
		KMSKeyRef: k.keys[k.activeID], CreatedAt: now.UTC()}
	record.WrappedKey, err = k.provider.Encrypt(ctx, record.KMSKeyRef, plaintext, persistentBranchContext(record))
	if err != nil {
		clear(plaintext)
		return BranchKeyRecord{}, nil, err
	}
	if err := k.registry.CreateActiveBranchKey(ctx, record); err != nil {
		clear(plaintext)
		winner, getErr := k.registry.GetActiveBranchKey(ctx, k.provider.Name(), k.activeID, scope)
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
	contextValues := persistentBranchContext(BranchKeyRecord{Provider: provider, KeyID: keyID, Scope: value.Scope, Generation: generation, KMSKeyRef: keyRef})
	if value.LegacyV1 {
		if k.provider.Name() != "aws-kms" {
			return nil, ErrDecrypt
		}
		provider, keyRef, generation = "aws-kms", k.keys[keyID], 0
		contextValues = legacyBranchKMSContext(keyID)
	}
	hash := branchCacheHash(provider, keyRef, generation, value.Scope, value.WrappedBranchKey)
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
	k.putCacheLocked(provider, keyRef, generation, value.Scope, value.WrappedBranchKey, entry)
	clear(plaintext)
	log.Printf("[KV_ENCRYPTION] decrypted %s branch key generation %d after cache miss for key ID %q", provider, generation, keyID)
	return append([]byte(nil), k.cache[hash].plaintext...), nil
}

func (k *BranchKMSKeyring) putCacheLocked(provider, keyRef string, generation int64, scope string, wrapped []byte, entry cachedBranchKey) {
	hash := branchCacheHash(provider, keyRef, generation, scope, wrapped)
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

func branchCacheHash(provider, keyRef string, generation int64, scope string, wrapped []byte) [32]byte {
	input, _ := json.Marshal(struct {
		Provider   string `json:"provider"`
		KeyRef     string `json:"key_ref"`
		Generation int64  `json:"generation"`
		Scope      string `json:"scope"`
		Wrapped    string `json:"wrapped"`
	}{provider, keyRef, generation, scope, base64.StdEncoding.EncodeToString(wrapped)})
	return sha256.Sum256(input)
}

func (k *BranchKMSKeyring) clearActiveLocked(scope string) {
	if active := k.active[scope]; len(active.plaintext) > 0 {
		clear(active.plaintext)
	}
	delete(k.active, scope)
	delete(k.activeMeta, scope)
}

func (k *BranchKMSKeyring) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	for scope := range k.active {
		k.clearActiveLocked(scope)
	}
	for hash, entry := range k.cache {
		clear(entry.plaintext)
		delete(k.cache, hash)
	}
}

func marshalBranchWrappedDEK(branch BranchKeyRecord, wrappedDEK []byte) ([]byte, error) {
	format := branchWrappedDEKFormatV2
	if branch.Scope != legacyBranchScope {
		format = branchWrappedDEKFormatV3
	}
	return json.Marshal(branchWrappedDEKJSON{Format: format, Provider: branch.Provider,
		KMSKeyRef: branch.KMSKeyRef, Generation: branch.Generation, Scope: branch.Scope,
		WrappedBranchKey: base64.StdEncoding.EncodeToString(branch.WrappedKey),
		WrappedDEK:       base64.StdEncoding.EncodeToString(wrappedDEK)})
}

func parseBranchWrappedDEK(data []byte) (branchWrappedDEK, bool, error) {
	var marker struct {
		Format string `json:"format"`
	}
	if json.Unmarshal(data, &marker) != nil || (marker.Format != branchWrappedDEKFormatV1 && marker.Format != branchWrappedDEKFormatV2 && marker.Format != branchWrappedDEKFormatV3) {
		return branchWrappedDEK{}, false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var raw branchWrappedDEKJSON
	if decoder.Decode(&raw) != nil || (raw.Format != branchWrappedDEKFormatV1 && raw.Format != branchWrappedDEKFormatV2 && raw.Format != branchWrappedDEKFormatV3) {
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
	if raw.Format == branchWrappedDEKFormatV3 && raw.Scope == "" {
		return branchWrappedDEK{}, true, ErrDecrypt
	}
	return branchWrappedDEK{Provider: raw.Provider, KMSKeyRef: raw.KMSKeyRef, Generation: raw.Generation, Scope: raw.Scope,
		WrappedBranchKey: wrappedBranch, WrappedDEK: wrappedDEK, LegacyV1: legacy}, true, nil
}

func persistentBranchContext(record BranchKeyRecord) map[string]string {
	context := map[string]string{"application": "agentapi-kv", "purpose": "persistent-branch-key",
		"provider": record.Provider, "key_id": record.KeyID, "generation": fmt.Sprintf("%d", record.Generation)}
	if record.Scope != legacyBranchScope {
		context["scope"] = record.Scope
	}
	return context
}

func (k *BranchKMSKeyring) scopeForRecord(record Record) string {
	if !k.scoped {
		return legacyBranchScope
	}
	return scopedBranchScope(record)
}

func scopedBranchScope(record Record) string {
	// Ownership labels differ between resource types. Prefer team boundaries,
	// then user/owner boundaries. Hash the selected value so identifiers are not
	// exposed in the branch-key table or KMS audit metadata.
	preferred := []string{
		"agentapi.proxy/team-id", "agentapi.proxy/session-profile-team-id-hash",
		"agentapi.proxy/session-route-team-id-hash", "agentapi.proxy/slackbot-team-id-hash",
		"agentapi.proxy/webhook-team-id-hash", "agentapi.proxy/team-hash",
		"agentapi.proxy/api-token-owner", "agentapi.proxy/user-id",
		"agentapi.proxy/session-profile-user-id", "agentapi.proxy/session-route-user-id",
		"agentapi.proxy/slackbot-user-id", "agentapi.proxy/webhook-user-id",
		"agentapi.proxy/owner-hash", "agentapi.proxy/settings-name", "agentapi.proxy/credentials-name",
	}
	for _, label := range preferred {
		if value := record.Labels[label]; value != "" {
			digest := sha256.Sum256([]byte(label + "\x00" + value))
			return "owner:" + fmt.Sprintf("%x", digest[:16])
		}
	}
	digest := sha256.Sum256([]byte(string(record.Kind) + "\x00" + record.Namespace))
	return "namespace:" + fmt.Sprintf("%x", digest[:16])
}

func legacyBranchKMSContext(keyID string) map[string]string {
	return map[string]string{"application": "agentapi-kv", "purpose": "branch-key", "key_id": keyID}
}

func cloneBranchKeyRecord(record BranchKeyRecord) BranchKeyRecord {
	record.WrappedKey = append([]byte(nil), record.WrappedKey...)
	return record
}
