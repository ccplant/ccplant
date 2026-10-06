package controllers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type templateizeSessionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	WaitForIdle bool   `json:"wait_for_idle,omitempty"`
}

func (c *SessionController) TemplateizeSession(ctx echo.Context) error {
	if c.contextTemplateRepo == nil {
		return echo.NewHTTPError(http.StatusNotImplemented, "session context templates are unavailable")
	}
	sessionID := ctx.Param("sessionId")
	session := c.getSessionManager().GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil || !authz.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to templateize this session")
	}
	var input templateizeSessionRequest
	if err := ctx.Bind(&input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if session.Status() == "running" {
		if !input.WaitForIdle {
			return echo.NewHTTPError(http.StatusConflict, "session is busy")
		}
		deadline := time.Now().Add(30 * time.Second)
		for session.Status() == "running" && time.Now().Before(deadline) {
			select {
			case <-ctx.Request().Context().Done():
				return ctx.Request().Context().Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		if session.Status() == "running" {
			return echo.NewHTTPError(http.StatusConflict, "session is busy")
		}
	}
	snapshotManager, remoteSnapshot := c.getSessionManager().(repositories.SessionContextSnapshotManager)
	checkpointer, localSnapshot := c.getSessionManager().(repositories.SessionCheckpointer)
	if !remoteSnapshot && (!localSnapshot || c.sessionStateStore == nil) {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "template_unsupported")
	}
	templateID := "tpl_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	template := &entities.SessionContextTemplate{ID: templateID, SourceSessionID: sessionID, SnapshotID: templateID, Name: input.Name, Description: strings.TrimSpace(input.Description), OwnerUserID: session.UserID(), Scope: session.Scope(), TeamID: session.TeamID(), Status: entities.SessionContextTemplatePreparing, CreatedAt: time.Now().UTC()}
	if err := c.contextTemplateRepo.Create(ctx.Request().Context(), template); err != nil {
		return echo.NewHTTPError(http.StatusConflict, "failed to reserve context template")
	}
	rollback := true
	defer func() {
		if rollback {
			_ = c.contextTemplateRepo.Delete(context.Background(), templateID)
		}
	}()
	if remoteSnapshot {
		if err := snapshotManager.CreateSessionContextSnapshot(ctx.Request().Context(), sessionID, templateID); err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to preserve template snapshot").SetInternal(err)
		}
	} else {
		if err := checkpointer.CheckpointSessionState(ctx.Request().Context(), sessionID); err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to checkpoint session").SetInternal(err)
		}
		snapshot, err := c.sessionStateStore.Load(ctx.Request().Context(), sessionID)
		if errors.Is(err, os.ErrNotExist) {
			return echo.NewHTTPError(http.StatusConflict, "snapshot_unavailable")
		}
		if err != nil {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "template_not_portable").SetInternal(err)
		}
		defer func() { _ = snapshot.Close() }()
		if err := c.sessionStateStore.Save(ctx.Request().Context(), templateID, snapshot); err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to preserve template snapshot").SetInternal(err)
		}
	}
	if err := c.sessionCreator.DeleteSessionByID(sessionID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to retire source session").SetInternal(err)
	}
	if c.sessionRouteRepo != nil {
		if route, routeErr := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID); routeErr == nil && route != nil {
			route.Status, route.ContextTemplateID, route.StatusUpdatedAt = "templated", templateID, time.Now().UTC()
			if routeErr = c.sessionRouteRepo.Save(ctx.Request().Context(), route); routeErr != nil {
				log.Printf("failed to preserve templated session tombstone for %s: %v", sessionID, routeErr)
			}
		}
	}
	c.cleanupSessionConfiguration(ctx.Request().Context(), sessionID)
	c.revokeGitHubBrokerLeases(ctx.Request().Context(), sessionID)
	template.Status = entities.SessionContextTemplateReady
	if err := c.contextTemplateRepo.Update(ctx.Request().Context(), template); err != nil {
		rollback = false
		return echo.NewHTTPError(http.StatusInternalServerError, "template created but finalization is pending").SetInternal(err)
	}
	rollback = false
	return ctx.JSON(http.StatusCreated, template)
}

func (c *SessionController) authorizedTemplate(ctx echo.Context, modify bool) (*entities.SessionContextTemplate, error) {
	if c.contextTemplateRepo == nil {
		return nil, echo.NewHTTPError(http.StatusNotImplemented, "session context templates are unavailable")
	}
	template, err := c.contextTemplateRepo.Get(ctx.Request().Context(), ctx.Param("templateId"))
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, echo.NewHTTPError(http.StatusNotFound, "context template not found")
	}
	authz := auth.GetAuthorizationContext(ctx)
	allowed := authz != nil && authz.CanAccessResource(template.OwnerUserID, string(template.Scope), template.TeamID)
	if modify {
		allowed = authz != nil && authz.CanModifyResource(template.OwnerUserID, string(template.Scope), template.TeamID)
	}
	if !allowed {
		return nil, echo.NewHTTPError(http.StatusForbidden, "context template access denied")
	}
	return template, nil
}

func (c *SessionController) ListSessionContextTemplates(ctx echo.Context) error {
	if c.contextTemplateRepo == nil {
		return echo.NewHTTPError(http.StatusNotImplemented, "session context templates are unavailable")
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	items, err := c.contextTemplateRepo.List(ctx.Request().Context(), repositories.SessionContextTemplateFilter{UserID: authz.PersonalScope.UserID, TeamIDs: authz.TeamScope.Teams})
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"templates": items})
}

func (c *SessionController) GetSessionContextTemplate(ctx echo.Context) error {
	template, err := c.authorizedTemplate(ctx, false)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, template)
}

func (c *SessionController) UpdateSessionContextTemplate(ctx echo.Context) error {
	template, err := c.authorizedTemplate(ctx, true)
	if err != nil {
		return err
	}
	var input struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := ctx.Bind(&input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if input.Name != nil {
		template.Name = strings.TrimSpace(*input.Name)
		if template.Name == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "name is required")
		}
	}
	if input.Description != nil {
		template.Description = strings.TrimSpace(*input.Description)
	}
	if err := c.contextTemplateRepo.Update(ctx.Request().Context(), template); err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, template)
}

func (c *SessionController) DeleteSessionContextTemplate(ctx echo.Context) error {
	template, err := c.authorizedTemplate(ctx, true)
	if err != nil {
		return err
	}
	snapshotManager, remoteSnapshot := c.getSessionManager().(repositories.SessionContextSnapshotManager)
	deleter, localSnapshot := c.sessionStateStore.(services.SessionStateDeleter)
	if !remoteSnapshot && !localSnapshot {
		return echo.NewHTTPError(http.StatusNotImplemented, "template snapshot deletion is unavailable")
	}
	if err := c.contextTemplateRepo.Delete(ctx.Request().Context(), template.ID); err != nil {
		return err
	}
	var deleteErr error
	if remoteSnapshot {
		deleteErr = snapshotManager.DeleteSessionContextSnapshot(ctx.Request().Context(), template.SnapshotID)
	} else {
		deleteErr = deleter.Delete(ctx.Request().Context(), template.SnapshotID)
	}
	if deleteErr != nil {
		log.Printf("failed to delete orphaned template snapshot %s: %v", template.SnapshotID, deleteErr)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionController) resolveContextTemplate(ctx echo.Context, req *entities.StartRequest) (*entities.SessionContextTemplate, error) {
	if req.ContextTemplateID == "" {
		return nil, nil
	}
	if c.contextTemplateRepo == nil {
		return nil, echo.NewHTTPError(http.StatusNotImplemented, "session context templates are unavailable")
	}
	template, err := c.contextTemplateRepo.Get(ctx.Request().Context(), req.ContextTemplateID)
	if err != nil {
		return nil, err
	}
	if template == nil || template.Status != entities.SessionContextTemplateReady {
		return nil, echo.NewHTTPError(http.StatusNotFound, "context template not found")
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil || !authz.CanAccessResource(template.OwnerUserID, string(template.Scope), template.TeamID) {
		return nil, echo.NewHTTPError(http.StatusForbidden, "context template access denied")
	}
	if req.Scope != template.Scope || req.TeamID != template.TeamID {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "session scope must match context template scope")
	}
	if req.Params == nil {
		req.Params = &entities.SessionParams{}
	}
	if req.Params.ResumeFrom != "" {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "context_template_id and resume_from are mutually exclusive")
	}
	req.Params.ResumeFrom = template.SnapshotID
	return template, nil
}

func (c *SessionController) recordContextTemplateUse(ctx context.Context, template *entities.SessionContextTemplate) {
	if template == nil {
		return
	}
	now := time.Now().UTC()
	template.LastUsedAt = &now
	template.UseCount++
	if err := c.contextTemplateRepo.Update(ctx, template); err != nil {
		log.Printf("failed to update context template usage for %s: %v", template.ID, err)
	}
}
