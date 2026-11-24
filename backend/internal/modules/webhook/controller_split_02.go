package webhook

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func (c *WebhookController) ListWebhooks(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	user := auth.GetUserFromContext(ctx)
	scopeFilter := ctx.QueryParam("scope")
	teamIDFilter := ctx.QueryParam("team_id")

	// Service accounts cannot use user scope; transparently route to team scope.
	scopeFilter, teamIDFilter = auth.ResolveUserScope(user, scopeFilter, teamIDFilter)

	var userID string
	var userTeamIDs []string
	if user != nil {
		userID = string(user.ID())
		if resolved, ok := user.ResolvedTeamIDs(); ok {
			userTeamIDs = resolved
		} else if githubInfo := user.GitHubInfo(); githubInfo != nil {
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				userTeamIDs = append(userTeamIDs, teamSlug)
			}
		}
	}

	// Build filter
	filter := repositories.WebhookFilter{
		Scope:   entities.ResourceScope(scopeFilter),
		TeamID:  teamIDFilter,
		TeamIDs: userTeamIDs,
	}

	if typeParam := ctx.QueryParam("type"); typeParam != "" {
		filter.Type = entities.WebhookType(typeParam)
	}
	if status := ctx.QueryParam("status"); status != "" {
		filter.Status = entities.WebhookStatus(status)
	}
	// For non-team scope, always filter by user ID - even admins should not see
	// other users' personal webhooks. Admin privileges apply to team-scoped resources only.
	if user != nil && scopeFilter != "team" && teamIDFilter == "" {
		filter.UserID = userID
	}

	webhooks, err := c.repo.List(ctx.Request().Context(), filter)
	if err != nil {
		log.Printf("Failed to list webhooks: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list webhooks")
	}

	responses := make([]WebhookResponse, 0, len(webhooks))
	for _, w := range webhooks {

		webhookScope := w.Scope()
		if scopeFilter == string(entities.ScopeTeam) {
			if webhookScope != entities.ScopeTeam {
				continue
			}
		} else {
			if webhookScope == entities.ScopeTeam {
				continue
			}
		}

		// Admin can access all team-scoped webhooks; user-scoped requires ownership.
		// This check mirrors CanAccessResource: admin bypasses only for team scope.
		if webhookScope == entities.ScopeTeam {
			if user != nil && (user.IsAdmin() || user.IsMemberOfTeam(w.TeamID())) {
				responses = append(responses, c.toResponse(ctx, w))
			}
		} else {
			if user != nil && w.UserID() == string(user.ID()) {
				responses = append(responses, c.toResponse(ctx, w))
			}
		}
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"webhooks": responses,
	})
}

// GetWebhook handles GET /webhooks/:id
func (c *WebhookController) GetWebhook(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		log.Printf("Failed to get webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	if !c.userCanAccessWebhook(ctx, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this webhook")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(ctx, webhook))
}

// UpdateWebhook handles PUT /webhooks/:id
func (c *WebhookController) UpdateWebhook(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	if !c.userCanAccessWebhook(ctx, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to update this webhook")
	}

	var req UpdateWebhookRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate templates before updating webhook
	if err := c.validateWebhookTemplatesForUpdate(webhook.WebhookType(), req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Template validation error: %v", err))
	}

	// Reject enterprise URLs that point at internal infrastructure (SSRF)
	if req.GitHub != nil {
		if err := validateEnterpriseURL(req.GitHub.EnterpriseURL); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	// Apply updates
	if req.Name != nil {
		webhook.SetName(*req.Name)
	}
	if req.Status != nil {
		webhook.SetStatus(*req.Status)
	}
	if req.Secret != nil && *req.Secret != "" {
		webhook.SetSecret(*req.Secret)
	}
	if req.SignatureHeader != nil {
		webhook.SetSignatureHeader(*req.SignatureHeader)
	}
	if req.SignatureType != nil {
		webhook.SetSignatureType(*req.SignatureType)
	}
	if req.SignaturePrefix != nil {
		webhook.SetSignaturePrefix(*req.SignaturePrefix)
	}
	if req.MaxSessions != nil && *req.MaxSessions > 0 {
		webhook.SetMaxSessions(*req.MaxSessions)
	}
	if req.GitHub != nil {
		webhook.SetGitHub(c.requestToGitHubConfig(req.GitHub))
	}
	if req.Triggers != nil {
		webhook.SetTriggers(c.requestToTriggers(req.Triggers))
	}
	if req.SessionConfig != nil {
		webhook.SetSessionConfig(c.requestToSessionConfig(req.SessionConfig))
	}

	// Refresh UserTeams from the current auth context so that team membership
	// changes since webhook creation are picked up (mirrors UpdateSchedule behaviour).
	if webhook.Scope() != entities.ScopeTeam {
		if azCtx := auth.GetAuthorizationContext(ctx); azCtx != nil {
			webhook.SetUserTeams(azCtx.TeamScope.Teams)
		}
	}

	if err := c.repo.Update(ctx.Request().Context(), webhook); err != nil {
		log.Printf("Failed to update webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update webhook")
	}

	log.Printf("Updated webhook %s", id)

	return ctx.JSON(http.StatusOK, c.toResponse(ctx, webhook))
}

// DeleteWebhook handles DELETE /webhooks/:id
func (c *WebhookController) DeleteWebhook(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	if !c.userCanAccessWebhook(ctx, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this webhook")
	}

	if err := c.repo.Delete(ctx.Request().Context(), id); err != nil {
		log.Printf("Failed to delete webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete webhook")
	}

	log.Printf("Deleted webhook %s", id)

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message": "Webhook deleted successfully",
		"id":      id,
	})
}

// RegenerateSecret handles POST /webhooks/:id/regenerate-secret
func (c *WebhookController) RegenerateSecret(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	if !c.userCanAccessWebhook(ctx, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to regenerate secret for this webhook")
	}

	// Generate new secret
	newSecret := generateSecret()
	webhook.SetSecret(newSecret)

	if err := c.repo.Update(ctx.Request().Context(), webhook); err != nil {
		log.Printf("Failed to regenerate secret for webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to regenerate secret")
	}

	log.Printf("Regenerated secret for webhook %s", id)

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"id":     id,
		"secret": newSecret,
	})
}

// requestToGitHubConfig converts a GitHub config request to an entity.
func (c *WebhookController) requestToGitHubConfig(req *GitHubConfigRequest) *entities.WebhookGitHubConfig {
	gh := entities.NewWebhookGitHubConfig()
	gh.SetEnterpriseURL(req.EnterpriseURL)
	gh.SetAllowedEvents(req.AllowedEvents)
	gh.SetAllowedRepositories(req.AllowedRepositories)
	return gh
}

// requestToTriggers converts a slice of trigger requests to entities.
func (c *WebhookController) requestToTriggers(reqs []TriggerRequest) []entities.WebhookTrigger {
	triggers := make([]entities.WebhookTrigger, 0, len(reqs))
	for _, t := range reqs {
		triggerID := t.ID
		if triggerID == "" {
			triggerID = uuid.New().String()
		}
		trigger := entities.NewWebhookTrigger(triggerID, t.Name)
		trigger.SetPriority(t.Priority)
		trigger.SetEnabled(t.Enabled)
		trigger.SetStopOnMatch(t.StopOnMatch)

		var conditions entities.WebhookTriggerConditions
		if t.Conditions.GitHub != nil {
			ghCond := entities.NewWebhookGitHubConditions()
			ghCond.SetEvents(t.Conditions.GitHub.Events)
			ghCond.SetActions(t.Conditions.GitHub.Actions)
			ghCond.SetBranches(t.Conditions.GitHub.Branches)
			ghCond.SetRepositories(t.Conditions.GitHub.Repositories)
			ghCond.SetLabels(t.Conditions.GitHub.Labels)
			ghCond.SetPaths(t.Conditions.GitHub.Paths)
			ghCond.SetBaseBranches(t.Conditions.GitHub.BaseBranches)
			ghCond.SetDraft(t.Conditions.GitHub.Draft)
			ghCond.SetSender(t.Conditions.GitHub.Sender)
			conditions.SetGitHub(ghCond)
		}
		if t.Conditions.GoTemplate != "" {
			conditions.SetGoTemplate(t.Conditions.GoTemplate)
		}
		trigger.SetConditions(conditions)

		if t.SessionConfig != nil {
			trigger.SetSessionConfig(c.requestToSessionConfig(t.SessionConfig))
		}

		triggers = append(triggers, trigger)
	}
	return triggers
}

// requestToSessionConfig converts a session config request to an entity.
func (c *WebhookController) requestToSessionConfig(req *SessionConfigRequest) *entities.WebhookSessionConfig {
	config := entities.NewWebhookSessionConfig()
	config.SetEnvironment(req.Environment)
	config.SetTags(req.Tags)
	config.SetInitialMessageTemplate(req.InitialMessageTemplate)
	config.SetReuseMessageTemplate(req.ReuseMessageTemplate)
	config.SetReuseSession(req.ReuseSession)
	config.SetMountPayload(req.MountPayload)
	config.SetSessionProfileID(req.SessionProfileID)
	if req.Params != nil {
		config.SetParams(req.Params)
	}
	return config
}

// toResponse converts entity to response
func (c *WebhookController) toResponse(ctx echo.Context, w *entities.Webhook) WebhookResponse {
	resp := WebhookResponse{
		ID:              w.ID(),
		Name:            w.Name(),
		UserID:          w.UserID(),
		Scope:           w.Scope(),
		TeamID:          w.TeamID(),
		Status:          w.Status(),
		Type:            w.WebhookType(),
		Secret:          w.Secret(),
		SignatureHeader: w.SignatureHeader(),
		SignatureType:   w.SignatureType(),
		SignaturePrefix: w.SignaturePrefix(),
		WebhookURL:      c.getWebhookURL(ctx, w),
		MaxSessions:     w.MaxSessions(),
		CreatedAt:       w.CreatedAt().Format(time.RFC3339),
		UpdatedAt:       w.UpdatedAt().Format(time.RFC3339),
		DeliveryCount:   w.DeliveryCount(),
	}

	// GitHub config
	if gh := w.GitHub(); gh != nil {
		resp.GitHub = &GitHubConfigResponse{
			EnterpriseURL:       gh.EnterpriseURL(),
			AllowedEvents:       gh.AllowedEvents(),
			AllowedRepositories: gh.AllowedRepositories(),
		}
	}

	// Triggers
	triggers := w.Triggers()
	resp.Triggers = make([]TriggerResponse, 0, len(triggers))
	for _, t := range triggers {
		tr := TriggerResponse{
			ID:          t.ID(),
			Name:        t.Name(),
			Priority:    t.Priority(),
			Enabled:     t.Enabled(),
			StopOnMatch: t.StopOnMatch(),
		}

		// Conditions
		cond := t.Conditions()
		if ghCond := cond.GitHub(); ghCond != nil {
			tr.Conditions.GitHub = &GitHubConditionsResponse{
				Events:       ghCond.Events(),
				Actions:      ghCond.Actions(),
				Branches:     ghCond.Branches(),
				Repositories: ghCond.Repositories(),
				Labels:       ghCond.Labels(),
				Paths:        ghCond.Paths(),
				BaseBranches: ghCond.BaseBranches(),
				Draft:        ghCond.Draft(),
				Sender:       ghCond.Sender(),
			}
		}
		if goTemplate := cond.GoTemplate(); goTemplate != "" {
			tr.Conditions.GoTemplate = goTemplate
		}

		// Session config
		if sc := t.SessionConfig(); sc != nil {
			tr.SessionConfig = c.sessionConfigToResponse(sc)
		}

		resp.Triggers = append(resp.Triggers, tr)
	}

	// Session config
	if sc := w.SessionConfig(); sc != nil {
		resp.SessionConfig = c.sessionConfigToResponse(sc)
	}

	// Last delivery
	if ld := w.LastDelivery(); ld != nil {
		resp.LastDelivery = &DeliveryRecordResponse{
			ID:             ld.ID(),
			ReceivedAt:     ld.ReceivedAt().Format(time.RFC3339),
			Status:         string(ld.Status()),
			MatchedTrigger: ld.MatchedTrigger(),
			SessionID:      ld.SessionID(),
			Error:          ld.Error(),
		}
	}

	return resp
}
