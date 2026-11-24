package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (c *SessionController) resumeAndRetryRemoteRequest(ctx echo.Context, route *repositories.SessionRoute, send func() (*http.Response, error)) (*http.Response, error) {
	resumeResp, err := c.requestRemoteResume(ctx, route)
	if err != nil {
		return nil, err
	}
	closeutils.SafeCloseResponseBody(resumeResp.Body)
	if resumeResp.StatusCode < 200 || resumeResp.StatusCode >= 300 {
		return nil, echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to resume external session workload")
	}
	resp, err := send()
	if err != nil {
		return nil, err
	}
	if remoteWorkloadSuspended(resp) {
		closeutils.SafeCloseResponseBody(resp.Body)
		return nil, nil
	}
	return resp, nil
}

func remoteWorkloadSuspended(resp *http.Response) bool {
	if resp.StatusCode != http.StatusConflict {
		return false
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(data), resp.Body), resp.Body}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	return json.Unmarshal(data, &payload) == nil && payload.Error.Code == "session_suspended"
}

func sessionResumingResponse(ctx echo.Context, sessionID string) error {
	ctx.Response().Header().Set("Retry-After", "2")
	return ctx.JSON(http.StatusServiceUnavailable, map[string]interface{}{
		"error": map[string]string{"code": "session_resuming", "message": "Session workload is resuming", "session_id": sessionID, "status": "resuming"},
	})
}

func (c *SessionController) resumeRemoteSession(ctx echo.Context, route *repositories.SessionRoute) error {
	resp, err := c.requestRemoteResume(ctx, route)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	for key, values := range resp.Header {
		for _, value := range values {
			ctx.Response().Header().Add(key, value)
		}
	}
	return ctx.Stream(resp.StatusCode, resp.Header.Get("Content-Type"), resp.Body)
}

func (c *SessionController) requestRemoteResume(ctx echo.Context, route *repositories.SessionRoute) (*http.Response, error) {
	if route.RemoteSessionID == "" {
		return nil, echo.NewHTTPError(http.StatusServiceUnavailable, "External session manager has not reported a session yet")
	}
	if c.esmControlTunnel == nil {
		return nil, echo.NewHTTPError(http.StatusServiceUnavailable, "External session manager outbound control connection is unavailable")
	}
	if route.Transport != repositories.SessionRouteTransportDirectRuntime && !c.esmControlTunnel.IsConnected(ctx.Request().Context(), route.ManagerID) {
		return nil, echo.NewHTTPError(http.StatusServiceUnavailable, "External session manager outbound control connection is unavailable")
	}
	targetURL := "http://esm.local/api/v1/sessions/" + url.PathEscape(route.RemoteSessionID) + "/resume"
	body, settingsErr := c.remoteResumeSettings(ctx, route)
	if settingsErr != nil {
		return nil, settingsErr
	}
	req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "Failed to build resume request")
	}
	if len(body) != 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	ts := hmacutil.NowTimestamp()
	parsedTarget, err := url.Parse(targetURL)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "Invalid external session manager URL")
	}
	msg := hmacutil.BuildMessage(req.Method, parsedTarget.RequestURI(), ts, body)
	req.Header.Set("X-Hub-Signature-256", hmacutil.Sign([]byte(route.HMACSecret), msg))
	req.Header.Set(hmacutil.TimestampHeader, ts)
	if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx != nil && authzCtx.PersonalScope.UserID != "" {
		req.Header.Set("X-Forwarded-User", authzCtx.PersonalScope.UserID)
	}
	if route.TeamID != "" {
		req.Header.Set("X-Forwarded-Team", route.TeamID)
	}
	previousStatus := route.Status
	if err := c.recordRemoteLifecycleStatus(ctx.Request().Context(), route, "resuming"); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "Failed to persist resuming session status")
	}
	rollback := func() {
		if err := c.recordRemoteLifecycleStatus(context.Background(), route, previousStatus); err != nil {
			log.Printf("[ROUTE] Failed to roll back resume status for %s: %v", route.SessionID, err)
		}
	}
	resp, err := c.esmControlTunnel.Do(ctx.Request().Context(), route.ManagerID, route.SessionID, route.RemoteSessionID, req)
	if err != nil {
		rollback()
		return nil, echo.NewHTTPError(http.StatusBadGateway, "Failed to reach external session manager")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rollback()
	}
	return resp, nil
}

// remoteResumeSettings refreshes mutable policy values before an existing pool
// allocation is restored. This prevents a session created under an older idle
// timeout from reverting to that timeout after every resume.
func (c *SessionController) remoteResumeSettings(ctx echo.Context, route *repositories.SessionRoute) ([]byte, error) {
	if c.sessionRunnerStore == nil {
		return nil, nil
	}
	allocation, err := c.sessionRunnerStore.GetAllocation(ctx.Request().Context(), route.SessionID)
	if err != nil || allocation == nil || len(allocation.ProvisionSettings) == 0 {
		return nil, nil
	}
	var settings sessionsettings.SessionSettings
	if err := json.Unmarshal(allocation.ProvisionSettings, &settings); err != nil {
		return nil, nil
	}
	persistedParentRuntime := settings.ParentRuntime
	persistedAutoSuspendEnabled := settings.Session.AutoSuspendEnabled
	persistedAutoSuspendMinutes := settings.Session.AutoSuspendMinutes
	// Re-resolve the original startup input on every resume so credentials stay
	// out of durable allocation data while short-lived tokens and profile secrets
	// are refreshed before the replacement workload starts.
	if store, ok := c.sessionRunnerStore.(sessionConfigurationStore); ok {
		if cfg, cfgErr := store.GetConfiguration(ctx.Request().Context(), route.SessionID); cfgErr == nil && cfg != nil {
			az := auth.GetAuthorizationContext(ctx)
			if az == nil {
				return nil, echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
			}
			// A GitHub OAuth token is deliberately omitted from the saved startup
			// input. Re-attach the caller's authenticated token only to an in-memory
			// copy used for this resume, so public/private repository setup can be
			// rebuilt without persisting the credential.
			resumeCfg := *cfg
			if token, ok := auth.GetGitHubTokenFromContext(ctx); ok {
				var start entities.StartRequest
				if json.Unmarshal(resumeCfg.Input, &start) == nil {
					if start.Params == nil {
						start.Params = &entities.SessionParams{}
					}
					start.Params.GithubToken = token
					if input, marshalErr := json.Marshal(start); marshalErr == nil {
						resumeCfg.Input = input
					}
				}
			}
			resolved, resolveErr := c.reloadSessionSettings(ctx.Request().Context(), &resumeCfg, resumeCfg.ProfileID, az)
			if resolveErr != nil {
				return nil, resolveErr
			}
			settings = *resolved
			// Auto-suspend is resolved separately from the startup settings. Keep
			// the allocation policy unless the scoped settings repository below
			// provides a newer explicit value. Otherwise a credential refresh can
			// silently replace an enabled policy with the resolver's zero value.
			settings.Session.AutoSuspendEnabled = persistedAutoSuspendEnabled
			settings.Session.AutoSuspendMinutes = persistedAutoSuspendMinutes
		}
	}
	if c.settingsRepo != nil {
		settingsName := route.UserID
		if route.Scope == string(entities.ScopeTeam) && route.TeamID != "" {
			settingsName = route.TeamID
		}
		if stored, findErr := c.settingsRepo.FindByName(ctx.Request().Context(), settingsName); findErr == nil && stored != nil && stored.AutoSuspend() != nil {
			policy := stored.AutoSuspend()
			settings.Session.AutoSuspendEnabled = &policy.Enabled
			settings.Session.AutoSuspendMinutes = policy.IdleTimeoutMinutes
		}
	}
	settings.ParentRuntime = refreshedParentRuntime(ctx, persistedParentRuntime, route, allocation)
	body, err := json.Marshal(&settings)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "Failed to encode refreshed resume settings")
	}
	return body, nil
}

// refreshedParentRuntime keeps the endpoint recorded by the runner when it
// claimed the allocation. Internal triggers reach /start through loopback or a
// cluster Service, so their request headers do not describe an endpoint that
// the resumed workload can necessarily reach.
func refreshedParentRuntime(ctx echo.Context, persisted *sessionsettings.ParentRuntimeConfig, route *repositories.SessionRoute, allocation *sessionrunnercore.Allocation) *sessionsettings.ParentRuntimeConfig {
	endpoint := ""
	if persisted != nil {
		endpoint = strings.TrimSpace(persisted.Endpoint)
	}
	// Retain the request-derived endpoint for allocations created before the
	// runner started persisting its parent runtime settings.
	if endpoint == "" {
		scheme := strings.TrimSpace(ctx.Request().Header.Get("X-Forwarded-Proto"))
		if scheme == "" {
			scheme = "https"
		}
		host := strings.TrimSpace(ctx.Request().Header.Get("X-Forwarded-Host"))
		if host == "" {
			host = ctx.Request().Host
		}
		prefix := strings.TrimSuffix(strings.TrimSpace(ctx.Request().Header.Get("X-Forwarded-Prefix")), "/")
		endpoint = scheme + "://" + host + prefix
	}
	return &sessionsettings.ParentRuntimeConfig{
		Enabled: true, Endpoint: endpoint, SessionID: route.SessionID,
		ManagerID: route.ManagerID, Token: allocation.RuntimeToken, Generation: allocation.Generation,
	}
}

// deleteRemoteSession deletes a session on External Session Manager via the session manager API.
func (c *SessionController) deleteRemoteSession(ctx echo.Context, route *repositories.SessionRoute) error {
	sessionID := ctx.Param("sessionId")
	// Routed sessions never reach the managed-session ownership check in
	// DeleteSession, and the manager does not re-authorize the forwarded user.
	// Enforce ownership here so every remote deletion path, including the local
	// finalization below, is authorized. Worker-authorized deletes (TTL and route
	// reconciliation) may delete regardless of owner, like managed sessions.
	if workerAuthorized, _ := ctx.Get(workerAuthorizedDeleteContextKey).(bool); !workerAuthorized {
		authzCtx := auth.GetAuthorizationContext(ctx)
		if authzCtx == nil || !authzCtx.CanModifyResource(route.UserID, route.Scope, route.TeamID) {
			return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this session")
		}
	}
	usageDimensions := c.resolveSessionUsageDimensions(ctx.Request().Context(), route.SessionID, "remote_session")
	if route.ManagerID == "" || route.RemoteSessionID == "" {
		c.recordSessionStatusUsage(ctx.Request().Context(), repositories.SessionStatusEvent{SessionID: route.SessionID, Status: "terminating", Timestamp: time.Now()})
		routeDeleted := c.sessionRouteRepo == nil
		if c.sessionRouteRepo != nil {
			if err := c.sessionRouteRepo.Delete(ctx.Request().Context(), sessionID); err != nil {
				log.Printf("[REMOTE_DELETE] Warning: failed to delete pending route entry for session %s: %v", sessionID, err)
			} else {
				routeDeleted = true
			}
		}
		if routeDeleted {
			c.recordSessionTerminated(ctx.Request().Context(), usageDimensions, "pending_remote_route", time.Now())
		}
		c.cleanupRemoteProvisionRequest(ctx.Request().Context(), sessionID)
		c.cleanupSessionConfiguration(ctx.Request().Context(), sessionID)
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"message":    "Pending external session removed",
			"session_id": sessionID,
			"status":     "terminated",
		})
	}

	useTunnel := c.esmControlTunnel != nil && c.esmControlTunnel.IsConnected(ctx.Request().Context(), route.ManagerID)
	if !useTunnel {
		// The parent's routes are authoritative. On every heartbeat a manager
		// receives orphaned_session_ids for workloads that no longer have a route
		// and removes them through its own idempotent delete path. Finalizing the
		// route locally is therefore safe even while the manager is unreachable:
		// a manager that comes back reaps the workload, and one that is gone for
		// good (for example a deleted runner pool) can no longer leave the route
		// stranded as an undeletable stale session. This is also what the former
		// force=true escape hatch did, so requests that still pass it behave the same.
		log.Printf("[REMOTE_DELETE] Manager %s is unreachable; finalizing session %s locally (remote ID: %s)",
			route.ManagerID, sessionID, route.RemoteSessionID)
		if err := c.finalizeRouteLocally(ctx.Request().Context(), route, usageDimensions, "manager_unreachable"); err != nil {
			log.Printf("[REMOTE_DELETE] Failed to finalize session %s locally: %v", sessionID, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete session")
		}
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"message":    "Session terminated successfully",
			"session_id": sessionID,
			"status":     "terminated",
		})
	}
	// The manager owns the allocated runner workload. The public session ID is
	// an API alias; deleting that alias on the manager would leave the Pod alive.
	targetURL := "http://esm.local/api/v1/sessions/" + route.RemoteSessionID

	req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodDelete, targetURL, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to build delete request")
	}

	// Compute HMAC signature over METHOD\nPATH?QUERY\nTIMESTAMP\n(empty body)
	ts := hmacutil.NowTimestamp()
	parsedTarget, _ := url.Parse(targetURL)
	msg := hmacutil.BuildMessage(http.MethodDelete, parsedTarget.RequestURI(), ts, nil)
	sig := hmacutil.Sign([]byte(route.HMACSecret), msg)
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set(hmacutil.TimestampHeader, ts)
	if route.UserID != "" {
		req.Header.Set("X-Forwarded-User", route.UserID)
	}
	if route.TeamID != "" {
		req.Header.Set("X-Forwarded-Team", route.TeamID)
	}
	if route.Transport == repositories.SessionRouteTransportDirectRuntime {
		enqueuer, ok := c.esmControlTunnel.(esmControlEnqueuer)
		if !ok {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "External session manager lifecycle queue is unavailable")
		}
		if route.DeletionRequestID != "" {
			if c.reconcileQueuedDeletion(ctx.Request().Context(), route) {
				return ctx.JSON(http.StatusOK, map[string]interface{}{
					"message": "Session terminated successfully", "session_id": sessionID, "status": "terminated",
				})
			}
			return ctx.JSON(http.StatusAccepted, map[string]interface{}{
				"message": "Session deletion is still pending", "session_id": sessionID, "status": "terminating",
			})
		}
		requestID, err := enqueuer.Enqueue(ctx.Request().Context(), route.ManagerID, route.SessionID, route.RemoteSessionID, req)
		if err != nil {
			log.Printf("[REMOTE_DELETE] Failed to enqueue direct-runtime deletion %s: %v", sessionID, err)
			return echo.NewHTTPError(http.StatusBadGateway, "Failed to queue external session deletion")
		}
		route.DeletionRequestID = requestID
		route.Status = "terminating"
		route.StatusUpdatedAt = time.Now()
		if c.sessionRouteRepo == nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "Session route repository is unavailable")
		}
		if err := c.sessionRouteRepo.Save(ctx.Request().Context(), route); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to persist session deletion state")
		}
		c.recordSessionStatusUsage(ctx.Request().Context(), repositories.SessionStatusEvent{SessionID: route.SessionID, Status: route.Status, Timestamp: route.StatusUpdatedAt})
		return ctx.JSON(http.StatusAccepted, map[string]interface{}{
			"message": "Session deletion queued", "session_id": sessionID, "status": "terminating",
		})
	}

	resp, err := c.esmControlTunnel.Do(ctx.Request().Context(), route.ManagerID, route.SessionID, route.RemoteSessionID, req)
	if err != nil {
		log.Printf("[REMOTE_DELETE] Failed to delete remote session %s over outbound control: %v", route.RemoteSessionID, err)
		return echo.NewHTTPError(http.StatusBadGateway, "Failed to reach external session manager")
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		// success
	case http.StatusNotFound:
		// Session already gone on External Session Manager — treat as success so we can
		// still clean up the local route entry.
		log.Printf("[REMOTE_DELETE] Remote session %s not found on External Session Manager (already deleted), cleaning up local route", route.RemoteSessionID)
	default:
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[REMOTE_DELETE] External Session Manager returned status %d: %s", resp.StatusCode, string(respBody))
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete remote session")
	}

	// Clean up local route entry regardless of whether External Session Manager had the session.
	c.recordSessionStatusUsage(ctx.Request().Context(), repositories.SessionStatusEvent{SessionID: route.SessionID, Status: "terminating", Timestamp: time.Now()})
	if c.sessionRouteRepo != nil {
		if err := c.sessionRouteRepo.Delete(ctx.Request().Context(), sessionID); err != nil {
			log.Printf("[REMOTE_DELETE] Warning: failed to delete route entry for session %s: %v", sessionID, err)
		}
	}
	c.recordSessionTerminated(ctx.Request().Context(), usageDimensions, "esm_sync", time.Now())
	c.cleanupRemoteProvisionRequest(ctx.Request().Context(), sessionID)
	c.cleanupSessionConfiguration(ctx.Request().Context(), sessionID)

	log.Printf("[REMOTE_DELETE] Deleted remote session %s (remote ID: %s) over outbound control", sessionID, route.RemoteSessionID)
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Session terminated successfully",
		"session_id": sessionID,
		"status":     "terminated",
	})
}

// reconcileQueuedDeletion removes a route only after the manager has durably
// reported completion. It is intentionally called from both DELETE and search,
// making progress after API restarts without relying on in-memory goroutines.
