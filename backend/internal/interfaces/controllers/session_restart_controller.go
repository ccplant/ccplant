package controllers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

type sessionConfigurationStore interface {
	CreateConfiguration(context.Context, *core.Configuration) error
	GetConfiguration(context.Context, string) (*core.Configuration, error)
	SaveConfiguration(context.Context, *core.Configuration) error
	UpdateProvisionSettings(context.Context, string, []byte) error
}

type sessionConfigurationOwnerStore interface {
	SetConfigurationOwnerReference(context.Context, string, core.OwnerReference) error
}

type sessionConfigurationDeleter interface {
	DeleteConfiguration(context.Context, string) error
}
type restartSettingsResolver interface {
	ResolveRestartSettings(context.Context, string, entities.StartRequest, string, []string) (*sessionsettings.SessionSettings, error)
}

type sessionRuntimeChangeInput struct {
	StartupInput   *entities.StartRequest `json:"startup_input"`
	ReloadSettings *bool                  `json:"reload_settings"`
	BusyPolicy     string                 `json:"busy_policy"`
	ProfileID      *string                `json:"session_profile_id"`
	Revision       *int64                 `json:"revision"`
}

type managerRestartRequest struct {
	route    *repositories.SessionRoute
	action   string
	id       string
	settings *sessionsettings.SessionSettings
	current  *sessionsettings.SessionSettings
}

const workerAuthorizedDeleteContextKey = "worker_authorized_session_delete"

func (c *SessionController) restartConfiguration(ctx echo.Context) (sessionConfigurationStore, *core.Configuration, error) {
	store, ok := c.sessionRunnerStore.(sessionConfigurationStore)
	if !ok {
		return nil, nil, echo.NewHTTPError(501, "settings reload is unavailable")
	}
	cfg, err := store.GetConfiguration(ctx.Request().Context(), ctx.Param("sessionId"))
	if err != nil {
		return nil, nil, echo.NewHTTPError(409, "session has no saved startup input; provide startup_input explicitly")
	}
	az := auth.GetAuthorizationContext(ctx)
	if az == nil || !az.CanAccessResource(cfg.UserID, cfg.Scope, cfg.TeamID) {
		return nil, nil, echo.NewHTTPError(403, "session access denied")
	}
	// Do not resolve another person's credentials with the caller's identity.
	if cfg.Scope != "team" && (az.User == nil || string(az.User.ID()) != cfg.UserID) {
		return nil, nil, echo.NewHTTPError(403, "only the session owner can reload personal settings")
	}
	if cfg.Phase != "" && cfg.Phase != "ready" && cfg.Phase != "paused" && cfg.Phase != "failed" && time.Since(cfg.UpdatedAt) > 12*time.Minute {
		cfg.Phase = "failed"
		cfg.Error = "Operation was interrupted. Retry to recover the stopped session."
		if err := store.SaveConfiguration(ctx.Request().Context(), cfg); err != nil {
			return nil, nil, echo.NewHTTPError(409, "operation state changed")
		}
	}
	return store, cfg, nil
}
func configurationStatus(ctx echo.Context, cfg *core.Configuration) error {
	return ctx.JSON(http.StatusOK, map[string]interface{}{"session_id": cfg.SessionID, "phase": cfg.Phase, "revision": cfg.Revision, "request_id": cfg.RequestID, "error": cfg.Error})
}
func (c *SessionController) RestartStatus(ctx echo.Context) error {
	_, cfg, err := c.restartConfiguration(ctx)
	if err != nil {
		return err
	}
	return configurationStatus(ctx, cfg)
}

// RestartSession sends a full, newly resolved settings payload to the execution plane.
func (c *SessionController) RestartSession(ctx echo.Context) error {
	return c.changeSessionRuntime(ctx, false)
}
func (c *SessionController) PauseSession(ctx echo.Context) error {
	return c.changeSessionRuntime(ctx, true)
}
func (c *SessionController) changeSessionRuntime(ctx echo.Context, pause bool) error {
	var input sessionRuntimeChangeInput
	if ctx.Request().ContentLength != 0 {
		if err := ctx.Bind(&input); err != nil {
			return echo.NewHTTPError(400, "invalid restart request")
		}
	}
	store, cfg, err := c.restartConfiguration(ctx)
	if err != nil && input.StartupInput != nil {
		if e, ok := err.(*echo.HTTPError); ok && e.Code == 409 {
			if err := c.saveLegacyStartupInput(ctx, input.StartupInput); err != nil {
				return err
			}
			store, cfg, err = c.restartConfiguration(ctx)
		}
	}
	if err != nil {
		return err
	}
	if input.StartupInput != nil {
		start := *input.StartupInput
		start.Scope = entities.ResourceScope(cfg.Scope)
		start.TeamID = cfg.TeamID
		raw, err := json.Marshal(start)
		if err != nil {
			return echo.NewHTTPError(400, "invalid startup input")
		}
		cfg.Input = raw
		if input.ProfileID == nil {
			profile := start.SessionProfileID
			input.ProfileID = &profile
		}
	}
	if input.BusyPolicy != "" && input.BusyPolicy != "wait" && input.BusyPolicy != "interrupt" {
		return echo.NewHTTPError(400, "invalid busy_policy")
	}
	if input.Revision != nil && *input.Revision != cfg.Revision {
		return echo.NewHTTPError(412, "settings revision changed")
	}
	requestID := ctx.Request().Header.Get("Idempotency-Key")
	if requestID == "" {
		requestID = uuid.NewString()
	}
	requestBody, err := json.Marshal(struct {
		Pause bool
		Input interface{}
	}{pause, input})
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid restart request").SetInternal(err)
	}
	requestHash := fmt.Sprintf("%x", sha256.Sum256(requestBody))
	if cfg.RequestID == requestID {
		if cfg.RequestHash != requestHash {
			return echo.NewHTTPError(409, "idempotency key was used for another request")
		}
		return configurationStatus(ctx, cfg)
	}
	if cfg.Phase != "" && cfg.Phase != "ready" && cfg.Phase != "paused" && cfg.Phase != "failed" {
		return echo.NewHTTPError(409, "session operation is in progress")
	}
	old, route, err := c.restartCurrentSettings(ctx, cfg.SessionID)
	if err != nil {
		return err
	}
	if old.Session.AgentType != "claude-acp" && old.Session.AgentType != "codex-acp" {
		return echo.NewHTTPError(422, "conversation resume supports only Claude ACP and Codex ACP")
	}
	encodedOld, err := json.Marshal(old)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to copy current settings").SetInternal(err)
	}
	var copied sessionsettings.SessionSettings
	if err := json.Unmarshal(encodedOld, &copied); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to copy current settings").SetInternal(err)
	}
	next := &copied
	var refresh func(context.Context) (*sessionsettings.SessionSettings, error)
	if !pause && (input.ReloadSettings == nil || *input.ReloadSettings) {
		az := auth.GetAuthorizationContext(ctx)
		profileID := cfg.ProfileID
		if input.ProfileID != nil {
			profileID = *input.ProfileID
		}
		refresh = func(ctx context.Context) (*sessionsettings.SessionSettings, error) {
			return c.reloadSessionSettings(ctx, cfg, profileID, az)
		}
		next, err = refresh(ctx.Request().Context())
		if err != nil {
			return err
		}
		cfg.ProfileID = profileID
	}

	if err := sessionsettings.ValidateRestart(old, next); err != nil {
		return echo.NewHTTPError(422, err.Error())
	}
	next.Session.ID = old.Session.ID
	next.Session.PersistenceEnabled = old.Session.PersistenceEnabled
	next.ParentRuntime = old.ParentRuntime
	next.Session.ResumeFrom = next.Session.ID
	next.Paused = false
	next.Restart = true
	next.InitialMessage = ""
	next.WebhookPayload = ""
	sessionsettings.PrepareRestart(old, next)
	raw, err := json.Marshal(next)
	if err != nil {
		return echo.NewHTTPError(500, "failed to encode settings")
	}
	if route != nil {
		request := managerRestartRequest{route: route, action: "restart/validate", id: requestID, settings: next, current: old}
		if err := c.sendRestartToManager(ctx.Request().Context(), request); err != nil {
			var httpErr *echo.HTTPError
			if errors.As(err, &httpErr) {
				return httpErr
			}
			return echo.NewHTTPError(503, "manager control connection unavailable; check that the manager is online").SetInternal(err)
		}
	} else {
		manager, ok := c.getSessionManager().(repositories.SessionRestarter)
		if !ok {
			return echo.NewHTTPError(501, "restart unavailable")
		}
		if err := manager.ValidateSessionRestart(ctx.Request().Context(), cfg.SessionID, next); err != nil {
			return echo.NewHTTPError(422, err.Error())
		}
	}
	previousPhase := cfg.Phase
	cfg.RequestHash = requestHash
	cfg.RequestID = requestID
	cfg.Error = ""
	cfg.Phase = "waiting_idle"
	cfg.Revision++
	if err := store.SaveConfiguration(ctx.Request().Context(), cfg); err != nil {
		return echo.NewHTTPError(409, "session operation conflict")
	}
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx.Request().Context()), 10*time.Minute)
	operation := sessionRestartOperation{controller: c, store: store, config: cfg, old: old, next: next, route: route, input: input, pause: pause, requestID: requestID, raw: raw, refresh: refresh, previousPhase: previousPhase}
	go operation.run(workCtx, cancel)
	return ctx.JSON(202, map[string]interface{}{"session_id": cfg.SessionID, "request_id": requestID, "status": "resuming"})
}
func (c *SessionController) restartCurrentSettings(ctx echo.Context, id string) (*sessionsettings.SessionSettings, *repositories.SessionRoute, error) {
	if session := c.getSessionManager().GetSession(id); session != nil {
		if manager, ok := c.getSessionManager().(repositories.SessionRestarter); ok {
			settings, err := manager.CurrentSessionSettings(ctx.Request().Context(), id)
			if err == nil {
				return settings, nil, nil
			}
		}
		if provider, ok := session.(interface {
			ProvisionSettings() *sessionsettings.SessionSettings
		}); ok && provider.ProvisionSettings() != nil {
			return provider.ProvisionSettings(), nil, nil
		}
	}
	if c.sessionRouteRepo == nil {
		return nil, nil, echo.NewHTTPError(404, "session not found")
	}
	route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), id)
	if err != nil || route == nil {
		return nil, nil, echo.NewHTTPError(404, "session not found")
	}
	raw, resumeErr := c.remoteResumeSettings(ctx, route)
	if resumeErr != nil {
		return nil, nil, resumeErr
	}
	var settings sessionsettings.SessionSettings
	decodeErr := json.Unmarshal(raw, &settings)
	if len(raw) == 0 || decodeErr != nil {
		return nil, nil, echo.NewHTTPError(409, "session settings unavailable")
	}
	return &settings, route, nil
}
func (c *SessionController) sendRestartToManager(ctx context.Context, request managerRestartRequest) error {
	if c.esmControlTunnel == nil {
		return fmt.Errorf("manager unavailable")
	}
	var payload interface{} = request.settings
	if request.action == "restart/validate" && request.current != nil {
		payload = sessionsettings.RestartValidationRequest{SessionSettings: request.settings, CurrentSettings: request.current}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://esm.local/api/v1/sessions/"+url.PathEscape(request.route.RemoteSessionID)+"/"+request.action, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", request.id)
	resp, err := c.esmControlTunnel.Do(ctx, request.route.ManagerID, request.route.SessionID, request.route.RemoteSessionID, req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return restartManagerResponseError(resp)
	}
	return nil
}
