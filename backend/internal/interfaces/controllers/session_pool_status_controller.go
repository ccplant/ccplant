package controllers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type managerOperationalStatus struct {
	Manager *core.Manager   `json:"manager"`
	Pools   []string        `json:"pools"`
	Online  bool            `json:"online"`
	Status  json.RawMessage `json:"status,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// ListManageablePoolStatus returns only pools for which the caller has a
// manage binding and managers which supply those pools. Manager status is
// deliberately fetched live over the outbound control tunnel on every call.
func (c *SessionPoolController) ListManageablePoolStatus(ctx echo.Context) error {
	requestCtx := ctx.Request().Context()
	pools, err := c.store.ListLogicalPools(requestCtx)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	user := auth.GetUserFromContext(ctx)
	manageable := make(map[string]bool)
	visiblePools := make([]*core.LogicalPool, 0)
	for _, pool := range pools {
		ok, checkErr := c.canManagePool(requestCtx, user, pool.Name)
		if checkErr != nil {
			return sessionRunnerStoreError(checkErr)
		}
		if ok {
			manageable[pool.Name] = true
			visiblePools = append(visiblePools, pool)
		}
	}
	suppliers, err := c.store.ListPoolSuppliers(requestCtx)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	managerPools := map[string][]string{}
	for _, supplier := range suppliers {
		if manageable[supplier.Pool] {
			managerPools[supplier.ManagerID] = append(managerPools[supplier.ManagerID], supplier.Pool)
		}
	}
	managers, err := c.store.ListManagers(requestCtx)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	statuses := make([]managerOperationalStatus, 0, len(managerPools))
	for _, manager := range managers {
		if ownedPools := managerPools[manager.ID]; len(ownedPools) > 0 {
			statuses = append(statuses, managerOperationalStatus{Manager: redactManager(manager), Pools: ownedPools})
		}
	}
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Add(1)
		go func(status *managerOperationalStatus) {
			defer wg.Done()
			if c.managerTunnel == nil || !c.managerTunnel.IsConnected(requestCtx, status.Manager.ID) {
				status.Error = "session manager control channel is offline"
				return
			}
			query := url.Values{}
			for _, pool := range status.Pools {
				query.Add("pool", pool)
			}
			req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, "http://manager/internal/esm-management/status?"+query.Encode(), nil)
			resp, callErr := c.managerTunnel.Do(requestCtx, status.Manager.ID, "", "", req)
			if callErr != nil {
				status.Error = callErr.Error()
				return
			}
			defer resp.Body.Close()
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			if readErr != nil {
				status.Error = readErr.Error()
				return
			}
			if resp.StatusCode != http.StatusOK {
				status.Error = "session manager returned " + resp.Status
				return
			}
			status.Online, status.Status = true, json.RawMessage(body)
		}(&statuses[i])
	}
	wg.Wait()
	return ctx.JSON(http.StatusOK, map[string]any{"session_pools": visiblePools, "session_managers": statuses})
}

func (c *SessionPoolController) GetManagerLogs(ctx echo.Context) error {
	managerID := ctx.Param("id")
	if err := c.requireManageableManager(ctx, managerID); err != nil {
		return err
	}
	return c.proxyManagerLogs(ctx, managerID, "")
}

func (c *SessionPoolController) GetRunnerLogs(ctx echo.Context) error {
	runnerID := ctx.Param("id")
	runner, err := c.store.GetRunner(ctx.Request().Context(), runnerID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "runner not found")
	}
	if ok, checkErr := c.canManagePool(ctx.Request().Context(), auth.GetUserFromContext(ctx), runner.Pool); checkErr != nil {
		return sessionRunnerStoreError(checkErr)
	} else if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "runner not found")
	}
	sessionID := runnerID
	allocations, listErr := c.store.ListAllocations(ctx.Request().Context(), runner.Pool)
	if listErr != nil {
		return sessionRunnerStoreError(listErr)
	}
	for _, allocation := range allocations {
		if allocation.RunnerID == runnerID {
			sessionID = allocation.SessionID
			break
		}
	}
	return c.proxyManagerLogs(ctx, runner.ManagerID, sessionID)
}

func (c *SessionPoolController) proxyManagerLogs(ctx echo.Context, managerID, sessionID string) error {
	if c.managerTunnel == nil || !c.managerTunnel.IsConnected(ctx.Request().Context(), managerID) {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session manager control channel is offline")
	}
	values := url.Values{}
	tail, _ := strconv.Atoi(ctx.QueryParam("tail"))
	if tail < 1 || tail > 5000 {
		tail = 200
	}
	values.Set("tail", strconv.Itoa(tail))
	if sessionID != "" {
		values.Set("session_id", sessionID)
	}
	req, _ := http.NewRequestWithContext(ctx.Request().Context(), http.MethodGet, "http://manager/internal/esm-management/logs?"+values.Encode(), nil)
	resp, err := c.managerTunnel.Do(ctx.Request().Context(), managerID, "", "", req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "session manager operation failed").SetInternal(err)
	}
	defer resp.Body.Close()
	for key, values := range resp.Header {
		for _, value := range values {
			ctx.Response().Header().Add(key, value)
		}
	}
	ctx.Response().WriteHeader(resp.StatusCode)
	_, err = io.Copy(ctx.Response(), resp.Body)
	return err
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
