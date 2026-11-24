package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

func (c *SessionController) waitRestartIdle(ctx context.Context, id string, route *repositories.SessionRoute, policy string) error {
	if policy == "interrupt" && route != nil {
		request := managerRestartRequest{route: route, action: "stop", id: uuid.NewString()}
		if err := c.sendRestartToManager(ctx, request); err != nil {
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
