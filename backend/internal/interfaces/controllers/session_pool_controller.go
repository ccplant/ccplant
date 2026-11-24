package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/buildinfo"
	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type SessionPoolController struct {
	store    core.Store
	resolver *core.Resolver
	notifier core.AllocationNotifier
	liveness managerLiveness
	routes   portrepos.SessionRouteRepository
	profile  interface {
		ExternalRuntimeProfile() *sessionsettings.RuntimeProfile
	}
	now           func() time.Time
	managerTunnel ESMControlTunnel
	managedFiles  managedFileSnapshotStore
	statusEvents  routeStatusPublisher
}

type routeStatusPublisher interface {
	NotifyRouteStatus(portrepos.SessionStatusEvent)
}

func (c *SessionPoolController) WithRouteStatusPublisher(publisher routeStatusPublisher) *SessionPoolController {
	c.statusEvents = publisher
	return c
}

func (c *SessionPoolController) notifyRouteStatus(route *portrepos.SessionRoute) {
	if c.statusEvents == nil || route == nil {
		return
	}
	c.statusEvents.NotifyRouteStatus(portrepos.SessionStatusEvent{SessionID: route.SessionID, Status: route.Status, Timestamp: route.StatusUpdatedAt})
}

func (c *SessionPoolController) WithManagedFileStore(store managedFileSnapshotStore) *SessionPoolController {
	c.managedFiles = store
	return c
}

// SaveManagerManagedFiles persists snapshots relayed by an authenticated ESM
// in the parent-owned application store.
func (c *SessionPoolController) SaveManagerManagedFiles(ctx echo.Context) error {
	manager, err := c.authenticateManager(ctx)
	if err != nil {
		return err
	}
	if c.managedFiles == nil || c.routes == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "managed-file persistence is unavailable")
	}
	route, err := c.routes.Get(ctx.Request().Context(), ctx.Param("sessionId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session route is unavailable")
	}
	// ESMs address their local allocated workload ID, while routes are keyed by
	// the public parent session ID. Resolve that alias without trusting the
	// owner supplied in the request body.
	if route == nil || route.ManagerID != manager.ID {
		routes, listErr := c.routes.List(ctx.Request().Context(), "")
		if listErr != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "session route is unavailable")
		}
		route = nil
		for _, candidate := range routes {
			if candidate != nil && candidate.ManagerID == manager.ID && candidate.RemoteSessionID == ctx.Param("sessionId") {
				route = candidate
				break
			}
		}
	}
	if route == nil || route.ManagerID != manager.ID {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	var req struct {
		Owner    string                        `json:"owner"`
		SyncID   string                        `json:"sync_id,omitempty"`
		Files    []sessionsettings.ManagedFile `json:"files"`
		Expected map[string]string             `json:"expected,omitempty"`
	}
	if err := ctx.Bind(&req); err != nil || req.Owner == "" || req.Owner != route.UserID {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid managed-file snapshot")
	}
	total := 0
	allowed := make(map[string]bool, len(sessionsettings.ManagedFileTypes))
	for _, path := range sessionsettings.ManagedFileTypes {
		allowed[path] = true
	}
	for _, file := range req.Files {
		total += len(file.Content)
		if !allowed[file.Path] || total > maxManagedFileSnapshotBytes {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid managed-file snapshot")
		}
	}
	if req.SyncID != "" {
		conditional, ok := c.managedFiles.(interface {
			SaveFilesIfUnchanged(context.Context, string, []sessionsettings.ManagedFile, map[string]string) error
		})
		if !ok || req.Expected == nil {
			return ctx.NoContent(http.StatusConflict)
		}
		if err := conditional.SaveFilesIfUnchanged(ctx.Request().Context(), req.Owner, req.Files, req.Expected); err != nil {
			return ctx.NoContent(http.StatusConflict)
		}
		return ctx.NoContent(http.StatusNoContent)
	}
	if err := c.managedFiles.SaveFiles(ctx.Request().Context(), req.Owner, req.Files); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "managed-file persistence is unavailable")
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionPoolController) WithManagerTunnel(tunnel ESMControlTunnel) *SessionPoolController {
	c.managerTunnel = tunnel
	return c
}

type managerLiveness interface {
	TouchManager(context.Context, string, string) error
	IsManagerConnected(context.Context, string) (bool, error)
}

type managerScopedSessionRunnerStore interface {
	ListManagerRunners(context.Context, string) ([]*core.Runner, error)
	ListManagerAllocations(context.Context, string) ([]*core.Allocation, error)
}

const (
	sessionManagerHeartbeatTTL = 90 * time.Second
	// Idle runners continuously long-poll for allocations. Their authenticated
	// polls refresh LastSeen, so records older than this no longer represent a
	// live workload and must not suppress stock-runner reconciliation.
	sessionRunnerHeartbeatTTL = 3 * time.Minute
	// A resume normally recreates its workload within the startup window. After
	// this grace period, a missing workload can safely become resumable again.
	missingResumingRunnerRecoveryDelay = 2 * time.Minute
	// Draining records fence workloads while the manager removes them. Keep the
	// tombstone long enough for delayed registrations to be rejected, then
	// collect it once manager inventory and allocations both confirm it is gone.
	sessionRunnerDrainingRetention = 10 * time.Minute
)

func NewSessionPoolController(store core.Store, routes portrepos.SessionRouteRepository, providers ...interface {
	ExternalRuntimeProfile() *sessionsettings.RuntimeProfile
}) *SessionPoolController {
	c := &SessionPoolController{store: store, resolver: core.NewResolver(store, sessionManagerHeartbeatTTL), routes: routes, now: func() time.Time { return time.Now().UTC() }}
	if len(providers) > 0 {
		c.profile = providers[0]
	}
	return c
}

func (c *SessionPoolController) WithManagerLiveness(liveness managerLiveness) *SessionPoolController {
	c.liveness = liveness
	c.resolver.WithManagerLiveness(liveness)
	return c
}

func (c *SessionPoolController) WithAllocationNotifier(notifier core.AllocationNotifier) *SessionPoolController {
	c.notifier = notifier
	return c
}

func (c *SessionPoolController) GetManagerRuntimeProfile(ctx echo.Context) error {
	_, err := c.authenticateManager(ctx)
	if err != nil {
		return err
	}
	if c.profile == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "runtime profile is unavailable")
	}
	profile := c.profile.ExternalRuntimeProfile()
	if profile == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "runtime profile is unavailable")
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "marshal runtime profile")
	}
	digest := sha256.Sum256(raw)
	return ctx.JSON(http.StatusOK, &sessionallocation.RuntimeProfileSnapshot{Revision: hex.EncodeToString(digest[:]), Profile: profile})
}

// GetManagerVersion returns the release version over the manager-authenticated
// control plane. Managers can migrate from the public health response to this
// endpoint without introducing a separate credential.
func (c *SessionPoolController) GetManagerVersion(ctx echo.Context) error {
	if _, err := c.authenticateManager(ctx); err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, map[string]string{"version": buildinfo.Version})
}

type managerCreateRequest struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Labels       map[string]string `json:"labels,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
}

type managerRegistrationRequest struct {
	Name   string            `json:"name"`
	Scope  core.ManagerScope `json:"scope"`
	TeamID string            `json:"team_id,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

type managerEnrollRequest struct {
	RegistrationToken string            `json:"registration_token"`
	InstanceID        string            `json:"instance_id"`
	Name              string            `json:"name,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Capabilities      []string          `json:"capabilities,omitempty"`
}

// IssueManagerRegistrationToken creates a pending manager in the unified
// user/team/system ownership model.
func (c *SessionPoolController) IssueManagerRegistrationToken(ctx echo.Context) error {
	var input managerRegistrationRequest
	if err := ctx.Bind(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	if input.Scope == "" {
		input.Scope = core.ManagerScopeUser
	}
	ownerID := string(user.ID())
	switch input.Scope {
	case core.ManagerScopeUser:
	case core.ManagerScopeTeam:
		if input.TeamID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required")
		}
		if !user.IsAdmin() && !user.IsMemberOfTeam(input.TeamID) {
			return echo.NewHTTPError(http.StatusForbidden, "access denied")
		}
		ownerID = input.TeamID
	case core.ManagerScopeSystem:
		if !user.IsAdmin() {
			return echo.NewHTTPError(http.StatusForbidden, "admin permission is required for system scope")
		}
		ownerID = ""
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "scope must be user, team or system")
	}
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create registration token")
	}
	manager := &core.Manager{
		Name: input.Name, Scope: input.Scope, OwnerID: ownerID, Labels: input.Labels,
		Enabled: false, RegistrationTokenHash: tokenHash,
		RegistrationExpiresAt: c.now().Add(15 * time.Minute),
	}
	if err := c.store.CreateManager(ctx.Request().Context(), manager); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusCreated, map[string]any{
		"manager": redactManager(manager), "registration_token": token,
		"expires_at": manager.RegistrationExpiresAt,
	})
}

// EnrollManager exchanges a short-lived registration token for the durable
// connection credential used by heartbeat and allocation APIs.
func (c *SessionPoolController) EnrollManager(ctx echo.Context) error {
	var input managerEnrollRequest
	if err := ctx.Bind(&input); err != nil || strings.TrimSpace(input.RegistrationToken) == "" || strings.TrimSpace(input.InstanceID) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "registration_token and instance_id are required")
	}
	managers, err := c.store.ListManagers(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	now := c.now()
	for _, manager := range managers {
		if manager.RegistrationTokenHash == "" || !verifySessionRunnerToken(manager.RegistrationTokenHash, input.RegistrationToken) {
			continue
		}
		if manager.RegistrationExpiresAt.IsZero() || now.After(manager.RegistrationExpiresAt) {
			return echo.NewHTTPError(http.StatusUnauthorized, "registration token has expired")
		}
		connectionToken, connectionHash, generateErr := newSessionRunnerToken()
		if generateErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to create connection token")
		}
		manager.ConnectionTokenHash = connectionHash
		manager.RegistrationTokenHash = ""
		manager.RegistrationExpiresAt = time.Time{}
		manager.Enabled = true
		if name := strings.TrimSpace(input.Name); name != "" {
			if len([]rune(name)) > 100 {
				return echo.NewHTTPError(http.StatusBadRequest, "name must be 100 characters or fewer")
			}
			manager.Name = name
		}
		if input.Labels != nil {
			manager.Labels = input.Labels
		}
		manager.Capabilities = input.Capabilities
		if len(manager.Capabilities) == 0 {
			manager.Capabilities = []string{core.CapabilityRunnerClaimV1, core.CapabilityDirectRuntimeV1}
		}
		if err := c.store.UpdateManager(ctx.Request().Context(), manager); err != nil {
			return sessionRunnerStoreError(err)
		}
		return ctx.JSON(http.StatusOK, map[string]any{
			"id": manager.ID, "name": manager.Name, "scope": manager.Scope,
			"owner_id": manager.OwnerID, "labels": manager.Labels,
			"instance_id": input.InstanceID, "connection_token": connectionToken,
			"created": true,
		})
	}
	return echo.NewHTTPError(http.StatusUnauthorized, "invalid or already used registration token")
}

func (c *SessionPoolController) managerAuthorized(user *entities.User, manager *core.Manager) bool {
	if user == nil || manager == nil {
		return false
	}
	switch manager.Scope {
	case core.ManagerScopeSystem:
		return user.IsAdmin()
	case core.ManagerScopeTeam:
		return user.IsAdmin() || user.IsMemberOfTeam(manager.OwnerID)
	case core.ManagerScopeUser:
		return manager.OwnerID == string(user.ID())
	default:
		return user.IsAdmin()
	}
}

func (c *SessionPoolController) ListOwnedManagers(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	managers, err := c.store.ListManagers(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	result := make([]*core.Manager, 0, len(managers))
	for _, manager := range managers {
		if c.managerAuthorized(user, manager) {
			result = append(result, redactManager(manager))
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"session_managers": result})
}
