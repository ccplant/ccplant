package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func validateSessionManagerRuntimeConfig(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("session-manager config is required")
	}
	remoteMode := cfg.SessionManager.UpstreamURL != "" && cfg.SessionManager.ConnectionToken != ""
	if !remoteMode && strings.TrimSpace(cfg.Redis.Addr) == "" {
		return errors.New("session-manager Redis is required")
	}
	if cfg.SessionManager.InternalAPIToken == "" {
		return errors.New("session-manager internal API token is required")
	}
	if cfg.SessionManager.UpstreamURL != "" && cfg.SessionManager.ConnectionToken != "" && (cfg.SessionManager.ID == "" || cfg.SessionManager.RunnerPool == "") {
		return errors.New("session-manager ID and runner pool are required")
	}
	return nil
}

func prepareSessionManagerRuntime(parent context.Context, cfg *config.Config, verbose bool) (*services.KubernetesSessionManager, error) {
	manager, err := services.NewKubernetesSessionManager(cfg, verbose, logger.NewLogger())
	if err != nil {
		return nil, fmt.Errorf("initialize Kubernetes session manager: %w", err)
	}
	ready := false
	defer func() {
		if !ready {
			cleanupSessionManagerStartup(manager, nil, nil, nil)
		}
	}()
	if cfg.SessionManager.RunnerPool != "" {
		manager.ConfigureSessionRunnerPool(cfg.SessionManager.UpstreamURL, cfg.SessionManager.ID, cfg.SessionManager.ConnectionToken, cfg.SessionManager.RunnerPool)
	}
	if cfg.SessionManager.UpstreamURL != "" && cfg.SessionManager.ConnectionToken != "" {
		profileCtx, profileCancel := context.WithTimeout(parent, 15*time.Second)
		profile, profileErr := fetchParentRuntimeProfile(profileCtx, cfg.SessionManager.UpstreamURL, cfg.SessionManager.ID, cfg.SessionManager.ConnectionToken)
		profileCancel()
		if profileErr != nil {
			return nil, fmt.Errorf("load parent runtime profile: %w", profileErr)
		}
		if profileErr = manager.ApplyRuntimeProfile(parent, profile); profileErr != nil {
			return nil, fmt.Errorf("apply parent runtime profile: %w", profileErr)
		}
	}
	// Stock workloads belong to the session-manager revision that created them.
	// Purge the complete inventory at the process boundary instead of relying
	// only on template-hash reconciliation: a manager replacement can otherwise
	// leave apparently compatible stock backed by the previous revision.
	purgeCtx, purgeCancel := context.WithTimeout(parent, 30*time.Second)
	if err := purgeSessionManagerStock(purgeCtx, manager); err != nil {
		purgeCancel()
		return nil, fmt.Errorf("purge stock sessions on session-manager startup: %w", err)
	}
	purgeCancel()

	ready = true
	return manager, nil
}

func cleanupSessionManagerStartup(manager interface{ Shutdown(time.Duration) error }, store kvstore.Store, redisClient *redis.Client, cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
	if redisClient != nil {
		if err := redisClient.Close(); err != nil {
			log.Printf("[SESSION_MANAGER] Redis cleanup failed: %v", err)
		}
	}
	if store != nil {
		if err := store.Close(); err != nil {
			log.Printf("[SESSION_MANAGER] KV store cleanup failed: %v", err)
		}
	}
	if err := manager.Shutdown(5 * time.Second); err != nil {
		log.Printf("[SESSION_MANAGER] Manager cleanup failed: %v", err)
	}
}
