package controllers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type adminRunnerInventoryItem struct {
	ID          string            `json:"id"`
	ManagerID   string            `json:"manager_id"`
	ManagerName string            `json:"manager_name,omitempty"`
	Pool        string            `json:"pool,omitempty"`
	FromPool    bool              `json:"from_pool"`
	Status      core.RunnerStatus `json:"status"`
	SessionID   string            `json:"session_id,omitempty"`
	Online      bool              `json:"online"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty"`
	LastSeen    time.Time         `json:"last_seen,omitempty"`
}

type managerRunnerStatus struct {
	RunningRunnerIDs []string `json:"running_runner_ids"`
	UsedRunnerIDs    []string `json:"used_runner_ids"`
}

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
	return c.proxyManagerLogs(ctx, managerID, "", "")
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
	return c.proxyManagerLogs(ctx, runner.ManagerID, runnerID, sessionID)
}

// ListAdminRunners returns the complete runner inventory. Durable runner and
// allocation records describe pooled workloads; live manager status fills in
// direct sessions which do not have a pool record.
func (c *SessionPoolController) ListAdminRunners(ctx echo.Context) error {
	requestCtx := ctx.Request().Context()
	runners, err := c.store.ListRunners(requestCtx, "")
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	allocations, err := c.store.ListAllocations(requestCtx, "")
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	managers, err := c.store.ListManagers(requestCtx)
	if err != nil {
		return sessionRunnerStoreError(err)
	}

	managerNames := make(map[string]string, len(managers))
	items := make(map[string]*adminRunnerInventoryItem, len(runners))
	sessionItems := make(map[string]*adminRunnerInventoryItem, len(allocations))
	for _, manager := range managers {
		managerNames[manager.ID] = manager.Name
	}
	for _, runner := range runners {
		items[runner.ManagerID+"\x00"+runner.ID] = &adminRunnerInventoryItem{
			ID: runner.ID, ManagerID: runner.ManagerID, ManagerName: managerNames[runner.ManagerID],
			Pool: runner.Pool, FromPool: true, Status: runner.Status,
			CreatedAt: runner.CreatedAt, UpdatedAt: runner.UpdatedAt, LastSeen: runner.LastSeen,
		}
	}
	for _, allocation := range allocations {
		if allocation.RunnerID == "" {
			continue
		}
		if item := items[allocation.ManagerID+"\x00"+allocation.RunnerID]; item != nil {
			item.SessionID = allocation.SessionID
			sessionItems[allocation.ManagerID+"\x00"+allocation.SessionID] = item
			continue
		}
		// Older allocation records may not carry ManagerID. Runner IDs are
		// cluster-unique, so retain the association when there is one match.
		for key, item := range items {
			if strings.HasSuffix(key, "\x00"+allocation.RunnerID) {
				item.SessionID = allocation.SessionID
				sessionItems[item.ManagerID+"\x00"+allocation.SessionID] = item
				break
			}
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, manager := range managers {
		manager := manager
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.managerTunnel == nil || !c.managerTunnel.IsConnected(requestCtx, manager.ID) {
				return
			}
			req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, "http://manager/internal/esm-management/status", nil)
			resp, callErr := c.managerTunnel.Do(requestCtx, manager.ID, "", "", req)
			if callErr != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			var status managerRunnerStatus
			if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&status) != nil {
				return
			}
			used := make(map[string]bool, len(status.UsedRunnerIDs))
			for _, id := range status.UsedRunnerIDs {
				used[id] = true
			}
			mu.Lock()
			defer mu.Unlock()
			for _, id := range status.RunningRunnerIDs {
				key := manager.ID + "\x00" + id
				if item := items[key]; item != nil {
					item.Online = true
					continue
				}
				if item := sessionItems[key]; item != nil {
					item.Online = true
					continue
				}
				item := &adminRunnerInventoryItem{ID: id, ManagerID: manager.ID, ManagerName: manager.Name, FromPool: false, Status: core.RunnerIdle, Online: true}
				if used[id] {
					item.Status = core.RunnerRunning
					item.SessionID = id
				}
				items[key] = item
			}
		}()
	}
	wg.Wait()

	result := make([]*adminRunnerInventoryItem, 0, len(items))
	for _, item := range items {
		if !item.Online && item.Status != core.RunnerDraining {
			item.Status = core.RunnerOffline
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ManagerName != result[j].ManagerName {
			return result[i].ManagerName < result[j].ManagerName
		}
		return result[i].ID < result[j].ID
	})
	return ctx.JSON(http.StatusOK, map[string]any{"session_runners": result})
}

func (c *SessionPoolController) GetAdminRunnerLogs(ctx echo.Context) error {
	managerID := strings.TrimSpace(ctx.QueryParam("manager_id"))
	if managerID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "manager_id is required")
	}
	if _, err := c.store.GetManager(ctx.Request().Context(), managerID); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
	return c.proxyManagerLogs(ctx, managerID, ctx.Param("id"), ctx.Param("id"))
}

// DeleteAdminRunner asks the owning manager to delete the complete workload,
// then removes the parent-side allocation, runner record, and route metadata.
// A manager-side 404 is idempotent: stale parent records are still cleaned up.
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
	if c.managerTunnel == nil || !c.managerTunnel.IsConnected(requestCtx, managerID) {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session manager control channel is offline")
	}

	targetURL := "http://manager/internal/esm-management/runners/" + url.PathEscape(runnerID)
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodDelete, targetURL, nil)
	resp, err := c.managerTunnel.Do(requestCtx, managerID, "", "", req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "session manager operation failed").SetInternal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return echo.NewHTTPError(http.StatusBadGateway, "session manager failed to delete runner: "+strings.TrimSpace(string(body)))
	}

	var sessionID string
	runner, runnerErr := c.store.GetRunner(requestCtx, runnerID)
	if runnerErr == nil && runner.ManagerID == managerID {
		allocations, listErr := c.store.ListAllocations(requestCtx, runner.Pool)
		if listErr != nil {
			return sessionRunnerStoreError(listErr)
		}
		for _, allocation := range allocations {
			if allocation.RunnerID == runnerID {
				sessionID = allocation.SessionID
				if err := c.store.DeleteAllocation(requestCtx, allocation.SessionID); err != nil && err != core.ErrNotFound {
					return sessionRunnerStoreError(err)
				}
			}
		}
		if err := c.store.DeleteRunner(requestCtx, runnerID); err != nil && err != core.ErrNotFound {
			return sessionRunnerStoreError(err)
		}
	}

	if c.routes != nil {
		routes, listErr := c.routes.List(requestCtx, "")
		if listErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to list session routes").SetInternal(listErr)
		}
		for _, route := range routes {
			if route.ManagerID == managerID && (route.RemoteSessionID == runnerID || route.SessionID == sessionID) {
				if err := c.routes.Delete(requestCtx, route.SessionID); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete session route").SetInternal(err)
				}
			}
		}
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionPoolController) proxyManagerLogs(ctx echo.Context, managerID, runnerID, sessionID string) error {
	if c.managerTunnel == nil || !c.managerTunnel.IsConnected(ctx.Request().Context(), managerID) {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session manager control channel is offline")
	}
	values := url.Values{}
	tail, _ := strconv.Atoi(ctx.QueryParam("tail"))
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
