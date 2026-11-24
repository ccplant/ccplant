package app

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

func newServerEncryptionRegistry() *services.EncryptionServiceRegistry {
	// Initialize encryption service registry
	// The registry manages multiple encryption services and selects the appropriate one
	// based on encryption metadata when decrypting
	encryptionFactory := services.NewEncryptionServiceFactory("AGENTAPI_ENCRYPTION")
	primaryService, err := encryptionFactory.Create()
	if err != nil {
		log.Fatalf("Failed to create primary encryption service: %v", err)
	}

	// Create registry with primary service (used for encryption)
	encryptionRegistry := services.NewEncryptionServiceRegistry(primaryService)

	// Register Noop service for backward compatibility with plaintext data
	// This allows reading old unencrypted data
	noopService := services.NewNoopEncryptionService()
	encryptionRegistry.Register(noopService)

	// Try to register additional services for migration scenarios
	// These are optional and will be used for decryption if data was encrypted with them

	// Try to create a local encryption service (if different from primary)
	localFactory := services.NewEncryptionServiceFactory("AGENTAPI_DECRYPTION")
	if localService, err := localFactory.Create(); err == nil {
		// Only register if it's different from primary
		if localService.Algorithm() != primaryService.Algorithm() || localService.KeyID() != primaryService.KeyID() {
			encryptionRegistry.Register(localService)
		}
	}

	log.Printf("[SERVER] Encryption registry initialized with primary: %s (keyID: %s)",
		primaryService.Algorithm(), primaryService.KeyID())

	return encryptionRegistry

}
