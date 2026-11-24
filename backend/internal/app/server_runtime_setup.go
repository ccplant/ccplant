package app

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/runtimeconfig"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"k8s.io/client-go/kubernetes"
)

type serverRuntimeConfig struct {
	config      *config.Config
	persistence kubernetes.Interface
	namespace   string
	kvStore     kvstore.Store
	k8sManager  *services.KubernetesSessionManager
}

type serverRuntime struct {
	config             *config.Config
	provider           *runtimeconfig.Provider
	context            context.Context
	cancel             context.CancelFunc
	allocationNotifier sessionrunnercore.AllocationNotifier
	allocationRedis    *redis.Client
	usage              portrepos.UsageRepository
	sessionCount       portrepos.SessionCountRepository
	routeStatusBus     portrepos.RouteStatusEventBus
}

func buildServerRuntime(input serverRuntimeConfig) serverRuntime {
	backfillServerLegacySettings(input.config, input.persistence, input.namespace)
	provider := runtimeconfig.New(input.config, input.kvStore, input.namespace)
	if err := provider.Reload(context.Background()); err != nil {
		log.Printf("[RUNTIME_CONFIG] Failed to load versioned settings; using startup configuration: %v", err)
	} else if provider.Version() > 0 {
		input.config = provider.Current()
		log.Printf("[RUNTIME_CONFIG] Loaded system settings version %d", provider.Version())
	}
	if input.k8sManager != nil {
		input.k8sManager.SetConfigProvider(provider)
	}
	runtimeContext, cancel := context.WithCancel(context.Background())
	provider.Start(runtimeContext, 30*time.Second, func(err error) { log.Printf("[RUNTIME_CONFIG] Reload failed: %v", err) })
	notifier, redisClient := buildSessionAllocationNotifier(input.config)
	usage, sessionCount := buildOptionalMetricsRepositories(input.config)
	routeStatusBus := configureStatusRepositories(input.config, input.k8sManager)
	return serverRuntime{
		config: input.config, provider: provider, context: runtimeContext, cancel: cancel,
		allocationNotifier: notifier, allocationRedis: redisClient, usage: usage, sessionCount: sessionCount, routeStatusBus: routeStatusBus,
	}
}

func buildOptionalMetricsRepositories(cfg *config.Config) (portrepos.UsageRepository, portrepos.SessionCountRepository) {
	var usage portrepos.UsageRepository
	var sessionCount portrepos.SessionCountRepository
	var err error
	if cfg.Usage.Enabled {
		usage, err = repositories.NewLibSQLUsageRepository(context.Background(), cfg.Usage.DatabaseURL, cfg.Usage.AuthToken)
		if err != nil {
			log.Fatalf("Failed to initialize usage store: %v", err)
		}
	}
	if cfg.SessionCount.Enabled {
		sessionCount, err = repositories.NewSessionCountRepository(context.Background(), cfg.SessionCount.Backend, cfg.SessionCount.DatabaseURL, cfg.SessionCount.AuthToken)
		if err != nil {
			log.Fatalf("Failed to initialize session count store: %v", err)
		}
	}
	return usage, sessionCount
}

func configureStatusRepositories(cfg *config.Config, manager *services.KubernetesSessionManager) portrepos.RouteStatusEventBus {
	statusRepository := buildStatusEventRepository(cfg)
	if manager != nil {
		manager.SetStatusEventRepository(statusRepository)
		if listCache, ok := statusRepository.(portrepos.SessionListCacheRepository); ok {
			manager.SetSessionListCacheRepository(listCache)
		}
	}
	routeStatusBus, _ := statusRepository.(portrepos.RouteStatusEventBus)
	return routeStatusBus
}
