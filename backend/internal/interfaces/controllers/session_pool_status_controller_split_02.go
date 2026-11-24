package controllers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (c *SessionPoolController) GetAdminRunnerLogs(ctx echo.Context) error {
	managerID := strings.TrimSpace(ctx.QueryParam("manager_id"))
	if managerID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "manager_id is required")
	}
	manager, err := c.store.GetManager(ctx.Request().Context(), managerID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
	if ctx.QueryParam("scope") == string(core.ManagerScopeSystem) && !isSystemManager(manager) {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
	sessionID := strings.TrimSpace(ctx.QueryParam("session_id"))
	if sessionID == "" {
		sessionID = ctx.Param("id")
	}
	return c.proxyManagerLogs(ctx, managerID, ctx.Param("id"), sessionID)
}

// GetAdminManagerLogs returns operational logs for any registered session
// manager. Unlike the pool-scoped endpoint, this is restricted to admins and
// does not require the caller to have a manage binding for one of its pools.
func (c *SessionPoolController) GetAdminManagerLogs(ctx echo.Context) error {
	managerID := strings.TrimSpace(ctx.Param("id"))
	manager, err := c.store.GetManager(ctx.Request().Context(), managerID)
	if err != nil || !isSystemManager(manager) {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
	return c.proxyManagerLogs(ctx, managerID, "", "")
}

func isSystemManager(manager *core.Manager) bool {
	// Managers created before scope was introduced have an empty scope and were
	// control-plane (system) managers.
	return manager.Scope == "" || manager.Scope == core.ManagerScopeSystem
}

// DeleteAdminRunner asks the owning manager to delete the complete workload,
// then removes the parent-side allocation, runner record, and route metadata.
// A manager-side 404 is idempotent: stale parent records are still cleaned up.
// When an offline manager is permanently gone, force=true skips the unreachable
// manager call and removes only its stale parent-side metadata.
func (c *SessionPoolController) DeleteAdminRunner(ctx echo.Context) error {
	requestCtx := ctx.Request().Context()
	runnerID := strings.TrimSpace(ctx.Param("id"))
	managerID := strings.TrimSpace(ctx.QueryParam("manager_id"))
	if runnerID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "runner id is required")
	}
	if managerID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "manager_id is required")
	}
	if _, err := c.store.GetManager(requestCtx, managerID); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
	managerConnected := c.managerTunnel != nil && c.managerTunnel.IsConnected(requestCtx, managerID)
	force := strings.EqualFold(strings.TrimSpace(ctx.QueryParam("force")), "true")
	if !managerConnected && !force {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session manager control channel is offline")
	}

	if managerConnected {
		if err := c.deleteManagerRunner(requestCtx, managerID, runnerID); err != nil {
			return err
		}
	}
	sessionIDs, err := c.deleteRunnerRecords(requestCtx, managerID, runnerID)
	if err != nil {
		return err
	}
	if err := c.deleteRunnerRoutes(requestCtx, managerID, runnerID, sessionIDs); err != nil {
		return err
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionPoolController) deleteManagerRunner(ctx context.Context, managerID, runnerID string) error {
	targetURL := "http://manager/internal/esm-management/runners/" + url.PathEscape(runnerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, targetURL, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to build session manager request").SetInternal(err)
	}
	resp, err := c.managerTunnel.Do(ctx, managerID, "", "", req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "session manager operation failed").SetInternal(err)
	}
	defer closeutils.SafeCloseResponseBody(resp.Body)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if readErr != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "failed to read session manager response").SetInternal(readErr)
	}
	return echo.NewHTTPError(http.StatusBadGateway, "session manager failed to delete runner: "+strings.TrimSpace(string(body)))
}

func (c *SessionPoolController) deleteRunnerRecords(ctx context.Context, managerID, runnerID string) (map[string]bool, error) {
	runner, runnerErr := c.store.GetRunner(ctx, runnerID)
	runnerOwned := runnerErr == nil && runner.ManagerID == managerID
	if !runnerOwned && !errors.Is(runnerErr, core.ErrNotFound) {
		return map[string]bool{}, nil
	}
	target := runnerAllocationDeleteTarget{managerID: managerID, runnerID: runnerID, runnerOwned: runnerOwned}
	if runnerOwned {
		target.pool = runner.Pool
	}
	sessionIDs, err := c.deleteRunnerAllocations(ctx, target)
	if err != nil {
		return nil, err
	}
	if runnerOwned {
		if err := c.store.DeleteRunner(ctx, runnerID); err != nil && err != core.ErrNotFound {
			return nil, sessionRunnerStoreError(err)
		}
	}
	return sessionIDs, nil
}

type runnerAllocationDeleteTarget struct {
	managerID   string
	runnerID    string
	pool        string
	runnerOwned bool
}

func (c *SessionPoolController) deleteRunnerAllocations(ctx context.Context, target runnerAllocationDeleteTarget) (map[string]bool, error) {
	allocations, err := c.store.ListAllocations(ctx, target.pool)
	if err != nil {
		return nil, sessionRunnerStoreError(err)
	}
	sessionIDs := make(map[string]bool)
	for _, allocation := range allocations {
		if allocation.RunnerID != target.runnerID || (!target.runnerOwned && allocation.ManagerID != target.managerID) {
			continue
		}
		sessionIDs[allocation.SessionID] = true
		if err := c.store.DeleteAllocation(ctx, allocation.SessionID); err != nil && err != core.ErrNotFound {
			return nil, sessionRunnerStoreError(err)
		}
	}
	return sessionIDs, nil
}

func (c *SessionPoolController) deleteRunnerRoutes(ctx context.Context, managerID, runnerID string, sessionIDs map[string]bool) error {
	if c.routes == nil {
		return nil
	}
	routes, err := c.routes.List(ctx, "")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list session routes").SetInternal(err)
	}
	configurations, canDeleteConfigurations := c.store.(sessionConfigurationDeleter)
	for _, route := range routes {
		if route.ManagerID != managerID || (route.RemoteSessionID != runnerID && !sessionIDs[route.SessionID]) {
			continue
		}
		if err := c.routes.Delete(ctx, route.SessionID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete session route").SetInternal(err)
		}
		if canDeleteConfigurations && configurations != nil {
			if err := configurations.DeleteConfiguration(ctx, route.SessionID); err != nil {
				log.Printf("[SESSION_RUNNER] Failed to delete configuration for session %s: %v", route.SessionID, err)
			}
		}
	}
	return nil
}

func (c *SessionPoolController) proxyManagerLogs(ctx echo.Context, managerID, runnerID, sessionID string) error {
	if c.managerTunnel == nil || !c.managerTunnel.IsConnected(ctx.Request().Context(), managerID) {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session manager control channel is offline")
	}
	values := url.Values{}
	tail, parseErr := strconv.Atoi(ctx.QueryParam("tail"))
	if parseErr != nil {
		tail = 200
	}
	if tail < 1 || tail > 5000 {
		tail = 200
	}
	values.Set("tail", strconv.Itoa(tail))
	if runnerID != "" {
		values.Set("runner_id", runnerID)
	}
	if sessionID != "" {
		values.Set("session_id", sessionID)
	}
	req, requestErr := http.NewRequestWithContext(ctx.Request().Context(), http.MethodGet, "http://manager/internal/esm-management/logs?"+values.Encode(), nil)
	if requestErr != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to build session manager log request").SetInternal(requestErr)
	}
	resp, err := c.managerTunnel.Do(ctx.Request().Context(), managerID, "", "", req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "session manager operation failed").SetInternal(err)
	}
	defer closeutils.SafeCloseResponseBody(resp.Body)
	for key, values := range resp.Header {
		for _, value := range values {
			ctx.Response().Header().Add(key, value)
		}
	}
	ctx.Response().WriteHeader(resp.StatusCode)
	written, copyErr := io.Copy(ctx.Response(), resp.Body)
	if copyErr != nil {
		return fmt.Errorf("copy session manager logs after %d bytes: %w", written, copyErr)
	}
	return nil
}

func (c *SessionPoolController) requireManageableManager(ctx echo.Context, managerID string) error {
	suppliers, err := c.store.ListPoolSuppliers(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, supplier := range suppliers {
		if supplier.ManagerID == managerID {
			if ok, checkErr := c.canManagePool(ctx.Request().Context(), auth.GetUserFromContext(ctx), supplier.Pool); checkErr != nil {
				return sessionRunnerStoreError(checkErr)
			} else if ok {
				return nil
			}
		}
	}
	return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
}
