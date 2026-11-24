package kvstore

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/authzscope"
)

const (
	envelopeFormat  = "agentapi-kv-envelope/v1"
	dataKeySize     = 32
	maxEnvelopeSize = 16 << 20
)

var (
	ErrDecrypt                   = errors.New("kv value decryption failed")
	ErrPlaintextInEncryptedStore = errors.New("encrypted KV store contains a plaintext value")
	ErrEncryptedInPlaintextStore = errors.New("plaintext KV store contains an encrypted value")
)

// LocalKeyring provides envelope encryption with one active KEK and any number
// of read keys. Each configured key must be a 32-byte AES key.
type LocalKeyring struct {
	activeID string
	keys     map[string][]byte
}

type EnvelopeKeyring interface {
	ActiveKeyID() string
	GenerateDataKey(context.Context, Record) ([]byte, []byte, error)
	WrapDataKey(context.Context, string, []byte, Record) ([]byte, error)
	UnwrapDataKey(context.Context, string, []byte, Record) ([]byte, error)
}

func (k *LocalKeyring) ActiveKeyID() string { return k.activeID }

func (k *LocalKeyring) GenerateDataKey(_ context.Context, record Record) ([]byte, []byte, error) {
	dek := make([]byte, dataKeySize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, nil, fmt.Errorf("generate KV data key: %w", err)
	}
	wrapped, err := wrapDEK(k.keys[k.activeID], dek, record)
	if err != nil {
		clear(dek)
		return nil, nil, err
	}
	return dek, wrapped, nil
}

func (k *LocalKeyring) WrapDataKey(_ context.Context, keyID string, dek []byte, record Record) ([]byte, error) {
	key, ok := k.keys[keyID]
	if !ok {
		return nil, ErrDecrypt
	}
	return wrapDEK(key, dek, record)
}

func (k *LocalKeyring) UnwrapDataKey(_ context.Context, keyID string, wrapped []byte, record Record) ([]byte, error) {
	key, ok := k.keys[keyID]
	if !ok {
		return nil, ErrDecrypt
	}
	return unwrapDEK(key, wrapped, record)
}

func NewLocalKeyring(activeID string, encodedKeys map[string]string) (*LocalKeyring, error) {
	activeID = strings.TrimSpace(activeID)
	if activeID == "" {
		return nil, errors.New("active KV encryption key ID is required")
	}
	if len(activeID) > 128 {
		return nil, errors.New("active KV encryption key ID is too long")
	}
	keys := make(map[string][]byte, len(encodedKeys))
	for id, encoded := range encodedKeys {
		if id == "" || len(id) > 128 {
			return nil, fmt.Errorf("invalid KV encryption key ID %q", id)
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode KV encryption key %q: %w", id, err)
		}
		if len(key) != dataKeySize {
			return nil, fmt.Errorf("KV encryption key %q must be 32 bytes, got %d", id, len(key))
		}
		keys[id] = key
	}
	if _, ok := keys[activeID]; !ok {
		return nil, fmt.Errorf("active KV encryption key %q is not in the keyring", activeID)
	}
	return &LocalKeyring{activeID: activeID, keys: keys}, nil
}

type encryptedStore struct {
	backend Store
	keyring EnvelopeKeyring
}

type RewrapResult struct {
	Selected  int
	Rewrapped int
	Skipped   int
}

// RewrapAll changes only the wrapped DEK of every encrypted record in a
// namespace to the active key. Callers must stop writers before invoking it.
func RewrapAll(ctx context.Context, backend Store, keyring EnvelopeKeyring, namespace string, dryRun bool) (RewrapResult, error) {
	return rewrapAll(ctx, backend, keyring, keyring, namespace, dryRun, false)
}

// RewrapAllWithKeyrings unwraps existing DEKs with source and wraps them with
// destination. This permits provider migrations without re-encrypting values.
func RewrapAllWithKeyrings(ctx context.Context, backend Store, source, destination EnvelopeKeyring, namespace string, dryRun bool) (RewrapResult, error) {
	return rewrapAll(ctx, backend, source, destination, namespace, dryRun, true)
}

func rewrapAll(ctx context.Context, backend Store, source, destination EnvelopeKeyring, namespace string, dryRun, force bool) (RewrapResult, error) {
	var result RewrapResult
	if source == nil || destination == nil {
		return result, errors.New("source and destination keyrings are required")
	}
	if mode, ok := backend.(interface{ expectEncryptedValues() }); ok {
		mode.expectEncryptedValues()
	}
	scanner, ok := backend.(Scanner)
	if !ok {
		return result, errors.New("KV backend does not support administrative scanning")
	}
	for _, kind := range []Kind{KindSecret, KindConfigMap} {
		records, err := scanner.Scan(ctx, ScanQuery{Kind: kind, Namespace: namespace})
		if err != nil {
			return result, err
		}
		for _, record := range records {
			result.Selected++
			envelope, err := parseEnvelope(record.Value)
			if err != nil {
				return result, fmt.Errorf("rewrap %s/%s: %w", kind, record.Key, ErrDecrypt)
			}
			dek, err := source.UnwrapDataKey(ctx, envelope.KeyID, envelope.WrappedDEK, record)
			alreadyDestination := false
			if err != nil && force {
				dek, err = destination.UnwrapDataKey(ctx, envelope.KeyID, envelope.WrappedDEK, record)
				alreadyDestination = err == nil
			}
			if err != nil {
				return result, fmt.Errorf("rewrap %s/%s: %w", kind, record.Key, ErrDecrypt)
			}
			needsRewrap := !alreadyDestination && (force || envelope.KeyID != destination.ActiveKeyID())
			if !force || alreadyDestination {
				if checker, ok := destination.(interface {
					NeedsRewrap(string, []byte, Record) bool
				}); ok {
					needsRewrap = checker.NeedsRewrap(envelope.KeyID, envelope.WrappedDEK, record)
				}
			}
			if !needsRewrap {
				clear(dek)
				result.Skipped++
				continue
			}
			wrapped, err := destination.WrapDataKey(ctx, destination.ActiveKeyID(), dek, record)
			clear(dek)
			if err != nil {
				return result, fmt.Errorf("rewrap %s/%s: %w", kind, record.Key, err)
			}
			if !dryRun {
				record.Value, err = marshalEnvelope(valueEnvelope{
					Format: envelopeFormat, KeyID: destination.ActiveKeyID(), WrappedDEK: wrapped,
					Nonce: envelope.Nonce, Ciphertext: envelope.Ciphertext,
				})
				if err != nil {
					return result, err
				}
				if _, err := backend.Update(ctx, record); err != nil {
					return result, fmt.Errorf("rewrap %s/%s: %w", kind, record.Key, err)
				}
			}
			result.Rewrapped++
		}
	}
	return result, nil
}

func NewEncryptedStore(backend Store, keyring EnvelopeKeyring) (Store, error) {
	if backend == nil || keyring == nil {
		return nil, errors.New("KV backend and encryption keyring are required")
	}
	if mode, ok := backend.(interface{ expectEncryptedValues() }); ok {
		mode.expectEncryptedValues()
	}
	return &encryptedStore{backend: backend, keyring: keyring}, nil
}

func (s *encryptedStore) Close() error {
	if closer, ok := s.keyring.(interface{ Close() }); ok {
		closer.Close()
	}
	return s.backend.Close()
}

func (s *encryptedStore) Create(ctx context.Context, record Record) (Record, error) {
	plaintext := append([]byte(nil), record.Value...)
	sealed, err := s.seal(ctx, &record)
	if err != nil {
		return Record{}, err
	}
	record.Value = sealed
	created, err := s.backend.Create(ctx, record)
	if err != nil {
		return Record{}, err
	}
	created.Value = plaintext
	return created, nil
}

func (s *encryptedStore) Update(ctx context.Context, record Record) (Record, error) {
	if _, restricted := authzscope.FromContext(ctx); restricted {
		actualLabels, err := documentLabels(record.Kind, record.Value)
		if err != nil {
			return Record{}, fmt.Errorf("extract KV document labels: %w", err)
		}
		if record.Labels != nil && !equalLabels(actualLabels, record.Labels) {
			return Record{}, errors.New("KV record labels do not match document labels")
		}
		record.Labels = actualLabels
		existing, err := s.backend.Get(ctx, record.Kind, record.Namespace, record.Key)
		if err != nil {
			return Record{}, err
		}
		if !authorizeRecord(ctx, existing, "update") {
			return Record{}, ErrNotFound
		}
		record.OwnerScope = ownerScopeForRecord(record)
		if existingScope := ownerScopeForRecord(existing); existingScope != record.OwnerScope {
			return Record{}, ErrAccessDenied
		}
	}
	plaintext := append([]byte(nil), record.Value...)
	sealed, err := s.seal(ctx, &record)
	if err != nil {
		return Record{}, err
	}
	record.Value = sealed
	updated, err := s.backend.Update(ctx, record)
	if err != nil {
		return Record{}, err
	}
	updated.Value = plaintext
	return updated, nil
}

func (s *encryptedStore) Get(ctx context.Context, kind Kind, namespace, key string) (Record, error) {
	record, err := s.backend.Get(ctx, kind, namespace, key)
	if err != nil {
		return Record{}, err
	}
	if err := s.open(ctx, &record); err != nil {
		if errors.Is(err, ErrAccessDenied) {
			return Record{}, ErrNotFound
		}
		return Record{}, err
	}
	return record, nil
}

func (s *encryptedStore) Delete(ctx context.Context, kind Kind, namespace, key string, version int64) error {
	record, err := s.backend.Get(ctx, kind, namespace, key)
	if err != nil {
		return err
	}
	if !authorizeRecord(ctx, record, "delete") {
		return ErrNotFound
	}
	return s.backend.Delete(ctx, kind, namespace, key, version)
}

func (s *encryptedStore) List(ctx context.Context, query Query) ([]Record, error) {
	records, err := s.backend.List(ctx, query)
	if err != nil {
		return nil, err
	}
	n := 0
	for i := range records {
		if err := s.open(ctx, &records[i]); err != nil {
			if errors.Is(err, ErrAccessDenied) {
				continue
			}
			return nil, err
		}
		records[n] = records[i]
		n++
	}
	return records[:n], nil
}

func (s *encryptedStore) Scan(ctx context.Context, query ScanQuery) ([]Record, error) {
	scanner, ok := s.backend.(Scanner)
	if !ok {
		return nil, errors.New("encrypted KV backend does not support scanning")
	}
	records, err := scanner.Scan(ctx, query)
	if err != nil {
		return nil, err
	}
	n := 0
	for i := range records {
		if err := s.open(ctx, &records[i]); err != nil {
			if errors.Is(err, ErrAccessDenied) {
				continue
			}
			return nil, err
		}
		records[n] = records[i]
		n++
	}
	return records[:n], nil
}

type valueEnvelope struct {
	Format     string
	KeyID      string
	WrappedDEK []byte
	Nonce      []byte
	Ciphertext []byte
}

type envelopeJSON struct {
	Format     string `json:"format"`
	KeyID      string `json:"key_id"`
	WrappedDEK string `json:"wrapped_dek"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func (s *encryptedStore) seal(ctx context.Context, record *Record) ([]byte, error) {
	actualLabels, err := documentLabels(record.Kind, record.Value)
	if err != nil {
		return nil, fmt.Errorf("extract KV document labels: %w", err)
	}
	if record.Labels != nil && !equalLabels(actualLabels, record.Labels) {
		return nil, errors.New("KV record labels do not match document labels")
	}
	record.Labels = actualLabels
	record.OwnerScope = ownerScopeForRecord(*record)
	if !authorizeRecord(ctx, *record, "write") {
		return nil, ErrAccessDenied
	}
	dek, wrappedDEK, err := s.keyring.GenerateDataKey(ctx, *record)
	if err != nil {
		return nil, err
	}
	defer clear(dek)

	dataAEAD, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, dataAEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate KV value nonce: %w", err)
	}
	aad, err := recordAAD(*record)
	if err != nil {
		return nil, err
	}
	ciphertext := dataAEAD.Seal(nil, nonce, record.Value, aad)

	encoded, err := marshalEnvelope(valueEnvelope{
		Format: envelopeFormat, KeyID: s.keyring.ActiveKeyID(), WrappedDEK: wrappedDEK,
		Nonce: nonce, Ciphertext: ciphertext,
	})
	if err != nil {
		return nil, fmt.Errorf("encode KV envelope: %w", err)
	}
	if len(encoded) > maxEnvelopeSize {
		return nil, fmt.Errorf("KV envelope exceeds %d bytes", maxEnvelopeSize)
	}
	return encoded, nil
}
