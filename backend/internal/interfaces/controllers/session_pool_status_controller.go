package controllers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

type adminRunnerInventoryItem struct {
	ID                  string             `json:"id"`
	ManagerID           string             `json:"manager_id"`
	ManagerName         string             `json:"manager_name,omitempty"`
	Pool                string             `json:"pool,omitempty"`
	FromPool            bool               `json:"from_pool"`
	Status              core.RunnerStatus  `json:"status"`
	SessionID           string             `json:"session_id,omitempty"`
	Online              bool               `json:"online"`
	Connectivity        runnerConnectivity `json:"connectivity"`
	WorkloadState       string             `json:"workload_state"`
	ManagerOnline       bool               `json:"manager_online"`
	ManagerConnectivity string             `json:"manager_connectivity"`
	CreatedAt           time.Time          `json:"created_at,omitempty"`
	UpdatedAt           time.Time          `json:"updated_at,omitempty"`
	LastSeen            time.Time          `json:"last_seen,omitempty"`
}

type runnerConnectivity struct {
	State      string     `json:"state"`
	Source     string     `json:"source,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}

const (
	runnerConnectivityOnline  = "online"
	runnerConnectivityOffline = "offline"
	runnerConnectivityUnknown = "unknown"
	workloadPresent           = "present"
	workloadMissing           = "missing"
	workloadUnknown           = "unknown"
)

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
			defer closeutils.SafeCloseResponseBody(resp.Body)
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
	user := auth.GetUserFromContext(ctx)
	if user == nil || !user.IsAdmin() {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
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
	if ctx.QueryParam("scope") == string(core.ManagerScopeSystem) {
		systemManagers := managers[:0]
		for _, manager := range managers {
			if isSystemManager(manager) {
				systemManagers = append(systemManagers, manager)
			}
		}
		managers = systemManagers
	}

	managerNames := make(map[string]string, len(managers))
	visibleManagers := make(map[string]bool, len(managers))
	managerConnectivity := make(map[string]string, len(managers))
	items := make(map[string]*adminRunnerInventoryItem, len(runners))
	sessionItems := make(map[string]*adminRunnerInventoryItem, len(allocations))
	draining := make(map[string]bool)
	for _, manager := range managers {
		managerNames[manager.ID] = manager.Name
		visibleManagers[manager.ID] = true
		managerConnectivity[manager.ID] = c.connectionState(requestCtx, manager.ID)
	}
	for _, runner := range runners {
		if !visibleManagers[runner.ManagerID] {
			continue
		}
		// Draining runners are deletion tombstones, not usable inventory. They
		// remain persisted briefly for fencing but should not appear as runners
		// that an administrator can inspect or operate.
		if runner.Status == core.RunnerDraining {
			draining[runner.ManagerID+"\x00"+runner.ID] = true
			continue
		}
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
	managerStatusKnown := make(map[string]bool, len(managers))
	for _, manager := range managers {
		manager := manager
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.managerTunnel == nil || managerConnectivity[manager.ID] != runnerConnectivityOnline {
				return
			}
			req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, "http://manager/internal/esm-management/status", nil)
			resp, callErr := c.managerTunnel.Do(requestCtx, manager.ID, "", "", req)
			if callErr != nil {
				return
			}
			defer closeutils.SafeCloseResponseBody(resp.Body)
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
			managerStatusKnown[manager.ID] = true
			for _, id := range status.RunningRunnerIDs {
				key := manager.ID + "\x00" + id
				if draining[key] {
					continue
				}
				if item := items[key]; item != nil {
					item.WorkloadState = workloadPresent
					continue
				}
				if item := sessionItems[key]; item != nil {
					item.WorkloadState = workloadPresent
					continue
				}
				item := &adminRunnerInventoryItem{ID: id, ManagerID: manager.ID, ManagerName: manager.Name, FromPool: false, Status: core.RunnerIdle, WorkloadState: workloadPresent}
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
		item.ManagerConnectivity = managerConnectivity[item.ManagerID]
		item.ManagerOnline = item.ManagerConnectivity == runnerConnectivityOnline
		if item.WorkloadState == "" {
			item.WorkloadState = workloadUnknown
			if managerStatusKnown[item.ManagerID] {
				item.WorkloadState = workloadMissing
			}
		}
		item.Connectivity = c.runnerConnectivity(requestCtx, item)
		item.Online = item.Connectivity.State == runnerConnectivityOnline
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

func (c *SessionPoolController) connectionState(ctx context.Context, id string) string {
	if c.liveness != nil {
		connected, err := c.liveness.IsManagerConnected(ctx, id)
		if err != nil {
			return runnerConnectivityUnknown
		}
		if connected {
			return runnerConnectivityOnline
		}
		return runnerConnectivityOffline
	}
	if c.managerTunnel == nil {
		return runnerConnectivityUnknown
	}
	if c.managerTunnel.IsConnected(ctx, id) {
		return runnerConnectivityOnline
	}
	return runnerConnectivityOffline
}

func (c *SessionPoolController) runnerConnectivity(ctx context.Context, item *adminRunnerInventoryItem) runnerConnectivity {
	if item == nil {
		return runnerConnectivity{State: runnerConnectivityUnknown}
	}
	if item.Status == core.RunnerRunning {
		if item.SessionID == "" {
			return runnerConnectivity{State: runnerConnectivityUnknown, Source: "direct_runtime"}
		}
		return runnerConnectivity{State: c.connectionState(ctx, item.SessionID), Source: "direct_runtime"}
	}
	if item.LastSeen.IsZero() {
		return runnerConnectivity{State: runnerConnectivityUnknown, Source: "runner_api"}
	}
	observed := item.LastSeen
	if c.now().Sub(observed) <= sessionManagerHeartbeatTTL {
		return runnerConnectivity{State: runnerConnectivityOnline, Source: "runner_api", ObservedAt: &observed}
	}
	return runnerConnectivity{State: runnerConnectivityOffline, Source: "runner_api", ObservedAt: &observed}
}
