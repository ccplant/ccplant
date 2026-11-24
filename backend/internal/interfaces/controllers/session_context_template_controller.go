package controllers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
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
	source, err := c.loadTemplateSource(ctx.Request().Context(), ctx.Param("sessionId"))
	if err != nil {
		return err
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil || !authz.CanModifyResource(source.ownerUserID, string(source.scope), source.teamID) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to templateize this session")
	}
	input, err := bindTemplateizeRequest(ctx)
	if err != nil {
		return err
	}
	if err := c.waitForTemplateSource(ctx.Request().Context(), source, input.WaitForIdle); err != nil {
		return err
	}
	templateID := "tpl_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	template := c.newContextTemplate(ctx.Request().Context(), source, input, templateID)
	if err := c.contextTemplateRepo.Create(ctx.Request().Context(), template); err != nil {
		return echo.NewHTTPError(http.StatusConflict, "failed to reserve context template")
	}
	rollback := true
	defer func() {
		if rollback {
			if err := c.contextTemplateRepo.Delete(context.Background(), templateID); err != nil {
				log.Printf("failed to roll back context template %s: %v", templateID, err)
			}
		}
	}()
	if err := c.preserveTemplateSnapshot(ctx.Request().Context(), source, templateID); err != nil {
		return err
	}
	if err := c.retireTemplateSource(ctx.Request().Context(), source, templateID); err != nil {
		return err
	}
	template.Status = entities.SessionContextTemplateReady
	if err := c.contextTemplateRepo.Update(ctx.Request().Context(), template); err != nil {
		rollback = false
		return echo.NewHTTPError(http.StatusInternalServerError, "template created but finalization is pending").SetInternal(err)
	}
	rollback = false
	return ctx.JSON(http.StatusCreated, template)
}

// SaveSessionAsWorkspace is the canonical workspace API. TemplateizeSession is
// retained as a backwards-compatible alias for older clients.
func (c *SessionController) SaveSessionAsWorkspace(ctx echo.Context) error {
	return c.TemplateizeSession(ctx)
}

func (c *SessionController) retireTunneledContextSource(ctx context.Context, route *repositories.SessionRoute) error {
	enqueuer, ok := c.esmControlTunnel.(esmControlEnqueuer)
	if !ok {
		return errors.New("external session manager lifecycle queue is unavailable")
	}
	target := "http://esm.local/api/v1/sessions/" + url.PathEscape(route.RemoteSessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, target, nil)
	if err != nil {
		return err
	}
	requestID, err := enqueuer.Enqueue(ctx, route.ManagerID, route.SessionID, route.RemoteSessionID, req)
	if err != nil {
		return err
	}
	if requestID == "" {
		return errors.New("external session manager returned no lifecycle request id")
	}
	return nil
}

func (c *SessionController) checkpointTunneledRuntimeSnapshot(ctx context.Context, route *repositories.SessionRoute, snapshotID string) error {
	target := "http://session.local/internal/checkpoint-session-state?snapshot_id=" + url.QueryEscape(snapshotID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
	if err != nil {
		return err
	}
	resp, err := c.esmControlTunnel.Do(ctx, route.SessionID, route.SessionID, route.RemoteSessionID, req)
	if err != nil {
		return err
	}
	defer closeutils.SafeCloseResponseBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(resp.Status)
	}
	return nil
}

func (c *SessionController) authorizedTemplate(ctx echo.Context, modify bool) (*entities.SessionContextTemplate, error) {
	if c.contextTemplateRepo == nil {
		return nil, echo.NewHTTPError(http.StatusNotImplemented, "session context templates are unavailable")
	}
	id := ctx.Param("workspaceId")
	if id == "" {
		id = ctx.Param("templateId")
	}
	template, err := c.contextTemplateRepo.Get(ctx.Request().Context(), id)
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

func (c *SessionController) ListWorkspaces(ctx echo.Context) error {
	if c.contextTemplateRepo == nil {
		return echo.NewHTTPError(http.StatusNotImplemented, "workspaces are unavailable")
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	items, err := c.contextTemplateRepo.List(ctx.Request().Context(), repositories.SessionContextTemplateFilter{UserID: authz.PersonalScope.UserID, TeamIDs: authz.TeamScope.Teams})
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"workspaces": items})
}

func (c *SessionController) GetWorkspace(ctx echo.Context) error {
	return c.GetSessionContextTemplate(ctx)
}

func (c *SessionController) UpdateWorkspace(ctx echo.Context) error {
	return c.UpdateSessionContextTemplate(ctx)
}

func (c *SessionController) DeleteWorkspace(ctx echo.Context) error {
	return c.DeleteSessionContextTemplate(ctx)
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
	var remoteRoute *repositories.SessionRoute
	if c.sessionRouteRepo != nil {
		remoteRoute, _ = c.sessionRouteRepo.Get(ctx.Request().Context(), template.SourceSessionID)
	}
	tunneledSnapshot := remoteRoute != nil && remoteRoute.ManagerID != "" && remoteRoute.RemoteSessionID != "" && c.esmControlTunnel != nil && c.esmControlTunnel.IsConnected(ctx.Request().Context(), remoteRoute.ManagerID)
	if !tunneledSnapshot && !remoteSnapshot && !localSnapshot {
		return echo.NewHTTPError(http.StatusNotImplemented, "template snapshot deletion is unavailable")
	}
	if err := c.contextTemplateRepo.Delete(ctx.Request().Context(), template.ID); err != nil {
		return err
	}
	var deleteErr error
	if tunneledSnapshot {
		deleteErr = c.manageTunneledContextSnapshot(ctx.Request().Context(), remoteRoute, template.SnapshotID, http.MethodDelete)
	} else if remoteSnapshot {
		deleteErr = snapshotManager.DeleteSessionContextSnapshot(ctx.Request().Context(), template.SnapshotID)
	} else {
		deleteErr = deleter.Delete(ctx.Request().Context(), template.SnapshotID)
	}
	if deleteErr != nil {
		log.Printf("failed to delete orphaned template snapshot %s: %v", template.SnapshotID, deleteErr)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionController) manageTunneledContextSnapshot(ctx context.Context, route *repositories.SessionRoute, snapshotID, method string) error {
	target := "http://esm.local/api/v1/sessions/" + url.PathEscape(route.RemoteSessionID) + "/context-snapshots/" + url.PathEscape(snapshotID)
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return err
	}
	if method == http.MethodPost {
		req.Header.Set("X-CCPlant-Template-Snapshot-Ready", "1")
	}
	resp, err := c.esmControlTunnel.Do(ctx, route.ManagerID, route.SessionID, route.RemoteSessionID, req)
	if err != nil {
		return err
	}
	defer closeutils.SafeCloseResponseBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(resp.Status)
	}
	return nil
}

func (c *SessionController) resolveContextTemplate(ctx echo.Context, req *entities.StartRequest) (*entities.SessionContextTemplate, error) {
	workspaceID, err := requestedWorkspaceID(req)
	if err != nil {
		return nil, err
	}
	if workspaceID == "" {
		return nil, nil
	}
	if c.contextTemplateRepo == nil {
		return nil, echo.NewHTTPError(http.StatusNotImplemented, "session context templates are unavailable")
	}
	template, err := c.contextTemplateRepo.Get(ctx.Request().Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	if err := validateContextTemplateRequest(ctx, req, template); err != nil {
		return nil, err
	}
	c.backfillContextTemplate(ctx.Request().Context(), template)
	applyContextTemplateLaunch(req, template)
	req.Params.ResumeFrom = template.SnapshotID
	return template, nil
}

func requestedWorkspaceID(req *entities.StartRequest) (string, error) {
	if req.WorkspaceID != "" && req.ContextTemplateID != "" && req.WorkspaceID != req.ContextTemplateID {
		return "", echo.NewHTTPError(http.StatusBadRequest, "workspace_id and context_template_id must match when both are provided")
	}
	if req.WorkspaceID != "" {
		return req.WorkspaceID, nil
	}
	return req.ContextTemplateID, nil
}

func validateContextTemplateRequest(ctx echo.Context, req *entities.StartRequest, template *entities.SessionContextTemplate) error {
	if template == nil || template.Status != entities.SessionContextTemplateReady {
		return echo.NewHTTPError(http.StatusNotFound, "context template not found")
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil || !authz.CanAccessResource(template.OwnerUserID, string(template.Scope), template.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "context template access denied")
	}
	if req.Scope != template.Scope || req.TeamID != template.TeamID {
		return echo.NewHTTPError(http.StatusBadRequest, "session scope must match context template scope")
	}
	if req.Params == nil {
		req.Params = &entities.SessionParams{}
	}
	if req.Params.ResumeFrom != "" {
		return echo.NewHTTPError(http.StatusBadRequest, "workspace_id and resume_from are mutually exclusive")
	}
	return nil
}

func (c *SessionController) backfillContextTemplate(ctx context.Context, template *entities.SessionContextTemplate) {
	if len(template.Tags) != 0 || c.sessionRouteRepo == nil {
		return
	}
	route, err := c.sessionRouteRepo.Get(ctx, template.SourceSessionID)
	if err != nil || route == nil {
		return
	}
	template.AgentType, template.SessionProfileID, template.Tags = c.templateLaunchContext(ctx, template.SourceSessionID, nil, route)
	if err := c.contextTemplateRepo.Update(ctx, template); err != nil {
		log.Printf("failed to backfill context template launch metadata for %s: %v", template.ID, err)
	}
}
