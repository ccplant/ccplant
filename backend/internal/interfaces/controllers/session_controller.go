package controllers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sync"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

// SessionCreator is an interface for creating sessions
type SessionCreator interface {
	CreateSession(ctx context.Context, sessionID string, req entities.StartRequest, userID, userRole string, teams []string) (entities.Session, error)
	DeleteSessionByID(sessionID string) error
}

type sessionStartPreviewer interface {
	PreviewSession(ctx context.Context, sessionID string, req entities.StartRequest, userID, userRole string, teams []string) (*entities.SessionStartPreview, error)
}

type pendingSessionAllocationDeleter interface {
	DeletePendingSessionAllocation(ctx context.Context, sessionID string) (bool, error)
}

type sessionStatusMessageProvider interface {
	StatusMessage() string
}

type sessionErrorCodeProvider interface {
	ErrorCode() string
}

type sessionStatusCacheUpdater interface {
	SetStatusSilent(string)
}

// SessionManagerProvider provides access to the session manager
// This allows the session manager to be swapped at runtime (e.g., for testing)
type SessionManagerProvider interface {
	GetSessionManager() repositories.SessionManager
}

type ESMControlTunnel interface {
	IsConnected(context.Context, string) bool
	Do(context.Context, string, string, string, *http.Request) (*http.Response, error)
}

type esmControlEnqueuer interface {
	Enqueue(context.Context, string, string, string, *http.Request) (string, error)
}

type esmControlCommandReader interface {
	CommandResult(context.Context, string) (bool, int, error)
}

type sessionAnnotationUpdater interface {
	UpdateSessionAnnotations(ctx context.Context, sessionID string, patch entities.UpdateSessionAnnotationsRequest) (entities.SessionAnnotations, error)
}

type sessionAnnotationsProvider interface {
	Annotations() entities.SessionAnnotations
}

type sessionSandboxPolicyProvider interface {
	SandboxPolicyID() string
}

type sessionModelOptionsProvider interface {
	ModelOptions() []string
}

type sessionStatusUsageRecorder interface {
	RecordStatus(context.Context, repositories.SessionStatusEvent) error
	ResolveDimensions(context.Context, string) (repositories.SessionUsageDimensions, error)
	RecordResolvedStatus(context.Context, repositories.SessionUsageDimensions, repositories.SessionStatusEvent) error
}

type sessionConfigurationOwnerProvider interface {
	ConfigurationOwnerReference() (apiVersion, kind, name, uid string)
}

// SessionController handles session management endpoints
type SessionController struct {
	sessionManagerProvider SessionManagerProvider
	sessionCreator         SessionCreator
	validateTeamUC         *sessionuc.ValidateTeamAccessUseCase
	sessionRouteRepo       repositories.SessionRouteRepository
	settingsRepo           repositories.SettingsRepository
	sessionProfileRepo     repositories.SessionProfileRepository
	contextTemplateRepo    repositories.SessionContextTemplateRepository
	sessionStateStore      services.SessionStateStore
	sessionRunnerStore     sessionRunnerAllocationStore
	sessionStatusRecorder  sessionStatusUsageRecorder
	esmControlTunnel       ESMControlTunnel
	statusSubscribersMu    sync.RWMutex
	statusSubscribers      map[uint64]chan repositories.SessionStatusEvent
	nextStatusSubscriberID uint64
	routeStatusBus         repositories.RouteStatusEventBus
	routeStatusOrigin      string
	routeStatusOutbox      chan repositories.SessionStatusEvent
	// routeReconcile remembers, across reconciliation passes, since when a
	// routed runtime has been observed disconnected.
	routeReconcile      routeReconcileState
	githubTokenResolver interface {
		ResolveAccessToken(context.Context, *entities.User, string) (string, error)
		ResolveAccessTokenForOrganization(context.Context, *entities.User, string) (string, string, bool, error)
		IssueBrokerLeaseForOrganization(context.Context, string, string, string) (string, string, bool, error)
		IssueBrokerLeaseForRepositories(context.Context, string, string, string, string, []string, *entities.User) (string, string, bool, error)
		ResolveConnectionURLs(context.Context, string) (string, string, error)
		RevokeBrokerLeases(context.Context, string) error
	}
	sessionTokenDebug     bool
	githubBrokerBaseURL   string
	scheduleSessionTTL    string
	webhookSessionTTL     string
	interactiveSessionTTL string
}

func WithGitHubTokenResolver(resolver interface {
	ResolveAccessToken(context.Context, *entities.User, string) (string, error)
	ResolveAccessTokenForOrganization(context.Context, *entities.User, string) (string, string, bool, error)
	IssueBrokerLeaseForOrganization(context.Context, string, string, string) (string, string, bool, error)
	IssueBrokerLeaseForRepositories(context.Context, string, string, string, string, []string, *entities.User) (string, string, bool, error)
	ResolveConnectionURLs(context.Context, string) (string, string, error)
	RevokeBrokerLeases(context.Context, string) error
}) SessionControllerOption {
	return func(c *SessionController) { c.githubTokenResolver = resolver }
}

// WithSessionTokenDebug enables safe token-routing diagnostics. Token values are never logged.
func WithSessionTokenDebug(enabled bool) SessionControllerOption {
	return func(c *SessionController) { c.sessionTokenDebug = enabled }
}

// WithGitHubBrokerBaseURL sets the session-reachable broker base URL, including any API prefix.
func WithGitHubBrokerBaseURL(baseURL string) SessionControllerOption {
	return func(c *SessionController) { c.githubBrokerBaseURL = baseURL }
}

func WithScheduleSessionTTL(ttl string) SessionControllerOption {
	return func(c *SessionController) { c.scheduleSessionTTL = ttl }
}

func WithWebhookSessionTTL(ttl string) SessionControllerOption {
	return func(c *SessionController) { c.webhookSessionTTL = ttl }
}

func WithInteractiveSessionTTL(ttl string) SessionControllerOption {
	return func(c *SessionController) { c.interactiveSessionTTL = ttl }
}

func applySessionTTLDefault(req *entities.StartRequest, defaultTTL string, reuseSession bool) {
	if defaultTTL == "" || reuseSession {
		return
	}
	if sessionuc.ResolveSessionTTL(req.Params) != "" {
		return
	}
	if req.Params == nil {
		req.Params = &entities.SessionParams{}
	}
	req.Params.SessionTTL = defaultTTL
}

func (c *SessionController) githubRepositoriesForSession(ctx context.Context, req entities.StartRequest, userID string, authzCtx *auth.AuthorizationContext) ([]string, error) {
	repositories := append([]string(nil), req.ProfileGitHubRepositories...)
	for _, repository := range req.ProfileRepositories {
		repositories = append(repositories, "^"+regexp.QuoteMeta(repository.FullName)+"$")
	}
	if len(repositories) == 0 && c.sessionProfileRepo != nil {
		profile, _, err := c.resolveSessionProfile(ctx, req.SessionProfileID, userID, req.Scope, req.TeamID, req.Tags, authzCtx)
		if err != nil {
			return nil, err
		}
		profileConfig, err := sessionuc.ResolveEffectiveSessionProfileConfig(ctx, c.sessionProfileRepo, profile, req.Scope, userID, req.TeamID)
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, profileConfig.GitHubRepositories()...)
		for _, repository := range profileConfig.ProfileRepositories() {
			repositories = append(repositories, "^"+regexp.QuoteMeta(repository.Repository)+"$")
		}
	}
	settingsName := ""
	switch req.Scope {
	case entities.ScopeTeam:
		settingsName = req.TeamID
	case entities.ScopeUser:
		settingsName = userID
	}
	if settingsName != "" && c.settingsRepo != nil {
		exists, err := c.settingsRepo.Exists(ctx, settingsName)
		if err != nil {
			return nil, fmt.Errorf("check scoped GitHub repositories: %w", err)
		}
		if exists {
			settings, err := c.settingsRepo.FindByName(ctx, settingsName)
			if err != nil {
				return nil, fmt.Errorf("load scoped GitHub repositories: %w", err)
			}
			if settings != nil {
				repositories = append(repositories, settings.GitHubRepositories()...)
			}
		}
	}
	return normalizeRepositoryPatterns(repositories)
}

func (c *SessionController) githubRoutingDefaults(ctx context.Context, req entities.StartRequest, userID string, authzCtx *auth.AuthorizationContext) (string, string, error) {
	repository := sessionRepository(req)
	connectionID := ""
	if req.Params != nil {
		connectionID = req.Params.ConnectionID
	}
	if c.sessionProfileRepo == nil || (repository != "" && connectionID != "") {
		return repository, connectionID, nil
	}
	profile, _, err := c.resolveSessionProfile(ctx, req.SessionProfileID, userID, req.Scope, req.TeamID, req.Tags, authzCtx)
	if err != nil {
		return "", "", err
	}
	config, err := sessionuc.ResolveEffectiveSessionProfileConfig(ctx, c.sessionProfileRepo, profile, req.Scope, userID, req.TeamID)
	if err != nil {
		return "", "", err
	}
	if connectionID == "" && config.Params() != nil {
		connectionID = config.Params().ConnectionID
	}
	if repository == "" {
		if repositories := config.ProfileRepositories(); len(repositories) > 0 {
			repository = repositories[0].Repository
		}
	}
	return repository, connectionID, nil
}

// NewSessionController creates a new SessionController instance
func NewSessionController(
	sessionManagerProvider SessionManagerProvider,
	sessionCreator SessionCreator,
	opts ...SessionControllerOption,
) *SessionController {
	c := &SessionController{
		sessionManagerProvider: sessionManagerProvider,
		sessionCreator:         sessionCreator,
		validateTeamUC:         sessionuc.NewValidateTeamAccessUseCase(),
		statusSubscribers:      make(map[uint64]chan repositories.SessionStatusEvent),
		routeStatusOrigin:      uuid.NewString(),
		routeStatusOutbox:      make(chan repositories.SessionStatusEvent, routeStatusOutboxSize),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SessionControllerOption is a functional option for SessionController
type SessionControllerOption func(*SessionController)

// WithSessionRouteRepository sets the session route repository on the controller
func WithSessionRouteRepository(repo repositories.SessionRouteRepository) SessionControllerOption {
	return func(c *SessionController) {
		c.sessionRouteRepo = repo
	}
}

// WithSettingsRepository sets the settings repository on the controller
func WithSettingsRepository(repo repositories.SettingsRepository) SessionControllerOption {
	return func(c *SessionController) {
		c.settingsRepo = repo
	}
}

// WithSessionProfileRepository sets the session profile repository on the controller
func WithSessionProfileRepository(repo repositories.SessionProfileRepository) SessionControllerOption {
	return func(c *SessionController) {
		c.sessionProfileRepo = repo
	}
}

func WithSessionContextTemplates(repo repositories.SessionContextTemplateRepository, store services.SessionStateStore) SessionControllerOption {
	return func(c *SessionController) { c.contextTemplateRepo, c.sessionStateStore = repo, store }
}

func WithESMControlTunnel(tunnel ESMControlTunnel) SessionControllerOption {
	return func(c *SessionController) { c.esmControlTunnel = tunnel }
}

type sessionRunnerAllocationStore interface {
	GetAllocation(context.Context, string) (*sessionrunnercore.Allocation, error)
}

func WithSessionRunnerStore(store sessionRunnerAllocationStore) SessionControllerOption {
	return func(c *SessionController) { c.sessionRunnerStore = store }
}

func WithSessionStatusUsageRecorder(recorder sessionStatusUsageRecorder) SessionControllerOption {
	return func(c *SessionController) { c.sessionStatusRecorder = recorder }
}

func (c *SessionController) SetSessionStatusUsageRecorder(recorder sessionStatusUsageRecorder) {
	c.sessionStatusRecorder = recorder
}

// getSessionManager returns the current session manager
func (c *SessionController) getSessionManager() repositories.SessionManager {
	return c.sessionManagerProvider.GetSessionManager()
}

// GetName returns the name of this handler for logging
func (c *SessionController) GetName() string {
	return "SessionController"
}

// RegisterRoutes registers session management routes
func (c *SessionController) RegisterRoutes(e *echo.Echo) error {
	// Session management routes
	e.POST("/start", c.StartSession)
	e.GET("/search", c.SearchSessions)
	e.PATCH("/sessions/:sessionId/annotations", c.UpdateSessionAnnotations)
	e.POST("/sessions/:sessionId/resume", c.ResumeSession)
	e.POST("/sessions/:sessionId/restart", c.RestartSession)
	e.GET("/sessions/:sessionId/restart", c.RestartStatus)
	e.POST("/sessions/:sessionId/pause", c.PauseSession)
	e.POST("/sessions/:sessionId/templateize", c.TemplateizeSession)
	e.POST("/sessions/:sessionId/workspace", c.SaveSessionAsWorkspace)
	e.GET("/workspaces", c.ListWorkspaces)
	e.GET("/workspaces/:workspaceId", c.GetWorkspace)
	e.PATCH("/workspaces/:workspaceId", c.UpdateWorkspace)
	e.DELETE("/workspaces/:workspaceId", c.DeleteWorkspace)
	e.GET("/session-context-templates", c.ListSessionContextTemplates)
	e.GET("/session-context-templates/:templateId", c.GetSessionContextTemplate)
	e.PATCH("/session-context-templates/:templateId", c.UpdateSessionContextTemplate)
	e.DELETE("/session-context-templates/:templateId", c.DeleteSessionContextTemplate)
	e.DELETE("/sessions/:sessionId", c.DeleteSession)

	// Session proxy route
	e.Any("/:sessionId/*", c.RouteToSession)

	log.Printf("Registered session management routes")
	return nil
}

// StartSession handles POST /start requests to start a new agentapi server
func (c *SessionController) StartSession(ctx echo.Context) error {
	return telemetry.LoggedOperationErr(ctx.Request().Context(), "controllers.SessionController.StartSession", func(requestCtx context.Context) error {
		ctx.SetRequest(ctx.Request().WithContext(requestCtx))
		return c.startSession(ctx)
	})
}
