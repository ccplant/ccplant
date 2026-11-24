package app

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func supportsGitHubSecretStorage(cfg config.KVStoreConfig) bool {
	backend := configuredKVBackend(cfg)
	return backend == "libsql-encrypted" || backend == "kubernetes"
}

// AddCustomHandler adds a custom handler to the registry
func (r *Router) AddCustomHandler(handler CustomHandler) {
	r.handlers.customHandlers = append(r.handlers.customHandlers, handler)
	log.Printf("Added custom handler: %s", handler.GetName())
}

// RegisterRoutes registers all routes
func (r *Router) RegisterRoutes() error {
	// Register core routes
	if err := r.registerCoreRoutes(); err != nil {
		return err
	}

	// Register conditional routes based on configuration
	if err := r.registerConditionalRoutes(); err != nil {
		return err
	}

	// Register custom handlers
	if err := r.registerCustomHandlers(); err != nil {
		return err
	}

	r.registerSessionProxyRoutes()

	return nil
}

// registerCoreRoutes registers the core routes that are always available
