package app

import (
	"context"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/di"
	infraesmcontrol "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/sessioncount"
	"github.com/takutakahashi/agentapi-proxy/internal/runtimeconfig"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	storageattachmentuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/storageattachment"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
	"k8s.io/client-go/kubernetes"
)

// Server represents the HTTP server
type Server struct {
	config                      *config.Config
	configProvider              *runtimeconfig.Provider
	runtimeConfigCancel         context.CancelFunc
	echo                        *echo.Echo
	verbose                     bool
	logger                      *logger.Logger
	oauthProvider               *auth.GitHubOAuthProvider
	oauthSessions               sync.Map // sessionID -> OAuthSession
	notificationSvc             *notification.Service
	container                   *di.Container            // Internal DI container
	sessionManager              portrepos.SessionManager // Session lifecycle manager
	codexDeviceAuthLauncher     codexauth.WorkloadLauncher
	persistenceClient           kubernetes.Interface // Secret/ConfigMap client for non-session application data
	kvStore                     kvstore.Store        // non-nil when persistenceClient is backed by libSQL
	usageRepo                   portrepos.UsageRepository
	sessionCountRepo            portrepos.SessionCountRepository
	sessionCountWorker          *sessioncount.Worker
	sessionCountWorkers         sync.WaitGroup
	sessionCountCancel          context.CancelFunc
	settingsRepo                portrepos.SettingsRepository      // Settings repository
	credentialsRepo             portrepos.CredentialsRepository   // Credentials repository
	shareRepo                   portrepos.ShareRepository         // Share repository for session sharing
	teamConfigRepo              portrepos.TeamConfigRepository    // Team configuration repository
	sandboxPolicyRepo           portrepos.SandboxPolicyRepository // Sandbox policy repository
	storageRepo                 portrepos.StorageRepository       // Scoped persistent storage metadata
	storageAttachmentResolver   *storageattachmentuc.Resolver
	storageObjectStore          services.StorageObjectStore                     // Immutable object_snapshot blobs
	sandboxDomainRepo           *repositories.KubernetesSandboxDomainRepository // Sandbox domain log repository
	sessionRouteRepo            portrepos.SessionRouteRepository                // Session route repository for External Session Manager routing
	sessionRunnerStore          sessionrunnercore.Store                         // Cluster-wide managers, pools, bindings, runners and pool allocations
	sessionAllocationNotifier   sessionrunnercore.AllocationNotifier            // Wakes long-polling runners when durable allocations are created
	sessionAllocationRedis      *redis.Client
	routeStatusBus              portrepos.RouteStatusEventBus
	routeStatusCancel           context.CancelFunc
	userFileRepo                portrepos.UserFileRepository       // User-managed files repository
	sessionProfileRepo          portrepos.SessionProfileRepository // Session profile repository
	contextTemplateRepo         portrepos.SessionContextTemplateRepository
	scheduleManager             schedule.Manager
	apiTokenRepo                portrepos.APITokenRepository // Named API token repository
	localUserRepo               portrepos.LocalUserRepository
	apiTokenDeps                *apiTokenInitDeps // Wiring for bootstrap/reconcile
	sessionStateStore           services.SessionStateStore
	sessionControlStore         sessioncontrol.Store
	esmControlStore             esmcontrol.Store
	esmControlTunnel            *infraesmcontrol.Tunnel
	directSessionRuntimeEnabled bool
	localSessionFallbackEnabled bool
	namespace                   string
	personalAPIKeyRepo          portrepos.PersonalAPIKeyRepository
	router                      *Router // Router for custom handler registration
}

// NewServer creates a new server instance
func NewServer(cfg *config.Config, verbose bool) *Server {
	e := newServerHTTP()
	container := di.NewContainer()
	lgr := logger.NewLogger()
	infrastructure := buildSessionInfrastructure(cfg, verbose, lgr)
	runtime := buildServerRuntime(serverRuntimeConfig{
		config: cfg, persistence: infrastructure.persistence, namespace: infrastructure.namespace,
		kvStore: infrastructure.kvStore, k8sManager: infrastructure.k8sManager,
	})
	cfg = runtime.config
	serverRepos := buildServerRepositories(serverRepositoryConfig{
		config: cfg, persistence: infrastructure.persistence, namespace: infrastructure.namespace,
		kvStore: infrastructure.kvStore, runtimeContext: runtime.context,
		k8sManager: infrastructure.k8sManager, remoteManager: infrastructure.remoteManager,
		container: container, logger: lgr, encryption: newServerEncryptionRegistry(),
	})
	sessionServices := buildServerSessionServices(cfg, infrastructure, serverRepos)
	s := assembleServer(serverAssembly{
		config: cfg, verbose: verbose, echo: e, logger: lgr, container: container,
		infrastructure: infrastructure, runtime: runtime, repositories: serverRepos, services: sessionServices,
	})
	s.finishInitialization()
	return s
}
