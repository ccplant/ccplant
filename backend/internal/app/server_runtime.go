package app

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/di"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/sessioncount"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes"
)

func (s *Server) StartMonitoring() {
	// Session monitoring disabled - notifications handled by Claude Code hooks
}

// StartSessionCountWorker records the initial state once and then consumes
// session status notifications. It performs no periodic polling.
func (s *Server) StartSessionCountWorker(ctx context.Context) {
	if s.sessionCountRepo == nil {
		return
	}
	source, ok := s.sessionManager.(interface {
		GetSession(string) entities.Session
		ListSessions(entities.SessionFilter) []entities.Session
		SubscribeStatusEvents() (<-chan portrepos.SessionStatusEvent, func())
	})
	if !ok {
		log.Printf("[SESSION_COUNT_EVENTS] Session manager does not expose status notifications")
		return
	}
	worker := sessioncount.NewWorker(source, s.sessionRunnerStore, s.sessionRouteRepo, s.teamConfigRepo, s.sessionCountRepo)
	s.sessionCountWorker = worker
	if s.router != nil && s.router.handlers != nil && s.router.handlers.sessionController != nil {
		s.router.handlers.sessionController.SetSessionStatusUsageRecorder(worker)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	s.sessionCountCancel = cancel
	s.sessionCountWorkers.Add(1)
	go func() {
		defer s.sessionCountWorkers.Done()
		worker.Run(workerCtx)
	}()
	log.Printf("[SESSION_COUNT_EVENTS] Started status event recorder")
}

// loggingMiddleware returns Echo middleware for request logging
func (s *Server) loggingMiddleware() echo.MiddlewareFunc {
	return echo.MiddlewareFunc(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			log.Printf("Request: %s %s from %s", req.Method, req.URL.Path, req.RemoteAddr)
			return next(c)
		}
	})
}

// setupRoutes configures the router with all defined routes
func (s *Server) setupRoutes() {
	// Register non-auth routes using Router
	s.router = NewRouter(s.echo, s)
	if err := s.router.RegisterRoutes(); err != nil {
		log.Printf("Failed to register routes: %v", err)
	}

	// Register auth-related routes directly
	s.setupAuthRoutes()
}

// apiTokenInitDeps bundles the optional dependencies gathered during
// NewServer so the fail-prone migration/bootstrap step can run later (in
// InitAPITokens) without re-deriving them. Keeping them on the Server also
// lets tests drive initialization explicitly.
type apiTokenInitDeps struct {
	authService *services.SimpleAuthService
}

// initAPITokenWiring records the auth service and derives the repositories
// needed to run migration/bootstrap later. It must not perform any I/O or
// fail-prone work so that NewServer remains side-effect-free with respect to
// API token initialization.
func (s *Server) initAPITokenWiring() {
	simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService)
	if !ok {
		return
	}
	if s.apiTokenRepo == nil {
		return
	}
	s.apiTokenDeps = &apiTokenInitDeps{
		authService: simpleAuth,
	}
	// Wire the repository into the auth service so the background reconciler
	// can keep named tokens consistent across replicas. Legacy static and
	// personal API keys live in a separate map and are unaffected.
	simpleAuth.SetAPITokenRepository(s.apiTokenRepo)
	simpleAuth.SetLocalUserRepository(s.localUserRepo)
}

// InitAPITokens loads named API tokens into the in-memory auth service.
func (s *Server) InitAPITokens(ctx context.Context) error {
	if s.apiTokenRepo == nil || s.apiTokenDeps == nil {
		return nil
	}
	deps := s.apiTokenDeps
	if deps.authService == nil {
		return nil
	}
	if err := services.BootstrapAPITokens(ctx, deps.authService, s.apiTokenRepo); err != nil {
		return fmt.Errorf("api token bootstrap: %w", err)
	}
	return nil
}

// StartAPITokenReconciler launches a background goroutine that periodically
// reconciles the in-memory named-token map with the repository so revocation
// of a named token propagates across replicas within the given interval. It
// returns immediately; the goroutine stops when ctx is canceled. No-op when
// the named-token subsystem is not configured.
func (s *Server) StartAPITokenReconciler(ctx context.Context, interval time.Duration) {
	if s.apiTokenRepo == nil || s.apiTokenDeps == nil || s.apiTokenDeps.authService == nil {
		return
	}
	if interval <= 0 {
		return
	}
	authService := s.apiTokenDeps.authService
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := authService.ReconcileAPITokens(ctx); err != nil {
					log.Printf("[RECONCILE] api token reconciliation failed: %v", err)
				}
			}
		}
	}()
	log.Printf("[RECONCILE] api token reconciler started (interval: %s)", interval)
}

// AddCustomHandler adds a custom handler to the router
func (s *Server) AddCustomHandler(handler CustomHandler) {
	if s.router != nil {
		s.router.AddCustomHandler(handler)
		// Register routes immediately since router is already initialized
		if err := handler.RegisterRoutes(s.echo); err != nil {
			log.Printf("Failed to register custom handler %s: %v", handler.GetName(), err)
		}
	}
}

// GetSessionManager returns the session manager
func (s *Server) GetSessionManager() portrepos.SessionManager {
	return s.sessionManager
}

// GetScheduleManager returns the API-owned schedule persistence manager.
func (s *Server) GetScheduleManager() schedule.Manager {
	return s.scheduleManager
}

// GetPersistenceClient returns the Secret/ConfigMap client used by all
// application repositories outside KubernetesSessionManager.
func (s *Server) GetPersistenceClient() kubernetes.Interface {
	return s.persistenceClient
}

// GetApplicationKVStore returns the storage-neutral application persistence
// boundary. Kubernetes-only deployments are exposed through the Store adapter
// just like libSQL and replicated deployments.
func (s *Server) GetApplicationKVStore() kvstore.Store {
	if s.kvStore != nil {
		return s.kvStore
	}
	return kvstore.NewKubernetesStore(s.persistenceClient)
}

// ResolveApplicationUserIDForGitHubUser maps a GitHub webhook sender to the
// same stable application user ID used when personal credentials are saved.
func (s *Server) ResolveApplicationUserIDForGitHubUser(ctx context.Context, githubUserID int64, login string) (string, error) {
	if s.router == nil || s.router.handlers == nil || s.router.handlers.githubConnectionsController == nil {
		return strings.TrimSpace(login), nil
	}
	return s.router.handlers.githubConnectionsController.ResolveApplicationUserIDForGitHubUser(ctx, githubUserID, login)
}

// SetSessionManager allows configuration of a custom session manager (for testing)
func (s *Server) SetSessionManager(manager portrepos.SessionManager) {
	s.sessionManager = manager
}

// GetShareRepository returns the share repository
func (s *Server) GetShareRepository() portrepos.ShareRepository {
	return s.shareRepo
}

// SetShareRepository allows configuration of a custom share repository (for testing)
func (s *Server) SetShareRepository(repo portrepos.ShareRepository) {
	s.shareRepo = repo
}

// GetContainer returns the DI container
func (s *Server) GetContainer() *di.Container {
	return s.container
}

// GetSessionRouteRepository returns the session route repository
func (s *Server) GetSessionRouteRepository() portrepos.SessionRouteRepository {
	return s.sessionRouteRepo
}

// CreateSession creates a new agent session
