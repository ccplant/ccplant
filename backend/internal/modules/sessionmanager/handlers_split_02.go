package sessionmanager

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func (h *Handlers) hmacMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sig := c.Request().Header.Get("X-Hub-Signature-256")
			if sig == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing X-Hub-Signature-256 header")
			}

			ts := c.Request().Header.Get(hmacutil.TimestampHeader)
			if ts == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing "+hmacutil.TimestampHeader+" header")
			}

			// Validate timestamp to prevent replay attacks.
			if err := hmacutil.ValidateTimestamp(ts); err != nil {
				log.Printf("[SESSION_MANAGER] Timestamp validation failed for %s %s: %v",
					c.Request().Method, c.Request().URL.Path, err)
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired timestamp")
			}

			// Read body and immediately restore it for downstream handlers.
			body, err := io.ReadAll(c.Request().Body)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "failed to read request body")
			}
			c.Request().Body = io.NopCloser(bytes.NewReader(body))

			pathWithQuery := c.Request().URL.RequestURI()
			msg := hmacutil.BuildMessage(c.Request().Method, pathWithQuery, ts, body)
			if !hmacutil.Verify(h.hmacSecret, msg, sig) {
				log.Printf("[SESSION_MANAGER] HMAC verification failed for %s %s",
					c.Request().Method, c.Request().URL.Path)
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid signature")
			}

			return next(c)
		}
	}
}

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

// CreateSessionResponse is returned after successful session creation.
type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
}

// SessionInfo is a lightweight session representation returned by list/get.
type SessionInfo struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// ListSessionsResponse wraps the list of sessions.
type ListSessionsResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// CreateSession handles POST /api/v1/sessions.
//
// Body: sessionsettings.SessionSettings JSON (pre-built by upstream 親プロキシ).
// The settings are used verbatim as the provision payload; no local secrets
// are resolved on External Session Manager.
func (h *Handlers) CreateSession(c echo.Context) error {
	var settings sessionsettings.SessionSettings
	if err := c.Bind(&settings); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if settings.Session.UserID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "session.user_id is required")
	}

	sessionID := uuid.New().String()

	// Reconstruct a minimal RunServerRequest from the settings metadata.
	// Secrets are NOT re-resolved here — they are already embedded in settings.Env.
	req := &entities.RunServerRequest{
		UserID:            settings.Session.UserID,
		Scope:             entities.ResourceScope(settings.Session.Scope),
		TeamID:            settings.Session.TeamID,
		AgentType:         settings.Session.AgentType,
		Teams:             settings.Session.Teams,
		InitialMessage:    settings.InitialMessage,
		ProvisionSettings: &settings,
	}
	if settings.Repository != nil {
		req.RepoInfo = &entities.RepositoryInfo{
			FullName: settings.Repository.FullName,
			CloneDir: settings.Repository.CloneDir,
			Branch:   settings.Repository.Branch,
			PR:       settings.Repository.PR,
		}
	}

	session, err := h.sessionManager.CreateSession(c.Request().Context(), sessionID, req, nil)
	if err != nil {
		log.Printf("[SESSION_MANAGER] Failed to create session %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to create session: %v", err))
	}

	log.Printf("[SESSION_MANAGER] Created session %s for user %s", session.ID(), settings.Session.UserID)
	return c.JSON(http.StatusCreated, CreateSessionResponse{SessionID: session.ID()})
}

// ListSessions handles GET /api/v1/sessions.
//
// Optional query parameters:
//   - user_id  : filter by user ID
//   - scope    : "user" or "team"
//   - team_id  : filter by team ID (e.g., "org/team-slug")
//   - status   : filter by session status (e.g., "running", "stopped")
func (h *Handlers) ListSessions(c echo.Context) error {
	filter := entities.SessionFilter{}

	if userID := c.QueryParam("user_id"); userID != "" {
		filter.UserID = userID
	}
	if scope := c.QueryParam("scope"); scope != "" {
		filter.Scope = entities.ResourceScope(scope)
	}
	if teamID := c.QueryParam("team_id"); teamID != "" {
		filter.TeamID = teamID
	}
	if status := c.QueryParam("status"); status != "" {
		filter.Status = status
	}

	sessions := h.sessionManager.ListSessions(filter)

	infos := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		infos = append(infos, SessionInfo{
			ID:        s.ID(),
			UserID:    s.UserID(),
			Status:    strings.ToLower(string(s.Status())),
			CreatedAt: s.StartedAt(),
		})
	}

	return c.JSON(http.StatusOK, ListSessionsResponse{Sessions: infos})
}

// StartCodexDeviceAuth handles POST /api/v1/codex-device-auth.
//
// Body: codexauth.WorkloadRequest JSON. The manager creates a purpose-built,
// short-lived authentication Pod; the worker reports its challenge and result
// directly back to the parent callback URL carried in the request.
func (h *Handlers) StartCodexDeviceAuth(c echo.Context) error {
	launcher, ok := h.sessionManager.(codexauth.WorkloadLauncher)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "codex device auth workloads are not supported")
	}
	var request codexauth.WorkloadRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}
	if request.AttemptID == "" || request.CallbackURL == "" || request.Token == "" || request.ExpiresAt.IsZero() {
		return echo.NewHTTPError(http.StatusBadRequest, "attempt_id, callback_url, token, and expires_at are required")
	}
	if err := launcher.StartCodexDeviceAuth(c.Request().Context(), request); err != nil {
		log.Printf("[SESSION_MANAGER] Failed to start codex device auth attempt %s: %v", request.AttemptID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to start codex device auth workload: %v", err))
	}
	log.Printf("[SESSION_MANAGER] Started codex device auth attempt %s", request.AttemptID)
	return c.NoContent(http.StatusAccepted)
}

// CancelCodexDeviceAuth handles DELETE /api/v1/codex-device-auth/:attemptId.
func (h *Handlers) CancelCodexDeviceAuth(c echo.Context) error {
	launcher, ok := h.sessionManager.(codexauth.WorkloadLauncher)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "codex device auth workloads are not supported")
	}
	attemptID := c.Param("attemptId")
	if attemptID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "attemptId is required")
	}
	if err := launcher.CancelCodexDeviceAuth(c.Request().Context(), attemptID); err != nil {
		log.Printf("[SESSION_MANAGER] Failed to cancel codex device auth attempt %s: %v", attemptID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to cancel codex device auth workload: %v", err))
	}
	return c.NoContent(http.StatusNoContent)
}

// GetSession handles GET /api/v1/sessions/:sessionId.
func (h *Handlers) GetSession(c echo.Context) error {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "sessionId is required")
	}

	session := h.sessionManager.GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}

	return c.JSON(http.StatusOK, SessionInfo{
		ID:        session.ID(),
		UserID:    session.UserID(),
		Status:    strings.ToLower(string(session.Status())),
		CreatedAt: session.StartedAt(),
	})
}

// DeleteSession handles DELETE /api/v1/sessions/:sessionId.
func (h *Handlers) DeleteSession(c echo.Context) error {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "sessionId is required")
	}

	if err := h.sessionManager.DeleteSession(sessionID); err != nil {
		// Surface "not found" as 404 so callers can distinguish it from real errors.
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("session not found: %s", sessionID))
		}
		log.Printf("[SESSION_MANAGER] Failed to delete session %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to delete session: %v", err))
	}

	log.Printf("[SESSION_MANAGER] Deleted session %s", sessionID)
	return c.NoContent(http.StatusNoContent)
}

func (h *Handlers) RestartSession(c echo.Context) error {
	manager, ok := h.sessionManager.(repositories.SessionRestarter)
	if !ok {
		return echo.NewHTTPError(501, "session restart is not supported")
	}
	var settings sessionsettings.SessionSettings
	if err := c.Bind(&settings); err != nil {
		return echo.NewHTTPError(400, "invalid restart settings")
	}
	id := c.Request().Header.Get("Idempotency-Key")
	if id == "" {
		return echo.NewHTTPError(400, "Idempotency-Key is required")
	}
	if err := manager.RestartSession(c.Request().Context(), c.Param("sessionId"), id, &settings); err != nil {
		return echo.NewHTTPError(503, "session restart failed").SetInternal(err)
	}
	return c.NoContent(204)
}
func (h *Handlers) PauseSession(c echo.Context) error {
	manager, ok := h.sessionManager.(repositories.SessionRestarter)
	if !ok {
		return echo.NewHTTPError(501, "session pause is not supported")
	}
	// Parent-owned allocations do not have settings in the manager until an
	// explicit lifecycle operation. Persist the running snapshot before pausing.
	if c.Request().ContentLength != 0 {
		var current sessionsettings.SessionSettings
		if err := c.Bind(&current); err != nil {
			return echo.NewHTTPError(400, "invalid current settings")
		}
		if pauser, ok := h.sessionManager.(parentPauser); ok {
			if err := pauser.PauseSessionWithSettings(c.Request().Context(), c.Param("sessionId"), &current); err != nil {
				return echo.NewHTTPError(503, "session pause failed").SetInternal(err)
			}
			return c.NoContent(204)
		}
		preparer, ok := h.sessionManager.(repositories.SessionResumePreparer)
		if !ok {
			return echo.NewHTTPError(501, "session resume preparation unavailable")
		}
		if err := preparer.PrepareSessionResume(c.Request().Context(), c.Param("sessionId"), &current); err != nil {
			return echo.NewHTTPError(503, "failed to preserve current settings").SetInternal(err)
		}
	}
	if err := manager.PauseSession(c.Request().Context(), c.Param("sessionId")); err != nil {
		return echo.NewHTTPError(503, "session pause failed").SetInternal(err)
	}
	return c.NoContent(204)
}

func (h *Handlers) ValidateRestart(c echo.Context) error {
	m, ok := h.sessionManager.(repositories.SessionRestarter)
	if !ok {
		return echo.NewHTTPError(501, "restart unavailable")
	}
	input := sessionsettings.RestartValidationRequest{SessionSettings: &sessionsettings.SessionSettings{}}
	if err := c.Bind(&input); err != nil {
		return echo.NewHTTPError(400, "invalid settings")
	}
	var err error
	if input.CurrentSettings != nil {
		validator, ok := h.sessionManager.(interface {
			ValidateSessionRestartWithCurrent(context.Context, string, *sessionsettings.SessionSettings, *sessionsettings.SessionSettings) error
		})
		if !ok {
			return echo.NewHTTPError(501, "pooled session restart validation unavailable")
		}
		err = validator.ValidateSessionRestartWithCurrent(c.Request().Context(), c.Param("sessionId"), input.CurrentSettings, input.SessionSettings)
	} else {
		err = m.ValidateSessionRestart(c.Request().Context(), c.Param("sessionId"), input.SessionSettings)
	}
	if err != nil {
		return echo.NewHTTPError(422, err.Error())
	}
	return c.NoContent(204)
}
func (h *Handlers) StopSessionAgent(c echo.Context) error {
	if err := h.sessionManager.StopAgent(c.Request().Context(), c.Param("sessionId")); err != nil {
		return echo.NewHTTPError(503, "failed to stop agent")
	}
	return c.NoContent(204)
}
