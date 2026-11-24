package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
)

func (o *sessionStartOperation) resolveReuseAndInput() (bool, error) {
	var err error
	o.reuseResolution = map[string]interface{}{"reason": "reuse_not_requested", "requested": false}
	if o.dryRun {
		if existingID, reused, status, err := o.controller.previewReuseStartSession(o.context, o.request, o.userID); err != nil {
			return false, err
		} else if reused {
			return true, o.context.JSON(http.StatusOK, map[string]interface{}{
				"dry_run": true, "session_id": existingID, "decision": "reuse",
				"reuse":      map[string]interface{}{"session_id": existingID, "status": status, "stop_before_reuse": o.request.StopBeforeReuse},
				"resolution": map[string]interface{}{"reuse": map[string]interface{}{"requested": true, "reason": "matching_live_session_selected", "session_id": existingID, "status": status}},
			})
		} else if len(o.request.ReuseMatchTags) > 0 && o.request.ReuseMessage != "" {
			o.reuseResolution = map[string]interface{}{"reason": "no_matching_live_session", "requested": true}
		}
	} else if existingID, reused, err := o.controller.reuseStartSession(o.context, o.request, o.userID); err != nil {
		return false, err
	} else if reused {
		return true, o.context.JSON(http.StatusOK, map[string]interface{}{"session_id": existingID, "session_reused": true})
	}
	if o.request.MaxSessions > 0 && len(o.controller.getSessionManager().ListSessions(entities.SessionFilter{Tags: o.request.LimitMatchTags})) >= o.request.MaxSessions {
		return false, echo.NewHTTPError(http.StatusTooManyRequests, fmt.Sprintf("session limit reached: maximum %d sessions", o.request.MaxSessions))
	}
	o.reuseSession = o.request.ReuseSession
	// Reuse and limit controls belong to this /start invocation and must not be
	// persisted as part of the session's restart configuration.
	o.request.ReuseMatchTags, o.request.ReuseMessage, o.request.StopBeforeReuse = nil, "", false
	o.request.ReuseSession = false
	o.request.LimitMatchTags, o.request.MaxSessions = nil, 0

	// Persist the explicit input before profile defaults are merged.
	o.input, err = json.Marshal(o.request)
	if err != nil {
		return false, echo.NewHTTPError(500, "failed to preserve session input")
	}
	return false, nil
}

func (o *sessionStartOperation) applyProfile() error {
	o.profileResolution = map[string]interface{}{"reason": "profile_repository_unavailable"}
	if o.controller.sessionProfileRepo != nil {
		profile, profileReason, err := o.controller.resolveSessionProfile(
			o.context.Request().Context(),
			o.request.SessionProfileID,
			o.userID,
			o.request.Scope,
			o.request.TeamID,
			o.request.Tags,
			o.authorization,
		)
		if err != nil {
			return err
		}
		o.profileResolution = map[string]interface{}{"reason": profileReason}
		if profile != nil {
			o.profileResolution["selected_profile_id"] = profile.ID()
			o.profileResolution["selected_profile_name"] = profile.Name()
			if tags := profile.SelectorTags(); len(tags) > 0 {
				o.profileResolution["selector_tags"] = tags
			}
		}
		profileCfg, err := sessionuc.ResolveEffectiveSessionProfileConfig(
			o.context.Request().Context(),
			o.controller.sessionProfileRepo,
			profile,
			o.request.Scope,
			o.userID,
			o.request.TeamID,
		)
		if err != nil {
			return sessionProfileResolutionHTTPError(err)
		}
		applySessionProfile(&o.request, profile, profileCfg, o.explicitSandbox, o.explicitDocker)
	}
	applySessionTTLDefault(&o.request, o.ttl, o.reuseSession)
	return nil
}

func (o *sessionStartOperation) persistInput() error {
	if !o.dryRun {
		if store, ok := o.controller.sessionRunnerStore.(sessionConfigurationStore); ok {
			config := &sessionrunnercore.Configuration{SessionID: o.sessionID, TriggeredUserID: o.request.TriggeredUserID, Input: o.input, ProfileID: o.request.ResolvedSessionProfileID, UserID: o.userID, Scope: string(o.request.Scope), TeamID: o.request.TeamID, Teams: o.teams}
			if err := store.CreateConfiguration(o.context.Request().Context(), config); err != nil {
				return echo.NewHTTPError(503, "failed to preserve session input")
			}
		}
	}

	return nil
}
