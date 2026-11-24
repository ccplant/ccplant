package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// GetInitialMessage retrieves the initial message from Secret for a given session
func (m *KubernetesSessionManager) GetInitialMessage(ctx context.Context, session *KubernetesSession) string {
	return m.getInitialMessageFromSecret(ctx, session.ServiceName())
}

// generatePersonalAPIKey generates a random API key for personal use
// This uses the same format as team service account keys
func generatePersonalAPIKey() (string, error) {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", err
	}
	return "ap_" + hex.EncodeToString(keyBytes), nil
}

func (m *KubernetesSessionManager) ensurePersonalAPIKey(ctx context.Context, userID string) *entities.PersonalAPIKey {
	if m.personalAPIKeyRepo == nil || userID == "" {
		return nil
	}
	apiKey, err := m.personalAPIKeyRepo.FindByUserID(ctx, entities.UserID(userID))
	if err == nil && apiKey != nil {
		return apiKey
	}

	log.Printf("[K8S_SESSION] No personal API key found for user %s, creating new one", userID)
	generatedKey, genErr := generatePersonalAPIKey()
	if genErr != nil {
		log.Printf("[K8S_SESSION] Warning: failed to generate personal API key: %v", genErr)
		return nil
	}
	apiKey = entities.NewPersonalAPIKey(entities.UserID(userID), generatedKey)
	if saveErr := m.personalAPIKeyRepo.Save(ctx, apiKey); saveErr != nil {
		log.Printf("[K8S_SESSION] Warning: failed to save personal API key for user %s: %v", userID, saveErr)
		return nil
	}
	if m.personalAPIKeyLoader != nil {
		// Register new keys immediately so in-session callbacks can authenticate
		// without waiting for a proxy restart and Kubernetes bootstrap.
		if loadErr := m.personalAPIKeyLoader.LoadPersonalAPIKey(ctx, apiKey); loadErr != nil {
			log.Printf("[K8S_SESSION] Warning: failed to register personal API key for user %s: %v", userID, loadErr)
		}
	}
	return apiKey
}

// buildSessionSettings constructs SessionSettings from RunServerRequest and session state.
