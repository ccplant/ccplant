package kvstore

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	envelopeFormat  = "agentapi-kv-envelope/v1"
	dataKeySize     = 32
	maxEnvelopeSize = 16 << 20
)

var ErrDecrypt = errors.New("kv value decryption failed")

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
	backend              Store
	keyring              EnvelopeKeyring
	allowLegacyPlaintext bool
}

type EncryptedStoreOptions struct {
	AllowLegacyPlaintext bool
}

type RewrapResult struct {
	Selected  int
	Rewrapped int
	Skipped   int
}

// RewrapAll changes only the wrapped DEK of every encrypted record in a
// namespace to the active key. Callers must stop writers before invoking it.
func RewrapAll(ctx context.Context, backend Store, keyring EnvelopeKeyring, namespace string, dryRun bool) (RewrapResult, error) {
	var result RewrapResult
	for _, kind := range []Kind{KindSecret, KindConfigMap} {
		records, err := backend.List(ctx, Query{Kind: kind, Namespace: namespace})
		if err != nil {
			return result, err
		}
		for _, record := range records {
			result.Selected++
			envelope, err := parseEnvelope(record.Value)
			if err != nil {
				return result, fmt.Errorf("rewrap %s/%s: %w", kind, record.Key, ErrDecrypt)
			}
			dek, err := keyring.UnwrapDataKey(ctx, envelope.KeyID, envelope.WrappedDEK, record)
			if err != nil {
				return result, fmt.Errorf("rewrap %s/%s: %w", kind, record.Key, ErrDecrypt)
			}
			if envelope.KeyID == keyring.ActiveKeyID() {
				clear(dek)
				result.Skipped++
				continue
			}
			wrapped, err := keyring.WrapDataKey(ctx, keyring.ActiveKeyID(), dek, record)
			clear(dek)
			if err != nil {
				return result, fmt.Errorf("rewrap %s/%s: %w", kind, record.Key, err)
			}
			if !dryRun {
				record.Value, err = marshalEnvelope(valueEnvelope{
					Format: envelopeFormat, KeyID: keyring.ActiveKeyID(), WrappedDEK: wrapped,
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
	return NewEncryptedStoreWithOptions(backend, keyring, EncryptedStoreOptions{})
}

func NewEncryptedStoreWithOptions(backend Store, keyring EnvelopeKeyring, options EncryptedStoreOptions) (Store, error) {
	if backend == nil || keyring == nil {
		return nil, errors.New("KV backend and encryption keyring are required")
	}
	return &encryptedStore{backend: backend, keyring: keyring, allowLegacyPlaintext: options.AllowLegacyPlaintext}, nil
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
		return Record{}, err
	}
	return record, nil
}

func (s *encryptedStore) Delete(ctx context.Context, kind Kind, namespace, key string, version int64) error {
	return s.backend.Delete(ctx, kind, namespace, key, version)
}

func (s *encryptedStore) List(ctx context.Context, query Query) ([]Record, error) {
	records, err := s.backend.List(ctx, query)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if err := s.open(ctx, &records[i]); err != nil {
			return nil, err
		}
	}
	return records, nil
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

func marshalEnvelope(envelope valueEnvelope) ([]byte, error) {
	encoded, err := json.Marshal(envelopeJSON{
		Format: envelope.Format, KeyID: envelope.KeyID,
		WrappedDEK: base64.StdEncoding.EncodeToString(envelope.WrappedDEK),
		Nonce:      base64.StdEncoding.EncodeToString(envelope.Nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(envelope.Ciphertext),
	})
	if err != nil {
		return nil, fmt.Errorf("encode KV envelope: %w", err)
	}
	if len(encoded) > maxEnvelopeSize {
		return nil, fmt.Errorf("KV envelope exceeds %d bytes", maxEnvelopeSize)
	}
	return encoded, nil
}

func (s *encryptedStore) open(ctx context.Context, record *Record) error {
	if !isEnvelopeCandidate(record.Value) {
		if !s.allowLegacyPlaintext {
			return ErrDecrypt
		}
		return s.openLegacyPlaintext(ctx, record)
	}
	envelope, err := parseEnvelope(record.Value)
	if err != nil {
		return ErrDecrypt
	}
	dek, err := s.keyring.UnwrapDataKey(ctx, envelope.KeyID, envelope.WrappedDEK, *record)
	if err != nil {
		return ErrDecrypt
	}
	defer clear(dek)
	dataAEAD, err := newGCM(dek)
	if err != nil || len(envelope.Nonce) != dataAEAD.NonceSize() {
		return ErrDecrypt
	}
	aad, err := recordAAD(*record)
	if err != nil {
		return ErrDecrypt
	}
	plaintext, err := dataAEAD.Open(nil, envelope.Nonce, envelope.Ciphertext, aad)
	if err != nil {
		return ErrDecrypt
	}
	actualLabels, err := documentLabels(record.Kind, plaintext)
	if err != nil || !equalLabels(actualLabels, record.Labels) {
		clear(plaintext)
		return ErrDecrypt
	}
	record.Value = plaintext
	return nil
}

func (s *encryptedStore) openLegacyPlaintext(ctx context.Context, record *Record) error {
	plaintext := append([]byte(nil), record.Value...)
	actualLabels, err := documentLabels(record.Kind, plaintext)
	if err != nil || !equalLabels(actualLabels, record.Labels) {
		return ErrDecrypt
	}
	repair := *record
	repair.Value = plaintext
	sealed, err := s.seal(ctx, &repair)
	if err != nil {
		return fmt.Errorf("encrypt legacy KV value: %w", err)
	}
	repair.Value = sealed
	updated, err := s.backend.Update(ctx, repair)
	if err != nil {
		return fmt.Errorf("replace legacy KV value: %w", err)
	}
	record.Version = updated.Version
	record.Value = plaintext
	return nil
}

func isEnvelopeCandidate(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	candidate := false
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return candidate
		}
		name, ok := nameToken.(string)
		if !ok {
			return candidate
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return candidate
		}
		switch name {
		case "key_id", "wrapped_dek", "nonce", "ciphertext":
			candidate = true
		case "format":
			var format string
			if json.Unmarshal(value, &format) == nil && format == envelopeFormat {
				candidate = true
			}
		}
	}
	return candidate
}

func wrapDEK(kek, dek []byte, record Record) ([]byte, error) {
	aead, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate KV key-wrap nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, dek, wrapAAD(record)), nil
}

func unwrapDEK(kek, wrapped []byte, record Record) ([]byte, error) {
	aead, err := newGCM(kek)
	if err != nil || len(wrapped) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrDecrypt
	}
	dek, err := aead.Open(nil, wrapped[:aead.NonceSize()], wrapped[aead.NonceSize():], wrapAAD(record))
	if err != nil || len(dek) != dataKeySize {
		clear(dek)
		return nil, ErrDecrypt
	}
	return dek, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}
	return aead, nil
}

func recordAAD(record Record) ([]byte, error) {
	return json.Marshal(struct {
		Format    string            `json:"format"`
		Kind      Kind              `json:"kind"`
		Namespace string            `json:"namespace"`
		Key       string            `json:"key"`
		Labels    map[string]string `json:"labels"`
	}{envelopeFormat, record.Kind, record.Namespace, record.Key, nonNilLabels(record.Labels)})
}

func wrapAAD(record Record) []byte {
	return []byte("agentapi-kv-dek/v1\x00" + string(record.Kind) + "\x00" + record.Namespace + "\x00" + record.Key)
}

func parseEnvelope(data []byte) (valueEnvelope, error) {
	if len(data) == 0 || len(data) > maxEnvelopeSize {
		return valueEnvelope{}, ErrDecrypt
	}
	if err := validateEnvelopeObject(data); err != nil {
		return valueEnvelope{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var raw envelopeJSON
	if err := decoder.Decode(&raw); err != nil {
		return valueEnvelope{}, err
	}
	if raw.Format != envelopeFormat || raw.KeyID == "" || len(raw.KeyID) > 128 {
		return valueEnvelope{}, ErrDecrypt
	}
	wrapped, err := base64.StdEncoding.Strict().DecodeString(raw.WrappedDEK)
	if err != nil {
		return valueEnvelope{}, err
	}
	nonce, err := base64.StdEncoding.Strict().DecodeString(raw.Nonce)
	if err != nil {
		return valueEnvelope{}, err
	}
	ciphertext, err := base64.StdEncoding.Strict().DecodeString(raw.Ciphertext)
	if err != nil || len(ciphertext) <= 16 {
		return valueEnvelope{}, ErrDecrypt
	}
	return valueEnvelope{Format: raw.Format, KeyID: raw.KeyID, WrappedDEK: wrapped, Nonce: nonce, Ciphertext: ciphertext}, nil
}

func validateEnvelopeObject(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ErrDecrypt
	}
	allowed := map[string]bool{"format": true, "key_id": true, "wrapped_dek": true, "nonce": true, "ciphertext": true}
	seen := make(map[string]bool, len(allowed))
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return ErrDecrypt
		}
		name, ok := nameToken.(string)
		if !ok || !allowed[name] || seen[name] {
			return ErrDecrypt
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return ErrDecrypt
		}
	}
	if _, err := decoder.Token(); err != nil || len(seen) != len(allowed) {
		return ErrDecrypt
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrDecrypt
	}
	return nil
}

func documentLabels(kind Kind, plaintext []byte) (map[string]string, error) {
	var document struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	if kind != KindSecret && kind != KindConfigMap {
		return nil, fmt.Errorf("unsupported KV kind %q", kind)
	}
	if err := json.Unmarshal(plaintext, &document); err != nil {
		return nil, err
	}
	return nonNilLabels(document.Metadata.Labels), nil
}

func nonNilLabels(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	return value
}

func equalLabels(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
