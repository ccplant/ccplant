package controllers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
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
	var remoteRoute *repositories.SessionRoute
	if c.sessionRouteRepo != nil {
		remoteRoute, _ = c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
	}
	if session == nil && remoteRoute == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}
	ownerUserID, scope, teamID, status := "", entities.ScopeUser, "", ""
	if session != nil {
		ownerUserID, scope, teamID, status = session.UserID(), session.Scope(), session.TeamID(), session.Status()
	} else {
		ownerUserID, scope, teamID, status = remoteRoute.UserID, entities.ResourceScope(remoteRoute.Scope), remoteRoute.TeamID, remoteRoute.Status
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil || !authz.CanModifyResource(ownerUserID, string(scope), teamID) {
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
	if status == "running" {
		if !input.WaitForIdle {
			return echo.NewHTTPError(http.StatusConflict, "session is busy")
		}
		deadline := time.Now().Add(30 * time.Second)
		for status == "running" && time.Now().Before(deadline) {
			select {
			case <-ctx.Request().Context().Done():
				return ctx.Request().Context().Err()
			case <-time.After(250 * time.Millisecond):
				if session != nil {
					status = session.Status()
				} else if refreshed, _ := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID); refreshed != nil {
					status = refreshed.Status
				}
			}
		}
		if status == "running" {
			return echo.NewHTTPError(http.StatusConflict, "session is busy")
		}
	}
	snapshotManager, remoteSnapshot := c.getSessionManager().(repositories.SessionContextSnapshotManager)
	tunneledSnapshot := remoteRoute != nil && remoteRoute.ManagerID != "" && remoteRoute.RemoteSessionID != "" && c.esmControlTunnel != nil && c.esmControlTunnel.IsConnected(ctx.Request().Context(), remoteRoute.ManagerID)
	checkpointer, localSnapshot := c.getSessionManager().(repositories.SessionCheckpointer)
	if !tunneledSnapshot && !remoteSnapshot && (!localSnapshot || c.sessionStateStore == nil) {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "template_unsupported")
	}
	templateID := "tpl_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	template := &entities.SessionContextTemplate{ID: templateID, SourceSessionID: sessionID, SnapshotID: templateID, Name: input.Name, Description: strings.TrimSpace(input.Description), OwnerUserID: ownerUserID, Scope: scope, TeamID: teamID, Status: entities.SessionContextTemplatePreparing, CreatedAt: time.Now().UTC()}
	if err := c.contextTemplateRepo.Create(ctx.Request().Context(), template); err != nil {
		return echo.NewHTTPError(http.StatusConflict, "failed to reserve context template")
	}
	rollback := true
	defer func() {
		if rollback {
			_ = c.contextTemplateRepo.Delete(context.Background(), templateID)
		}
	}()
	if tunneledSnapshot {
		if err := c.checkpointTunneledRuntimeSnapshot(ctx.Request().Context(), remoteRoute, templateID); err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to checkpoint session").SetInternal(err)
		}
		if err := c.manageTunneledContextSnapshot(ctx.Request().Context(), remoteRoute, templateID, http.MethodPost); err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to preserve template snapshot").SetInternal(err)
		}
	} else if remoteSnapshot {
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
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(resp.Status)
	}
	return nil
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
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(resp.Status)
	}
	return nil
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
