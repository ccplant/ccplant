package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	infraallocation "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/runtimeconfig"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"k8s.io/client-go/kubernetes"
)

type sessionManagerStartup struct {
	config         *config.Config
	manager        *services.KubernetesSessionManager
	remote         bool
	store          kvstore.Store
	client         kubernetes.Interface
	namespace      string
	context        context.Context
	cancel         context.CancelFunc
	authentication *repositories.KubernetesCredentialsRepository
	settings       portrepos.SettingsRepository
	routes         portrepos.SessionRouteRepository
	state          services.SessionStateStore
	control        sessioncontrol.Store
	redis          *redis.Client
}

func (b *sessionManagerStartup) initializePersistence() error {
	cfg, manager, remoteMode := b.config, b.manager, b.remote
	var applicationStore kvstore.Store
	var err error
	persistence := manager.GetClient()
	if !remoteMode {
		var wrapped bool
		applicationStore, wrapped, err = buildApplicationKVStore(cfg.KVStore, persistence)
		b.store = applicationStore
		if err != nil {
			return fmt.Errorf("initialize session-manager KV store: %w", err)
		}
		if wrapped {
			persistence = kvstore.NewKubernetesAdapter(persistence, applicationStore)
		}
	}
	b.client = persistence
	return nil
}

func (b *sessionManagerStartup) startConfiguration(parent context.Context) {
	cfg, manager, applicationStore := b.config, b.manager, b.store
	applicationNamespace := cfg.KVStore.Namespace
	if applicationNamespace == "" {
		applicationNamespace = "default"
	}
	runtimeProvider := runtimeconfig.New(cfg, applicationStore, applicationNamespace)
	if err := runtimeProvider.Reload(parent); err != nil {
		log.Printf("[SESSION_MANAGER] Runtime settings reload failed; using startup configuration: %v", err)
	} else if runtimeProvider.Version() > 0 {
		cfg = runtimeProvider.Current()
	}
	manager.SetConfigProvider(runtimeProvider)
	runtimeCtx, runtimeCancel := context.WithCancel(parent)
	runtimeProvider.Start(runtimeCtx, 30*time.Second, func(err error) {
		log.Printf("[SESSION_MANAGER] Runtime settings reload failed: %v", err)
	})
	b.config, b.namespace = cfg, applicationNamespace
	b.context, b.cancel = runtimeCtx, runtimeCancel
}

func (b *sessionManagerStartup) initializeRepositories() error {
	cfg, manager, remoteMode := b.config, b.manager, b.remote
	persistence, applicationNamespace := b.client, b.namespace
	var credentialsRepo *repositories.KubernetesCredentialsRepository
	var settingsRepo portrepos.SettingsRepository
	var sessionRouteRepo portrepos.SessionRouteRepository
	if !remoteMode {
		registry, registryErr := newSessionManagerEncryptionRegistry()
		if registryErr != nil {
			return registryErr
		}
		settingsRepo = repositories.NewKubernetesSettingsRepository(persistence, applicationNamespace, registry)
		credentialsRepo = repositories.NewKubernetesCredentialsRepository(persistence, applicationNamespace)
		manager.SetSettingsRepository(settingsRepo)
		manager.SetCredentialsRepository(credentialsRepo)
		manager.SetUserFileRepository(repositories.NewKubernetesUserFileRepository(persistence, applicationNamespace))
		manager.SetTeamConfigRepository(repositories.NewKubernetesTeamConfigRepository(persistence, applicationNamespace))
		manager.SetPersonalAPIKeyRepository(repositories.NewKubernetesPersonalAPIKeyRepository(persistence, applicationNamespace))
		manager.SetSandboxPolicyRepository(repositories.NewKubernetesSandboxPolicyRepository(persistence, applicationNamespace))
		manager.SetSessionProfileRepository(repositories.NewKubernetesSessionProfileRepository(persistence, applicationNamespace, registry))
		sessionRouteRepo = repositories.NewKubernetesSessionRouteRepository(persistence, applicationNamespace)
		manager.AddSessionDeletedHandler(func(ctx context.Context, session entities.Session) {
			cleanupLocalSessionRoutes(ctx, sessionRouteRepo, session.ID())
		})
	}
	statusRepo := buildSessionManagerStatusEventRepository(cfg, remoteMode)
	manager.SetStatusEventRepository(statusRepo)
	if cache, ok := statusRepo.(portrepos.SessionListCacheRepository); ok {
		manager.SetSessionListCacheRepository(cache)
	}
	b.authentication, b.settings, b.routes = credentialsRepo, settingsRepo, sessionRouteRepo
	return nil
}

func (b *sessionManagerStartup) initializeState(parent context.Context) error {
	cfg, manager, remoteMode := b.config, b.manager, b.remote
	stateStore, err := services.NewSessionStateStore(parent, cfg.SessionPersistence)
	if err != nil {
		return fmt.Errorf("initialize session state store: %w", err)
	}
	controlStore := buildSessionManagerControlStore(cfg, remoteMode)
	if controlStore != nil {
		manager.SetSessionControlStore(controlStore)
	}
	b.state, b.control = stateStore, controlStore
	return nil
}

func (b *sessionManagerStartup) initializeRedis() error {
	cfg, manager, remoteMode := b.config, b.manager, b.remote
	var redisClient *redis.Client
	var err error
	// A manager registered with a parent is an execution plane only: the parent
	// owns allocations and their claim leases. Remote replicas coordinate their
	// single upstream poller with a Kubernetes Lease below.
	if !remoteMode {
		redisClient, err = newSessionManagerRedis(cfg)
		b.redis = redisClient
		if err != nil {
			return err
		}
	}
	if !remoteMode {
		manager.SetSessionAllocationNotifier(infraallocation.NewRedisNotifier(redisClient))
		manager.SetSessionAllocatorEnabled(true)
	}
	return nil
}

func (b *sessionManagerStartup) router() (*echo.Echo, error) {
	return newSessionManagerRuntimeRouter(sessionManagerRouteDependencies{
		config: b.config, manager: b.manager, redis: b.redis, state: b.state,
		settings: b.settings, authentication: b.authentication, control: b.control, remote: b.remote,
	})
}
