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
)

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
	expectedScope := ownerScopeForRecord(*record)
	if record.OwnerScope != "" && record.OwnerScope != expectedScope {
		return ErrDecrypt
	}
	record.OwnerScope = expectedScope
	if !authorizeRecord(ctx, *record, "decrypt") {
		return ErrAccessDenied
	}
	if !isEnvelopeCandidate(record.Value) {
		return ErrPlaintextInEncryptedStore
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
