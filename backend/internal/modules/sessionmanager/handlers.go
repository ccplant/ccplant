// Package sessionmanager implements the session manager forwarding endpoint.
//
// This package enables "small-cluster mode": a External Session Manager instance that accepts
// pre-built SessionSettings from a trusted upstream 親プロキシ and creates sessions
// without needing any local secrets (agentapi-settings-*, GitHub secrets, etc.).
//
// All requests to /api/v1/sessions must carry an HMAC-SHA256 signature in the
// X-Hub-Signature-256 header and the Unix timestamp in the X-Timestamp header.
// The signature covers "METHOD\nPATH?QUERY\nTIMESTAMP\nBODY", preventing both
// request forgery and replay attacks.
// The shared secret is configured via SESSION_MANAGER_HMAC_SECRET (or config file).
package sessionmanager

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// Handlers implements the custom route handler for the session manager forwarding endpoint.
type Handlers struct {
	sessionManager repositories.SessionManager
	stateStore     services.SessionStateStore
	hmacSecret     []byte
}

type parentResumer interface {
	ResumeParentSession(context.Context, string, *sessionsettings.SessionSettings) (entities.Session, bool, error)
}

type parentSuspender interface {
	SuspendSessionWithSettings(context.Context, string, *sessionsettings.SessionSettings) error
}

type parentPauser interface {
	PauseSessionWithSettings(context.Context, string, *sessionsettings.SessionSettings) error
}

type workloadSuspensionReporter interface {
	SessionWorkloadSuspended(context.Context, string) (bool, error)
}

type operationalProvider interface {
	OperationalStatus(context.Context, []string) (map[string]interface{}, error)
	OperationalLogs(context.Context, string, string, int) ([]string, string, error)
}

// NewHandlers creates a new Handlers instance.
// hmacSecret must be non-empty; if it is empty, RegisterRoutes will refuse to register.
type HandlerOption func(*Handlers)

func WithSessionStateStore(store services.SessionStateStore) HandlerOption {
	return func(h *Handlers) { h.stateStore = store }
}

func NewHandlers(sessionManager repositories.SessionManager, hmacSecret string, options ...HandlerOption) *Handlers {
	h := &Handlers{
		sessionManager: sessionManager,
		hmacSecret:     []byte(hmacSecret),
	}
	for _, option := range options {
		option(h)
	}
	return h
}

// GetName returns the handler name for logging.
func (h *Handlers) GetName() string {
	return "SessionManagerHandlers"
}

// RegisterRoutes registers /api/v1/sessions routes with HMAC middleware.
// If hmacSecret is empty, registration is skipped with a warning.
func (h *Handlers) RegisterRoutes(e *echo.Echo) error {
	if len(h.hmacSecret) == 0 {
		log.Printf("[SESSION_MANAGER] Warning: HMAC secret is empty, skipping route registration")
		return nil
	}

	g := e.Group("/api/v1/sessions")
	g.Use(h.hmacMiddleware())

	g.POST("", h.CreateSession)
	g.GET("", h.ListSessions)
	g.GET("/:sessionId", h.GetSession)
	g.POST("/:sessionId/resume", h.ResumeSession)
	g.POST("/:sessionId/suspend", h.SuspendSession)
	g.POST("/:sessionId/context-snapshots/:snapshotId", h.CreateContextSnapshot)
	g.DELETE("/:sessionId/context-snapshots/:snapshotId", h.DeleteContextSnapshot)
	g.POST("/:sessionId/restart", h.RestartSession)
	g.POST("/:sessionId/restart/validate", h.ValidateRestart)
	g.POST("/:sessionId/stop", h.StopSessionAgent)
	g.POST("/:sessionId/pause", h.PauseSession)
	g.DELETE("/:sessionId", h.DeleteSession)

	// Codex device auth workloads are manager-level operations addressed by the
	// parent through the outbound control tunnel, exactly like session deletion.
	// They use the same HMAC middleware as /api/v1/sessions.
	auth := e.Group("/api/v1/codex-device-auth")
	auth.Use(h.hmacMiddleware())
	auth.POST("", h.StartCodexDeviceAuth)
	auth.DELETE("/:attemptId", h.CancelCodexDeviceAuth)

	if _, ok := h.sessionManager.(operationalProvider); ok {
		management := e.Group("/internal/esm-management")
		management.Use(h.hmacMiddleware())
		management.GET("/status", h.GetOperationalStatus)
		management.GET("/logs", h.GetOperationalLogs)
		management.DELETE("/runners/:runnerId", h.DeleteOperationalRunner)
	}

	// Runtime traffic is addressed by the parent as
	// /<remote-id>/<agent-endpoint>, rather than through the management API
	// prefix. Register the finite AgentAPI/ACP surface explicitly. Echo's final
	// path parameter is greedy, so even /:sessionId/:endpoint would also match
	// /internal/session-state/... and steal provisioner callbacks.
	for _, endpoint := range []string{
		"health", "status", "session", "messages", "message", "rpc", "sse",
		"events", "tool_status", "action", "stop", "herdr/rpc",
	} {
		e.Any("/:sessionId/"+endpoint, h.ProxySession, h.hmacMiddleware())
	}

	log.Printf("[SESSION_MANAGER] Registered routes under /api/v1/sessions")
	return nil
}

func (h *Handlers) CreateContextSnapshot(c echo.Context) error {
	if c.Request().Header.Get("X-CCPlant-Template-Snapshot-Ready") == "1" {
		return c.NoContent(http.StatusNoContent)
	}
	if creator, ok := h.sessionManager.(repositories.SessionContextSnapshotCreator); ok {
		if err := creator.CreateSessionContextSnapshot(c.Request().Context(), c.Param("sessionId"), c.Param("snapshotId")); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
	checkpointer, ok := h.sessionManager.(repositories.SessionCheckpointer)
	if !ok || h.stateStore == nil {
		return echo.NewHTTPError(http.StatusNotImplemented, "session context snapshots are not supported")
	}
	if err := checkpointer.CheckpointSessionState(c.Request().Context(), c.Param("sessionId")); err != nil {
		return err
	}
	snapshot, err := h.stateStore.Load(c.Request().Context(), c.Param("sessionId"))
	if err != nil {
		return err
	}
	defer snapshot.Close()
	if err := h.stateStore.Save(c.Request().Context(), c.Param("snapshotId"), snapshot); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handlers) DeleteContextSnapshot(c echo.Context) error {
	deleter, ok := h.stateStore.(services.SessionStateDeleter)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "session context snapshot deletion is not supported")
	}
	if err := deleter.Delete(c.Request().Context(), c.Param("snapshotId")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// DeleteOperationalRunner removes the complete workload through the session
// manager's normal deletion path. Kubernetes managers therefore delete the
// Service, Deployment/Pod, PVC, provision request, and every session-labelled
// Secret instead of only terminating the Pod.
func (h *Handlers) DeleteOperationalRunner(c echo.Context) error {
	runnerID := strings.TrimSpace(c.Param("runnerId"))
	if runnerID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "runnerId is required")
	}
	if err := h.sessionManager.DeleteSession(runnerID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return c.NoContent(http.StatusNotFound)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete runner").SetInternal(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handlers) GetOperationalStatus(c echo.Context) error {
	provider, ok := h.sessionManager.(operationalProvider)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "operational status is unavailable")
	}
	status, err := provider.OperationalStatus(c.Request().Context(), c.QueryParams()["pool"])
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to inspect session manager").SetInternal(err)
	}
	return c.JSON(http.StatusOK, status)
}

func (h *Handlers) GetOperationalLogs(c echo.Context) error {
	provider, ok := h.sessionManager.(operationalProvider)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "operational logs are unavailable")
	}
	tail, _ := strconv.Atoi(c.QueryParam("tail"))
	if tail < 1 || tail > 5000 {
		tail = 200
	}
	lines, source, err := provider.OperationalLogs(c.Request().Context(), c.QueryParam("runner_id"), c.QueryParam("session_id"), tail)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read logs").SetInternal(err)
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"lines": lines, "source": source})
}

func (h *Handlers) ResumeSession(c echo.Context) error {
	var settings *sessionsettings.SessionSettings
	if c.Request().ContentLength != 0 {
		settings = &sessionsettings.SessionSettings{}
		if err := c.Bind(settings); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid resume settings")
		}
	}
	session, restoring, err := h.resumeWorkload(c.Request().Context(), c.Param("sessionId"), settings)
	if httpErr, ok := err.(*echo.HTTPError); ok {
		return httpErr
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error())
	}
	status := "active"
	code := http.StatusOK
	if restoring {
		status = "resuming"
		code = http.StatusAccepted
		c.Response().Header().Set("Retry-After", "2")
	} else if session != nil {
		status = session.Status()
	}
	return c.JSON(code, map[string]string{"session_id": c.Param("sessionId"), "status": status})
}

func (h *Handlers) resumeWorkload(ctx context.Context, sessionID string, settings *sessionsettings.SessionSettings) (entities.Session, bool, error) {
	if resumer, ok := h.sessionManager.(parentResumer); ok {
		return resumer.ResumeParentSession(ctx, sessionID, settings)
	}
	if settings != nil {
		if preparer, ok := h.sessionManager.(repositories.SessionResumePreparer); ok {
			if err := preparer.PrepareSessionResume(ctx, sessionID, settings); err != nil {
				return nil, false, err
			}
		}
	}
	ensurer, ok := h.sessionManager.(repositories.SessionWorkloadEnsurer)
	if !ok {
		return nil, false, echo.NewHTTPError(http.StatusNotImplemented, "session resume is not supported")
	}
	return ensurer.EnsureSessionWorkload(ctx, sessionID)
}

func (h *Handlers) SuspendSession(c echo.Context) error {
	if c.Request().ContentLength == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "suspend settings are required")
	}
	var settings sessionsettings.SessionSettings
	if err := c.Bind(&settings); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid suspend settings")
	}
	if suspender, ok := h.sessionManager.(parentSuspender); ok {
		if err := suspender.SuspendSessionWithSettings(c.Request().Context(), c.Param("sessionId"), &settings); err != nil {
			log.Printf("[SESSION_MANAGER] suspend failed instance=%s control_instance=%s command_id=%s session=%s error=%v", os.Getenv("HOSTNAME"), c.Request().Header.Get("X-ESM-Control-Instance"), c.Request().Header.Get("X-ESM-Control-Command-ID"), c.Param("sessionId"), err)
			return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error())
		}
		return c.NoContent(http.StatusNoContent)
	}
	preparer, ok := h.sessionManager.(repositories.SessionResumePreparer)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "session resume preparation is not supported")
	}
	if err := preparer.PrepareSessionResume(c.Request().Context(), c.Param("sessionId"), &settings); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error())
	}
	suspender, ok := h.sessionManager.(repositories.SessionSuspender)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "session suspend is not supported")
	}
	if err := suspender.SuspendSession(c.Request().Context(), c.Param("sessionId")); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

// ProxySession forwards an authenticated parent request to the concrete
// session runtime while preserving streaming responses such as SSE.
func (h *Handlers) ProxySession(c echo.Context) error {
	sessionID := c.Param("sessionId")
	session := h.sessionManager.GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	if session.Status() == "suspended" && h.workloadSuspended(c.Request().Context(), sessionID) {
		return suspendedSessionResponse(c, sessionID)
	}
	addr := strings.TrimSpace(session.Addr())
	if addr == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session runtime is not ready")
	}
	target, err := url.Parse(addr)
	if err != nil || target.Host == "" {
		if parsed, parseErr := url.Parse("http://" + addr); parseErr == nil {
			target = parsed
			err = parseErr
		}
	}
	if err != nil || target == nil || target.Host == "" {
		return echo.NewHTTPError(http.StatusInternalServerError, "invalid session runtime address")
	}
	suffix := strings.TrimPrefix(c.Request().URL.Path, "/"+sessionID+"/")
	target = sessionProxyTarget(target, suffix)

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = "/" + strings.TrimPrefix(suffix, "/")
		req.URL.RawPath = ""
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, proxyErr error) {
		if h.workloadSuspended(req.Context(), sessionID) {
			_ = suspendedSessionResponse(c, sessionID)
			return
		}
		log.Printf("[SESSION_MANAGER] Proxy session %s failed: %v", sessionID, proxyErr)
		http.Error(w, "session runtime unavailable", http.StatusBadGateway)
	}
	proxy.ServeHTTP(c.Response(), c.Request())
	return nil
}

func sessionProxyTarget(target *url.URL, suffix string) *url.URL {
	if suffix != "herdr/rpc" {
		return target
	}
	targetCopy := *target
	targetCopy.Host = net.JoinHostPort(target.Hostname(), "9001")
	return &targetCopy
}

func (h *Handlers) workloadSuspended(ctx context.Context, sessionID string) bool {
	reporter, ok := h.sessionManager.(workloadSuspensionReporter)
	if !ok {
		return false
	}
	suspended, err := reporter.SessionWorkloadSuspended(ctx, sessionID)
	if err != nil {
		log.Printf("[SESSION_MANAGER] Inspect workload of session %s failed: %v", sessionID, err)
		return false
	}
	return suspended
}

func suspendedSessionResponse(c echo.Context, sessionID string) error {
	return c.JSON(http.StatusConflict, map[string]interface{}{
		"error": map[string]string{"code": "session_suspended", "message": "Session workload is suspended", "session_id": sessionID, "status": "suspended"},
	})
}

// ---------------------------------------------------------------------------
// HMAC middleware
// ---------------------------------------------------------------------------

// hmacMiddleware verifies X-Hub-Signature-256 and X-Timestamp on every request.
// The signature must cover "METHOD\nPATH?QUERY\nTIMESTAMP\nBODY".
// Requests missing X-Timestamp or carrying an expired timestamp are rejected.
