package controllers

import (
	"errors"
	"log"
	"net/http"
	"sort"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func (o *sessionStartOperation) preview() error {
	previewer, ok := o.controller.sessionCreator.(sessionStartPreviewer)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "session start dry-run is not supported")
	}
	preview, err := previewer.PreviewSession(o.context.Request().Context(), o.sessionID, o.request, o.userID, o.role, o.teams)
	if err != nil {
		var quotaErr *sessionrunnercore.QuotaExceededError
		if errors.As(err, &quotaErr) {
			return o.context.JSON(http.StatusTooManyRequests, map[string]any{
				"error": quotaErr.Error(), "pool": quotaErr.Pool, "binding_id": quotaErr.BindingID,
				"max_concurrent": quotaErr.MaxConcurrent, "active": quotaErr.Active,
			})
		}
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	effectiveRequest, requestRedactions, err := redactSessionStartPreview(o.request)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode effective request")
	}
	settings, settingsRedactions, err := redactSessionStartPreview(preview.Settings)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode session settings")
	}
	redactions := make([]string, 0, len(requestRedactions)+len(settingsRedactions))
	for _, path := range requestRedactions {
		redactions = append(redactions, "/effective_request"+path)
	}
	for _, path := range settingsRedactions {
		redactions = append(redactions, "/settings"+path)
	}
	sort.Strings(redactions)
	effects := []map[string]interface{}{{"kind": "persist_configuration", "would_execute": true}, {"kind": "create_session", "would_execute": true}}
	if preview.Placement.Transport == repositories.SessionRouteTransportDirectRuntime {
		effects = append(effects, map[string]interface{}{"kind": "enqueue_allocation", "would_execute": true}, map[string]interface{}{"kind": "save_session_route", "would_execute": true})
	}
	resolution := map[string]interface{}{"reuse": o.reuseResolution, "profile": o.profileResolution}
	if placementResolution, ok := preview.Resolution.(map[string]interface{}); ok {
		for key, value := range placementResolution {
			resolution[key] = value
		}
	}
	return o.context.JSON(http.StatusOK, map[string]interface{}{
		"dry_run": true, "session_id": o.sessionID, "decision": "create",
		"effective_request": effectiveRequest, "resolved_session_profile_id": o.request.ResolvedSessionProfileID,
		"placement": preview.Placement, "settings": settings, "redactions": redactions,
		"resolution": resolution,
		"effects":    effects,
		"warnings":   []string{"Dry-run does not reserve capacity; a later start may resolve differently."},
	})
}

func (o *sessionStartOperation) create() error {
	session, err := o.controller.sessionCreator.CreateSession(o.context.Request().Context(), o.sessionID, o.request, o.userID, o.role, o.teams)
	if err != nil {
		o.controller.revokeGitHubBrokerLeases(o.context.Request().Context(), o.sessionID)
		var quotaErr *sessionrunnercore.QuotaExceededError
		if errors.As(err, &quotaErr) {
			return o.context.JSON(http.StatusTooManyRequests, map[string]any{
				"error":          quotaErr.Error(),
				"pool":           quotaErr.Pool,
				"binding_id":     quotaErr.BindingID,
				"max_concurrent": quotaErr.MaxConcurrent,
				"active":         quotaErr.Active,
			})
		}
		log.Printf("Failed to create session: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create session")
	}
	o.controller.recordContextTemplateUse(o.context.Request().Context(), o.template)
	if owner, ok := session.(sessionConfigurationOwnerProvider); ok {
		if store, ok := o.controller.sessionRunnerStore.(sessionConfigurationOwnerStore); ok {
			apiVersion, kind, name, uid := owner.ConfigurationOwnerReference()
			if err := store.SetConfigurationOwnerReference(o.context.Request().Context(), o.sessionID, sessionrunnercore.OwnerReference{APIVersion: apiVersion, Kind: kind, Name: name, UID: uid}); err != nil {
				log.Printf("Failed to set configuration owner reference for session %s: %v", o.sessionID, err)
			}
		}
	}

	response := map[string]interface{}{"session_id": session.ID()}
	if o.template != nil {
		response["workspace_id"] = o.template.ID
		response["context_template_id"] = o.template.ID
	}
	return o.context.JSON(http.StatusOK, response)
}
