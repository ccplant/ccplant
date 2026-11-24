package controllers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type templateSource struct {
	session     entities.Session
	route       *repositories.SessionRoute
	sessionID   string
	ownerUserID string
	scope       entities.ResourceScope
	teamID      string
	status      string
}

func (c *SessionController) loadTemplateSource(ctx context.Context, sessionID string) (templateSource, error) {
	source := templateSource{sessionID: sessionID, session: c.getSessionManager().GetSession(sessionID)}
	if c.sessionRouteRepo != nil {
		route, err := c.sessionRouteRepo.Get(ctx, sessionID)
		if err != nil {
			log.Printf("failed to load session route for template creation %s: %v", sessionID, err)
		}
		source.route = route
	}
	if source.session == nil && source.route == nil {
		return source, echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}
	if source.session != nil {
		source.ownerUserID, source.scope = source.session.UserID(), source.session.Scope()
		source.teamID, source.status = source.session.TeamID(), source.session.Status()
	} else {
		source.ownerUserID, source.scope = source.route.UserID, entities.ResourceScope(source.route.Scope)
		source.teamID, source.status = source.route.TeamID, source.route.Status
	}
	return source, nil
}

func bindTemplateizeRequest(ctx echo.Context) (templateizeSessionRequest, error) {
	var input templateizeSessionRequest
	if err := ctx.Bind(&input); err != nil {
		return input, echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return input, echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	return input, nil
}

func (c *SessionController) waitForTemplateSource(ctx context.Context, source templateSource, wait bool) error {
	if source.status != "running" {
		return nil
	}
	if !wait {
		return echo.NewHTTPError(http.StatusConflict, "session is busy")
	}
	deadline := time.Now().Add(30 * time.Second)
	status := source.status
	for status == "running" && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
			status = c.templateSourceStatus(ctx, source)
		}
	}
	if status == "running" {
		return echo.NewHTTPError(http.StatusConflict, "session is busy")
	}
	return nil
}

func (c *SessionController) templateSourceStatus(ctx context.Context, source templateSource) string {
	if source.session != nil {
		return source.session.Status()
	}
	refreshed, err := c.sessionRouteRepo.Get(ctx, source.sessionID)
	if err != nil {
		log.Printf("failed to refresh session route %s: %v", source.sessionID, err)
		return source.status
	}
	if refreshed == nil {
		return source.status
	}
	return refreshed.Status
}

func (c *SessionController) newContextTemplate(ctx context.Context, source templateSource, input templateizeSessionRequest, templateID string) *entities.SessionContextTemplate {
	agentType, profileID, tags := c.templateLaunchContext(ctx, source.sessionID, source.session, source.route)
	return &entities.SessionContextTemplate{
		ID: templateID, SourceSessionID: source.sessionID, SnapshotID: templateID,
		Name: input.Name, Description: strings.TrimSpace(input.Description), OwnerUserID: source.ownerUserID,
		Scope: source.scope, TeamID: source.teamID, AgentType: agentType, SessionProfileID: profileID,
		Tags: tags, Status: entities.SessionContextTemplatePreparing, CreatedAt: time.Now().UTC(),
	}
}

func (c *SessionController) preserveTemplateSnapshot(ctx context.Context, source templateSource, templateID string) error {
	snapshotManager, remoteSnapshot := c.getSessionManager().(repositories.SessionContextSnapshotManager)
	tunneled := c.tunneledTemplateSource(ctx, source.route)
	checkpointer, localSnapshot := c.getSessionManager().(repositories.SessionCheckpointer)
	if !tunneled && !remoteSnapshot && (!localSnapshot || c.sessionStateStore == nil) {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "template_unsupported")
	}
	if tunneled {
		return c.preserveTunneledTemplateSnapshot(ctx, source.route, templateID)
	}
	if remoteSnapshot {
		if err := snapshotManager.CreateSessionContextSnapshot(ctx, source.sessionID, templateID); err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to preserve template snapshot").SetInternal(err)
		}
		return nil
	}
	return c.preserveLocalTemplateSnapshot(ctx, checkpointer, source.sessionID, templateID)
}

func (c *SessionController) tunneledTemplateSource(ctx context.Context, route *repositories.SessionRoute) bool {
	return route != nil && route.ManagerID != "" && route.RemoteSessionID != "" && c.esmControlTunnel != nil && c.esmControlTunnel.IsConnected(ctx, route.ManagerID)
}

func (c *SessionController) preserveTunneledTemplateSnapshot(ctx context.Context, route *repositories.SessionRoute, templateID string) error {
	if err := c.checkpointTunneledRuntimeSnapshot(ctx, route, templateID); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to checkpoint session").SetInternal(err)
	}
	if err := c.manageTunneledContextSnapshot(ctx, route, templateID, http.MethodPost); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to preserve template snapshot").SetInternal(err)
	}
	return nil
}

func (c *SessionController) preserveLocalTemplateSnapshot(ctx context.Context, checkpointer repositories.SessionCheckpointer, sessionID, templateID string) error {
	if err := checkpointer.CheckpointSessionState(ctx, sessionID); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to checkpoint session").SetInternal(err)
	}
	snapshot, err := c.sessionStateStore.Load(ctx, sessionID)
	if errors.Is(err, os.ErrNotExist) {
		return echo.NewHTTPError(http.StatusConflict, "snapshot_unavailable")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "template_not_portable").SetInternal(err)
	}
	defer func() {
		if err := snapshot.Close(); err != nil {
			log.Printf("failed to close context template snapshot %s: %v", templateID, err)
		}
	}()
	if err := c.sessionStateStore.Save(ctx, templateID, snapshot); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to preserve template snapshot").SetInternal(err)
	}
	return nil
}

func (c *SessionController) retireTemplateSource(ctx context.Context, source templateSource, templateID string) error {
	if c.tunneledTemplateSource(ctx, source.route) {
		if err := c.retireTunneledContextSource(ctx, source.route); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to retire source session").SetInternal(err)
		}
	} else if err := c.sessionCreator.DeleteSessionByID(source.sessionID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retire source session").SetInternal(err)
	}
	c.preserveTemplatedRoute(ctx, source.sessionID, templateID)
	c.cleanupSessionConfiguration(ctx, source.sessionID)
	c.revokeGitHubBrokerLeases(ctx, source.sessionID)
	return nil
}

func (c *SessionController) preserveTemplatedRoute(ctx context.Context, sessionID, templateID string) {
	if c.sessionRouteRepo == nil {
		return
	}
	route, err := c.sessionRouteRepo.Get(ctx, sessionID)
	if err != nil || route == nil {
		return
	}
	route.Status, route.ContextTemplateID, route.StatusUpdatedAt = "templated", templateID, time.Now().UTC()
	if err := c.sessionRouteRepo.Save(ctx, route); err != nil {
		log.Printf("failed to preserve templated session tombstone for %s: %v", sessionID, err)
	}
}
