package kvstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

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
	if record.OwnerScope != "" {
		return record.OwnerScope
	}
	// Ownership labels differ between resource types. Prefer team boundaries,
	// then user/owner boundaries. Hash the selected value so identifiers are not
	// exposed in the branch-key table or KMS audit metadata.
	preferred := []string{
		"agentapi.proxy/team-id", "agentapi.proxy/session-profile-team-id-hash",
		"agentapi.proxy/session-route-team-id-hash", "agentapi.proxy/slackbot-team-id-hash",
		"agentapi.proxy/webhook-team-id-hash", "agentapi.proxy/team-hash", "agentapi.proxy/schedule-team-id",
		"agentapi.proxy/api-token-owner", "agentapi.proxy/user-id",
		"agentapi.proxy/session-profile-user-id", "agentapi.proxy/session-route-user-id",
		"agentapi.proxy/slackbot-user-id", "agentapi.proxy/webhook-user-id",
		"agentapi.proxy/owner-hash", "agentapi.proxy/settings-name", "agentapi.proxy/credentials-name", "agentapi.proxy/schedule-user-id",
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
