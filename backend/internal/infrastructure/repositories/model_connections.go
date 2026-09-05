package repositories

import (
	"context"
	"fmt"

	domainservices "github.com/takutakahashi/agentapi-proxy/internal/domain/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

type connectionJSON struct {
	*modelprovider.Connection
	APIKey          string               `json:"api_key,omitempty"`
	EncryptedAPIKey *encryptedEnvVarJSON `json:"encrypted_api_key,omitempty"`
}

func (r *KubernetesSettingsRepository) encodeConnection(ctx context.Context, c *modelprovider.Connection) (*connectionJSON, error) {
	if c == nil {
		return nil, nil
	}
	out := &connectionJSON{Connection: c.Clone()}
	if c.APIKey == "" {
		return out, nil
	}
	if svc := r.encryptionSvc(); svc != nil && svc.Algorithm() != "noop" {
		encrypted, err := svc.Encrypt(ctx, c.APIKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt connection key")
		}
		out.EncryptedAPIKey = &encryptedEnvVarJSON{EncryptedValue: encrypted.EncryptedValue, Algorithm: encrypted.Metadata.Algorithm, KeyID: encrypted.Metadata.KeyID, EncryptedAt: encrypted.Metadata.EncryptedAt, Version: encrypted.Metadata.Version}
	} else {
		out.APIKey = c.APIKey
	}
	return out, nil
}
func (r *KubernetesSettingsRepository) decodeConnection(ctx context.Context, stored *connectionJSON) (*modelprovider.Connection, error) {
	if stored == nil {
		return nil, nil
	}
	if stored.Connection == nil {
		return nil, fmt.Errorf("invalid stored connection")
	}
	c := stored.Clone()
	c.APIKey = stored.APIKey
	if ev := stored.EncryptedAPIKey; ev != nil {
		metadata := domainservices.EncryptionMetadata{Algorithm: ev.Algorithm, KeyID: ev.KeyID, EncryptedAt: ev.EncryptedAt, Version: ev.Version}
		svc := r.decryptionSvc(metadata)
		if svc == nil {
			return nil, fmt.Errorf("connection decryption service unavailable")
		}
		value, err := svc.Decrypt(ctx, &domainservices.EncryptedData{EncryptedValue: ev.EncryptedValue, Metadata: metadata})
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt connection key")
		}
		c.APIKey = value
	}
	c.HasAPIKey = c.APIKey != ""
	return c, nil
}
