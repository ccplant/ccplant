package controllers

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func (pc *ProvisionerController) GetNextSessionAllocation(c echo.Context) error {
	if !pc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	wait := parseWait(c.QueryParam("wait"))
	req, ok, err := pc.allocationQueue.NextSessionAllocation(c.Request().Context(), wait)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if !ok {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, req)
}

func (pc *ProvisionerController) CompleteSessionAllocation(c echo.Context) error {
	if !pc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var result sessionallocation.AllocationResult
	if err := c.Bind(&result); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if result.Status == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "status is required"})
	}
	allocation, err := pc.allocationQueue.CompleteSessionAllocation(c.Request().Context(), c.Param("sessionId"), result)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if result.Status == sessionallocation.StatusAssigned && result.AllocatedSessionID != "" && result.AllocatedSessionID != allocation.SessionID && pc.sessionRouteRepo != nil {
		route := &repositories.SessionRoute{
			SessionID:       allocation.SessionID,
			RemoteSessionID: result.AllocatedSessionID,
			StartedAt:       time.Now(),
			Status:          "starting",
		}
		if allocation.Request != nil {
			route.UserID = allocation.Request.UserID
			route.Scope = string(allocation.Request.Scope)
			route.TeamID = allocation.Request.TeamID
			route.Tags = allocation.Request.Tags
			route.InitialMessage = allocation.Request.InitialMessage
		}
		if err := pc.sessionRouteRepo.Save(c.Request().Context(), route); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (pc *ProvisionerController) GetNextExternalSessionAllocation(c echo.Context) error {
	managerID, _, ok := pc.authorizedExternalManager(c)
	if !ok {
		return c.NoContent(http.StatusUnauthorized)
	}
	if snapshot, snapshotErr := pc.externalRuntimeProfileSnapshot(); snapshotErr == nil {
		c.Response().Header().Set("X-AgentAPI-Runtime-Profile-Revision", snapshot.Revision)
		if _, present := c.QueryParams()["profile_revision"]; present && c.QueryParam("profile_revision") != snapshot.Revision {
			return c.NoContent(http.StatusNoContent)
		}
	}
	wait := parseWait(c.QueryParam("wait"))
	req, found, err := pc.allocationQueue.NextExternalSessionAllocation(c.Request().Context(), managerID, wait)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if !found {
		return c.NoContent(http.StatusNoContent)
	}
	if c.QueryParam("metadata_only") == "true" {
		req = allocationMetadata(req)
	}
	return c.JSON(http.StatusOK, req)
}

func (pc *ProvisionerController) GetExternalSessionManagerRuntimeProfile(c echo.Context) error {
	if _, _, ok := pc.authorizedExternalManager(c); !ok {
		return c.NoContent(http.StatusUnauthorized)
	}
	snapshot, err := pc.externalRuntimeProfileSnapshot()
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, snapshot)
}

func allocationMetadata(req *sessionallocation.AllocationRequest) *sessionallocation.AllocationRequest {
	if req == nil {
		return nil
	}
	copyReq := *req
	copyReq.ProvisionSettings = nil
	if req.Request != nil {
		copyReq.Request = &entities.RunServerRequest{
			UserID:    req.Request.UserID,
			Tags:      req.Request.Tags,
			Teams:     req.Request.Teams,
			Scope:     req.Request.Scope,
			TeamID:    req.Request.TeamID,
			AgentType: req.Request.AgentType,
		}
	}
	return &copyReq
}

func (pc *ProvisionerController) CompleteExternalSessionAllocation(c echo.Context) error {
	managerID, managerSecret, ok := pc.authorizedExternalManager(c)
	if !ok {
		return c.NoContent(http.StatusUnauthorized)
	}
	var result sessionallocation.AllocationResult
	if err := c.Bind(&result); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if result.Status == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "status is required"})
	}
	if result.Status == sessionallocation.StatusAssigned && result.AllocatedSessionID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "allocated_session_id is required when status is assigned"})
	}
	allocation, err := pc.allocationQueue.CompleteExternalSessionAllocation(c.Request().Context(), c.Param("sessionId"), result)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if result.Status != sessionallocation.StatusAssigned {
		if pc.sessionRouteRepo != nil {
			_ = pc.sessionRouteRepo.Delete(c.Request().Context(), allocation.SessionID)
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
	if pc.sessionRouteRepo != nil {
		route, err := pc.sessionRouteRepo.Get(c.Request().Context(), allocation.SessionID)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if route == nil {
			route = &repositories.SessionRoute{
				SessionID:  allocation.SessionID,
				StartedAt:  time.Now(),
				Status:     "starting",
				HMACSecret: managerSecret,
				ManagerID:  managerID,
			}
		}
		route.ManagerID = managerID
		route.RemoteSessionID = result.AllocatedSessionID
		if route.HMACSecret == "" {
			route.HMACSecret = managerSecret
		}
		if allocation.Request != nil {
			route.UserID = allocation.Request.UserID
			route.Scope = string(allocation.Request.Scope)
			route.TeamID = allocation.Request.TeamID
			route.Tags = allocation.Request.Tags
			route.InitialMessage = allocation.Request.InitialMessage
		}
		if err := pc.sessionRouteRepo.Save(c.Request().Context(), route); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (pc *ProvisionerController) authorized(c echo.Context) bool {
	if pc.manager == nil {
		return false
	}
	h := c.Request().Header.Get("Authorization")
	token := strings.TrimPrefix(h, "Bearer ")
	return pc.manager.ValidateProvisionerToken(token)
}

func (pc *ProvisionerController) authorizedSession(c echo.Context, sessionID string) bool {
	if pc.manager == nil || sessionID == "" {
		return false
	}
	header := c.Request().Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	return ok && token != "" && pc.manager.ValidateSessionControlToken(sessionID, token)
}

func (pc *ProvisionerController) authorizedExternalManager(c echo.Context) (string, string, bool) {
	if pc.settingsRepo == nil {
		return "", "", false
	}
	token := c.Request().Header.Get("X-Session-Manager-Token")
	if token == "" {
		h := c.Request().Header.Get("Authorization")
		token = strings.TrimPrefix(h, "Bearer ")
		if token == "" || token == h {
			return "", "", false
		}
	}
	settingsList, err := pc.settingsRepo.List(c.Request().Context())
	if err != nil {
		return "", "", false
	}
	for _, settings := range settingsList {
		for _, manager := range settings.ExternalSessionManagers() {
			if manager.HMACSecret != "" && subtle.ConstantTimeCompare([]byte(manager.HMACSecret), []byte(token)) == 1 {
				return manager.ID, manager.HMACSecret, true
			}
		}
	}
	return "", "", false
}

func parseWait(raw string) time.Duration {
	if raw == "" {
		return 0
	}
	if d, err := time.ParseDuration(raw); err == nil {
		if d > 30*time.Second {
			return 30 * time.Second
		}
		return d
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		d := time.Duration(n) * time.Second
		if d > 30*time.Second {
			return 30 * time.Second
		}
		return d
	}
	return 0
}
