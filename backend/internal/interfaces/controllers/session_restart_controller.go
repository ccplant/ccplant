package controllers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
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
type restartSettingsResolver interface {
	ResolveRestartSettings(context.Context, string, entities.StartRequest, string, []string) (*sessionsettings.SessionSettings, error)
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
	var input struct {
		StartupInput   *entities.StartRequest `json:"startup_input"`
		ReloadSettings *bool                  `json:"reload_settings"`
		BusyPolicy     string                 `json:"busy_policy"`
		ProfileID      *string                `json:"session_profile_id"`
		Revision       *int64                 `json:"revision"`
	}
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
	requestBody, _ := json.Marshal(struct {
		Pause bool
		Input interface{}
	}{pause, input})
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
	encodedOld, _ := json.Marshal(old)
	var copied sessionsettings.SessionSettings
	_ = json.Unmarshal(encodedOld, &copied)
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
		if err := c.sendRestartToManager(ctx.Request().Context(), route, "restart/validate", requestID, next, old); err != nil {
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
	// HTTP cancellation must not leave a stopped agent without an operation owner.
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx.Request().Context()), 10*time.Minute)
	go func() {
		defer cancel()
		fail := func(e error) {
			log.Printf("[SESSION_RESTART] session=%s request=%s failed (%T)", cfg.SessionID, cfg.RequestID, e)
			cfg.Phase = "failed"
			cfg.Error = "Session operation failed; check runtime status and retry."
			_ = store.SaveConfiguration(context.WithoutCancel(workCtx), cfg)
		}
		if err := c.waitRestartIdle(workCtx, cfg.SessionID, route, input.BusyPolicy); err != nil {
			cfg.Phase = previousPhase
			cfg.Error = "Could not finish the current turn; stop it and retry."
			_ = store.SaveConfiguration(context.WithoutCancel(workCtx), cfg)
			return
		}
		// Stop the old runtime before reading credentials again. This prevents its
		// last OAuth refresh from replacing the credential selected for restart.
		cfg.Phase = "pausing"
		if err := store.SaveConfiguration(workCtx, cfg); err != nil {
			fail(err)
			return
		}
		if route != nil {
			err = c.sendRestartToManager(workCtx, route, "pause", requestID, old)
		} else {
			err = c.getSessionManager().(repositories.SessionRestarter).PauseSession(workCtx, cfg.SessionID)
		}
		if err != nil {
			fail(err)
			return
		}
		if pause {
			cfg.Phase = "paused"
			_ = store.SaveConfiguration(context.WithoutCancel(workCtx), cfg)
			return
		}
		if refresh != nil {
			next, err = refresh(workCtx)
			if err != nil {
				fail(err)
				return
			}
			if err = sessionsettings.ValidateRestart(old, next); err != nil {
				fail(err)
				return
			}
			next.Session.ID = old.Session.ID
			next.Session.PersistenceEnabled = old.Session.PersistenceEnabled
			next.ParentRuntime = old.ParentRuntime
			next.Session.ResumeFrom = next.Session.ID
			next.Restart = true
			next.InitialMessage = ""
			next.WebhookPayload = ""
			sessionsettings.PrepareRestart(old, next)
			raw, err = json.Marshal(next)
			if err != nil {
				fail(err)
				return
			}
		}
		cfg.Phase = "restarting"
		if pause {
			cfg.Phase = "pausing"
		}
		cfg.Settings = raw
		if err := store.SaveConfiguration(workCtx, cfg); err != nil {
			fail(err)
			return
		}
		if route != nil {
			if !pause {
				if err := store.UpdateProvisionSettings(workCtx, cfg.SessionID, raw); err != nil {
					fail(err)
					return
				}
			}
			action := "restart"
			if pause {
				action = "pause"
			}
			if err := c.sendRestartToManager(workCtx, route, action, requestID, next); err != nil {
				fail(err)
				return
			}
		} else {
			manager, ok := c.getSessionManager().(repositories.SessionRestarter)
			if !ok {
				fail(fmt.Errorf("unsupported manager"))
				return
			}
			if pause {
				err = manager.PauseSession(workCtx, cfg.SessionID)
			} else {
				err = manager.RestartSession(workCtx, cfg.SessionID, requestID, next)
			}
			if err != nil {
				fail(err)
				return
			}
		}
		cfg.Phase = "ready"
		if pause {
			cfg.Phase = "paused"
		}
		cfg.Error = ""
		_ = store.SaveConfiguration(context.WithoutCancel(workCtx), cfg)
	}()
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
	raw := c.remoteResumeSettings(ctx, route)
	var settings sessionsettings.SessionSettings
	if len(raw) == 0 || json.Unmarshal(raw, &settings) != nil {
		return nil, nil, echo.NewHTTPError(409, "session settings unavailable")
	}
	return &settings, route, nil
}
func (c *SessionController) sendRestartToManager(ctx context.Context, route *repositories.SessionRoute, action, id string, settings *sessionsettings.SessionSettings, current ...*sessionsettings.SessionSettings) error {
	if c.esmControlTunnel == nil {
		return fmt.Errorf("manager unavailable")
	}
	var payload interface{} = settings
	if action == "restart/validate" && len(current) > 0 {
		payload = sessionsettings.RestartValidationRequest{SessionSettings: settings, CurrentSettings: current[0]}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://esm.local/api/v1/sessions/"+url.PathEscape(route.RemoteSessionID)+"/"+action, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", id)
	resp, err := c.esmControlTunnel.Do(ctx, route.ManagerID, route.SessionID, route.RemoteSessionID, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return restartManagerResponseError(resp)
	}
	return nil
}
func (c *SessionController) waitRestartIdle(ctx context.Context, id string, route *repositories.SessionRoute, policy string) error {
	if policy == "interrupt" && route != nil {
		if err := c.sendRestartToManager(ctx, route, "stop", uuid.NewString(), nil); err != nil {
			return err
		}
	}
	if policy == "interrupt" && route == nil {
		if err := c.getSessionManager().StopAgent(ctx, id); err != nil {
			return err
		}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		status := ""
		if route != nil {
			r, e := c.sessionRouteRepo.Get(ctx, id)
			if e != nil {
				return e
			}
			if r != nil {
				status = r.Status
			}
		} else if s := c.getSessionManager().GetSession(id); s != nil {
			status = s.Status()
		}
		if status != "running" {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Blocks runtime access while settings are changing, including automatic resume.
func (c *SessionController) checkRestartHold(ctx echo.Context) error {
	if authorized, _ := ctx.Get(workerAuthorizedDeleteContextKey).(bool); authorized {
		return nil
	}
	store, ok := c.sessionRunnerStore.(sessionConfigurationStore)
	if !ok {
		return nil
	}
	cfg, err := store.GetConfiguration(ctx.Request().Context(), ctx.Param("sessionId"))
	if errors.Is(err, core.ErrNotFound) {
		return nil
	}
	if err != nil {
		return echo.NewHTTPError(503, "session lifecycle state unavailable")
	}
	az := auth.GetAuthorizationContext(ctx)
	if az == nil || !az.CanAccessResource(cfg.UserID, cfg.Scope, cfg.TeamID) {
		return echo.NewHTTPError(403, "session access denied")
	}
	if ctx.Request().Method == http.MethodDelete && (cfg.Phase == "failed" || cfg.Phase == "paused") {
		return nil
	}
	if cfg.Phase != "" && cfg.Phase != "ready" {
		return echo.NewHTTPError(423, "session_paused: "+cfg.Phase)
	}
	return nil
}

func (c *SessionController) reloadSessionSettings(ctx context.Context, cfg *core.Configuration, profileID string, az *auth.AuthorizationContext) (*sessionsettings.SessionSettings, error) {
	resolver, ok := c.sessionCreator.(restartSettingsResolver)
	if !ok {
		return nil, echo.NewHTTPError(501, "settings reload is unavailable")
	}
	var start entities.StartRequest
	if json.Unmarshal(cfg.Input, &start) != nil {
		return nil, echo.NewHTTPError(409, "invalid saved startup input")
	}
	start.Scope = entities.ResourceScope(cfg.Scope)
	start.TeamID = cfg.TeamID
	start.TriggeredUserID = cfg.TriggeredUserID
	if profileID != "" {
		if c.sessionProfileRepo == nil {
			return nil, echo.NewHTTPError(503, "session profiles unavailable")
		}
		profile, err := c.sessionProfileRepo.Get(ctx, profileID)
		if err != nil || profile == nil {
			return nil, echo.NewHTTPError(422, "session profile unavailable")
		}
		if !az.CanAccessResource(profile.UserID(), string(profile.Scope()), profile.TeamID()) {
			return nil, echo.NewHTTPError(403, "session profile access denied")
		}
		profileCfg, err := sessionuc.ResolveEffectiveSessionProfileConfig(
			ctx,
			c.sessionProfileRepo,
			profile,
			start.Scope,
			cfg.UserID,
			start.TeamID,
		)
		if err != nil {
			return nil, sessionProfileResolutionHTTPError(err)
		}
		applySessionProfile(&start, profile, profileCfg, start.Params != nil && start.Params.Sandbox != nil, start.Params != nil && start.Params.Docker != nil)
	}
	// Never allow a team member to select another person's private credentials.
	if start.Params != nil {
		switch start.Params.CredentialSource {
		case "session_user":
			if az.User == nil || string(az.User.ID()) != cfg.UserID {
				return nil, echo.NewHTTPError(403, "only the owner can reload personal credentials")
			}
		case "triggered_user", "github_sender":
			if cfg.TriggeredUserID != "" && (az.User == nil || string(az.User.ID()) != cfg.TriggeredUserID) {
				return nil, echo.NewHTTPError(403, "only the triggering user can reload their credentials")
			}
		}
	}
	if start.Params != nil && start.Params.ConnectionID != "" {
		if c.githubTokenResolver == nil {
			return nil, echo.NewHTTPError(503, "GitHub credentials unavailable")
		}
		token, err := c.githubTokenResolver.ResolveAccessToken(ctx, az.User, start.Params.ConnectionID)
		if err != nil {
			return nil, echo.NewHTTPError(422, "GitHub connection unavailable")
		}
		start.Params.GithubToken = token
		if err := c.applyGitHubConnectionURLs(ctx, &start, start.Params.ConnectionID); err != nil {
			return nil, echo.NewHTTPError(422, "GitHub connection settings unavailable")
		}
	}
	teams := az.TeamScope.Teams
	if cfg.Scope == "team" {
		teams = []string{cfg.TeamID}
	}
	settings, err := resolver.ResolveRestartSettings(ctx, cfg.SessionID, start, cfg.UserID, teams)
	if err != nil {
		return nil, echo.NewHTTPError(422, "failed to resolve session settings").SetInternal(err)
	}
	return settings, nil
}

// Legacy sessions can supply the explicit start input instead of guessing which
// values in a resolved snapshot originally came from a profile or an override.
func (c *SessionController) saveLegacyStartupInput(ctx echo.Context, input *entities.StartRequest) error {
	store, ok := c.sessionRunnerStore.(sessionConfigurationStore)
	if !ok {
		return echo.NewHTTPError(501, "settings reload unavailable")
	}
	id := ctx.Param("sessionId")
	userID, scope, teamID := "", "", ""
	if session := c.getSessionManager().GetSession(id); session != nil {
		userID, scope, teamID = session.UserID(), string(session.Scope()), session.TeamID()
	}
	if userID == "" && c.sessionRouteRepo != nil {
		route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), id)
		if err != nil {
			return echo.NewHTTPError(503, "session lookup failed")
		}
		if route != nil {
			userID, scope, teamID = route.UserID, route.Scope, route.TeamID
		}
	}
	if userID == "" {
		return echo.NewHTTPError(404, "session not found")
	}
	az := auth.GetAuthorizationContext(ctx)
	if az == nil || !az.CanAccessResource(userID, scope, teamID) || (scope != "team" && (az.User == nil || string(az.User.ID()) != userID)) {
		return echo.NewHTTPError(403, "session access denied")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return echo.NewHTTPError(400, "invalid startup input")
	}
	if err := store.CreateConfiguration(ctx.Request().Context(), &core.Configuration{SessionID: id, UserID: userID, Scope: scope, TeamID: teamID, Input: raw, ProfileID: input.SessionProfileID}); err != nil {
		return echo.NewHTTPError(409, "startup input already exists")
	}
	return nil
}

// Only forward known validation messages; arbitrary manager responses can contain
// runtime details or secret values. Status still distinguishes unsupported peers.
func restartManagerResponseError(resp *http.Response) error {
	var body struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
	message := fmt.Sprintf("manager restart request failed (HTTP %d)", resp.StatusCode)
	switch resp.StatusCode {
	case http.StatusNotFound:
		message = "manager restart endpoint or session not found; check the manager version and session registration"
	case http.StatusNotImplemented:
		message = "manager does not support conversation restart; update the manager to a compatible version"
	case http.StatusUnauthorized, http.StatusForbidden:
		message = "manager rejected control authentication; check the manager connection"
	case http.StatusUnprocessableEntity:
		switch body.Message {
		case "session settings unavailable",
			"conversation checkpoint storage is required for restart",
			"conversation resume supports only Claude ACP and Codex ACP",
			"agent type cannot change when resuming a conversation",
			"oneshot sessions cannot restart",
			"repository settings cannot change when resuming a conversation",
			"model or provider routing cannot change when resuming",
			"Codex connection or model cannot change when resuming",
			"Claude connection or model cannot change when resuming":
			message = body.Message
		}
	}
	return echo.NewHTTPError(resp.StatusCode, message)
}
