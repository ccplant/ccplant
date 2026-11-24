package controllers

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/buildinfo"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

func (c *SessionPoolController) ClaimRunnerAllocation(ctx echo.Context) error {
	runner, err := c.authenticateRunner(ctx)
	if err != nil {
		return err
	}
	if err := c.validateRunnerManager(ctx.Request().Context(), runner); err != nil {
		return err
	}
	wait := parseRunnerWait(ctx.QueryParam("wait"))
	notifications, cancel, err := c.subscribeRunnerAllocations(ctx.Request().Context(), runner.Pool, wait)
	if err != nil {
		return err
	}
	defer cancel()
	return c.claimRunnerAllocation(ctx, runner, wait, notifications)
}

func (c *SessionPoolController) validateRunnerManager(ctx context.Context, runner *core.Runner) error {
	manager, err := c.store.GetManager(ctx, runner.ManagerID)
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session manager is unavailable")
	}
	available := core.ManagerAvailable(manager, sessionManagerHeartbeatTTL, c.now())
	if c.liveness != nil {
		available, err = c.liveness.IsManagerConnected(ctx, manager.ID)
	}
	if err != nil || !available {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session manager heartbeat is stale")
	}
	return nil
}

func (c *SessionPoolController) subscribeRunnerAllocations(ctx context.Context, pool string, wait time.Duration) (<-chan struct{}, func(), error) {
	if wait <= 0 || c.notifier == nil {
		return nil, func() {}, nil
	}
	notifications, cancel, err := c.notifier.Subscribe(ctx, pool)
	if err != nil {
		return nil, func() {}, echo.NewHTTPError(http.StatusServiceUnavailable, "allocation notifications are unavailable").SetInternal(err)
	}
	return notifications, cancel, nil
}

func (c *SessionPoolController) claimRunnerAllocation(ctx echo.Context, runner *core.Runner, wait time.Duration, notifications <-chan struct{}) error {
	deadline := c.now().Add(wait)
	for {
		allocation, found, err := c.store.ClaimNext(ctx.Request().Context(), runner.Pool, runner.ID, 45*time.Second)
		if err != nil {
			return sessionRunnerStoreError(err)
		}
		if found {
			return c.respondToRunnerClaim(ctx, runner, allocation)
		}
		if wait <= 0 || !c.now().Before(deadline) {
			return ctx.NoContent(http.StatusNoContent)
		}
		if err := waitForRunnerAllocation(ctx.Request().Context(), deadline.Sub(c.now()), notifications); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return ctx.NoContent(http.StatusNoContent)
			}
			return err
		}
	}
}

func (c *SessionPoolController) respondToRunnerClaim(ctx echo.Context, runner *core.Runner, allocation *core.Allocation) error {
	if err := c.prepareClaimRoute(ctx.Request().Context(), allocation, runner); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, runnerClaimResponse(allocation, runner))
}

func waitForRunnerAllocation(ctx context.Context, remaining time.Duration, notifications <-chan struct{}) error {
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return context.DeadlineExceeded
	case _, ok := <-notifications:
		if !ok {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "allocation notifications are unavailable")
		}
		return nil
	}
}

func (c *SessionPoolController) AckRunnerAllocation(ctx echo.Context) error {
	runner, err := c.authenticateRunner(ctx)
	if err != nil {
		return err
	}
	var input struct {
		LeaseID string `json:"lease_id"`
	}
	if err := ctx.Bind(&input); err != nil || input.LeaseID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "lease_id is required")
	}
	allocation, err := c.store.Acknowledge(ctx.Request().Context(), ctx.Param("sessionId"), runner.ID, input.LeaseID)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, allocation)
}

func (c *SessionPoolController) FailRunnerAllocation(ctx echo.Context) error {
	runner, err := c.authenticateRunner(ctx)
	if err != nil {
		return err
	}
	var input struct {
		LeaseID string `json:"lease_id"`
	}
	if err := ctx.Bind(&input); err != nil || input.LeaseID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "lease_id is required")
	}
	allocation, err := c.store.Fail(ctx.Request().Context(), ctx.Param("sessionId"), runner.ID, input.LeaseID)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, allocation)
}

func (c *SessionPoolController) HeartbeatManager(ctx echo.Context) error {
	return telemetry.LoggedOperationErr(ctx.Request().Context(), "controllers.SessionPoolController.HeartbeatManager", func(requestCtx context.Context) error {
		ctx.SetRequest(ctx.Request().WithContext(requestCtx))
		return c.heartbeatManager(ctx)
	})
}

func (c *SessionPoolController) heartbeatManager(ctx echo.Context) error {
	manager, err := c.authenticateManager(ctx)
	if err != nil {
		return err
	}
	requestCtx := ctx.Request().Context()
	telemetry.SetAttributes(requestCtx, telemetry.String("session_manager.id", manager.ID))
	input, err := bindManagerHeartbeat(ctx)
	if err != nil {
		return err
	}
	data, err := c.loadManagerHeartbeatData(requestCtx, manager.ID, input)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	summary, err := c.summarizeManagerHeartbeat(requestCtx, manager.ID, input, data)
	if err != nil {
		return err
	}
	telemetry.SetAttributes(requestCtx,
		telemetry.Int64("session_pool.heartbeat.pool_count", int64(len(summary.pools))),
		telemetry.Int64("session_pool.heartbeat.runner_count", int64(len(data.runners))),
		telemetry.Int64("session_pool.heartbeat.allocation_count", int64(len(data.allocations))),
		telemetry.Int64("session_pool.heartbeat.registered_runner_count", int64(len(summary.registeredRunnerIDs))),
		telemetry.Int64("session_pool.heartbeat.allocated_runner_count", int64(len(summary.allocatedRunnerIDs))),
		telemetry.Int64("session_pool.heartbeat.orphaned_session_count", int64(len(data.orphanedSessionIDs))),
	)
	if err := c.recordManagerHeartbeat(requestCtx, manager); err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, map[string]any{
		"ok": true, "at": c.now(), "manager_id": manager.ID, "pools": summary.pools,
		"registered_runner_ids":     summary.registeredRunnerIDs,
		"allocated_runner_ids":      summary.allocatedRunnerIDs,
		"allocated_runner_policies": summary.allocatedRunnerPolicies,
		"orphaned_session_ids":      data.orphanedSessionIDs,
		"upstream_version":          buildinfo.Version,
	})
}

type managerHeartbeatInput struct {
	localRunnerIDs  *[]string
	sessionStatuses map[string]string
}

type managerHeartbeatData struct {
	localRunners       map[string]bool
	runners            []*core.Runner
	allocations        []*core.Allocation
	pools              []*core.PoolSupplier
	logicalPools       []*core.LogicalPool
	orphanedSessionIDs []string
}

type managerHeartbeatSummary struct {
	pools                   []*core.PoolSupplier
	registeredRunnerIDs     []string
	allocatedRunnerIDs      []string
	allocatedRunnerPolicies map[string]sessionsettings.SessionMeta
}

func bindManagerHeartbeat(ctx echo.Context) (managerHeartbeatInput, error) {
	var payload struct {
		LocalRunnerIDs  *[]string         `json:"local_runner_ids"`
		SessionStatuses map[string]string `json:"session_statuses"`
	}
	if ctx.Request().Body != nil && ctx.Request().Body != http.NoBody {
		if err := ctx.Bind(&payload); err != nil {
			return managerHeartbeatInput{}, echo.NewHTTPError(http.StatusBadRequest, "invalid heartbeat request")
		}
	}
	return managerHeartbeatInput{localRunnerIDs: payload.LocalRunnerIDs, sessionStatuses: payload.SessionStatuses}, nil
}

func (c *SessionPoolController) loadManagerHeartbeatData(ctx context.Context, managerID string, input managerHeartbeatInput) (managerHeartbeatData, error) {
	data := managerHeartbeatData{localRunners: make(map[string]bool)}
	if input.localRunnerIDs != nil {
		for _, id := range *input.localRunnerIDs {
			data.localRunners[id] = true
		}
	}
	var err error
	data.runners, err = telemetry.LoggedOperation(ctx, "session_pool.heartbeat.list_runners", func(operationCtx context.Context) ([]*core.Runner, error) {
		return c.listManagerRunners(operationCtx, managerID)
	}, telemetry.String("session_manager.id", managerID))
	if err != nil {
		return data, err
	}
	data.allocations, err = telemetry.LoggedOperation(ctx, "session_pool.heartbeat.list_manager_allocations", func(operationCtx context.Context) ([]*core.Allocation, error) {
		return c.listManagerAllocations(operationCtx, managerID)
	}, telemetry.String("session_manager.id", managerID))
	if err != nil {
		return data, err
	}
	if input.localRunnerIDs != nil {
		data.runners, data.allocations, err = c.reconcileMissingManagerRunnersWith(ctx, managerID, *input.localRunnerIDs, data.runners, data.allocations)
		if err != nil {
			return data, err
		}
	}
	if len(input.sessionStatuses) > 0 {
		if err := c.reconcileManagerSessionStatuses(ctx, managerID, input.sessionStatuses); err != nil {
			return data, err
		}
	}
	data.orphanedSessionIDs, err = c.orphanedManagerSessionIDs(ctx, managerID, input.sessionStatuses)
	if err != nil {
		return data, err
	}
	data.pools, err = telemetry.LoggedOperation(ctx, "session_pool.heartbeat.list_pool_suppliers", func(operationCtx context.Context) ([]*core.PoolSupplier, error) {
		return c.store.ListPoolSuppliers(operationCtx)
	}, telemetry.String("session_manager.id", managerID))
	if err != nil {
		return data, err
	}
	data.logicalPools, err = telemetry.LoggedOperation(ctx, "session_pool.heartbeat.list_logical_pools", func(operationCtx context.Context) ([]*core.LogicalPool, error) {
		return c.store.ListLogicalPools(operationCtx)
	}, telemetry.String("session_manager.id", managerID))
	if err != nil {
		return data, err
	}
	if err := telemetry.LoggedOperationErr(ctx, "session_pool.heartbeat.repair_manager_routes", func(operationCtx context.Context) error {
		return c.repairManagerRoutes(operationCtx, managerID, data.allocations)
	}, telemetry.String("session_manager.id", managerID)); err != nil {
		return data, err
	}
	return data, nil
}

func (c *SessionPoolController) summarizeManagerHeartbeat(ctx context.Context, managerID string, input managerHeartbeatInput, data managerHeartbeatData) (managerHeartbeatSummary, error) {
	logicalPoolEnabled := make(map[string]bool, len(data.logicalPools))
	for _, pool := range data.logicalPools {
		logicalPoolEnabled[pool.Name] = pool.Enabled
	}
	runnersByPool := make(map[string][]*core.Runner)
	for _, runner := range data.runners {
		runnersByPool[runner.Pool] = append(runnersByPool[runner.Pool], runner)
	}
	allocationsByPool := make(map[string][]*core.Allocation)
	for _, allocation := range data.allocations {
		allocationsByPool[allocation.Pool] = append(allocationsByPool[allocation.Pool], allocation)
	}
	summary := managerHeartbeatSummary{allocatedRunnerPolicies: make(map[string]sessionsettings.SessionMeta)}
	protected := make(map[string]bool)
	for _, pool := range data.pools {
		if pool.ManagerID != managerID {
			continue
		}
		poolInput := managerPoolInput{ctx: ctx, pool: pool, logicalEnabled: logicalPoolEnabled[pool.Pool], inventoryReported: input.localRunnerIDs != nil, local: data.localRunners, runners: runnersByPool[pool.Pool], allocations: allocationsByPool[pool.Pool]}
		poolSummary, err := c.summarizeManagerPool(poolInput)
		if err != nil {
			return summary, err
		}
		summary.pools = append(summary.pools, poolSummary.pool)
		summary.registeredRunnerIDs = append(summary.registeredRunnerIDs, poolSummary.registered...)
		for id := range poolSummary.protected {
			protected[id] = true
		}
		for id, policy := range poolSummary.policies {
			summary.allocatedRunnerPolicies[id] = policy
		}
	}
	for id := range protected {
		summary.allocatedRunnerIDs = append(summary.allocatedRunnerIDs, id)
	}
	return summary, nil
}

type managerPoolSummary struct {
	pool       *core.PoolSupplier
	registered []string
	protected  map[string]bool
	policies   map[string]sessionsettings.SessionMeta
}

type managerPoolInput struct {
	ctx                               context.Context
	pool                              *core.PoolSupplier
	logicalEnabled, inventoryReported bool
	local                             map[string]bool
	runners                           []*core.Runner
	allocations                       []*core.Allocation
}

func (c *SessionPoolController) summarizeManagerPool(input managerPoolInput) (managerPoolSummary, error) {
	result := managerPoolSummary{pool: &core.PoolSupplier{}, protected: make(map[string]bool), policies: make(map[string]sessionsettings.SessionMeta)}
	*result.pool = *input.pool
	result.pool.Enabled = result.pool.Enabled && input.logicalEnabled
	c.summarizeManagerAllocations(input, &result)
	if err := c.summarizeManagerRunners(input, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *SessionPoolController) summarizeManagerAllocations(input managerPoolInput, result *managerPoolSummary) {
	for _, allocation := range input.allocations {
		if allocation.RunnerID == "" || (allocation.Status != core.AllocationLeased && allocation.Status != core.AllocationClaimed && allocation.Status != core.AllocationRunning) {
			continue
		}
		result.protected[allocation.RunnerID] = true
		if input.inventoryReported && !input.local[allocation.RunnerID] {
			continue
		}
		if meta, ok := allocationSessionMeta(allocation); ok {
			result.policies[allocation.RunnerID] = meta
		}
	}
	return
}

func (c *SessionPoolController) summarizeManagerRunners(input managerPoolInput, result *managerPoolSummary) error {
	for _, runner := range input.runners {
		if err := c.summarizeManagerRunner(input, result, runner); err != nil {
			return err
		}
	}
	return nil
}

func (c *SessionPoolController) summarizeManagerRunner(input managerPoolInput, result *managerPoolSummary, runner *core.Runner) error {
	if runner.Status == core.RunnerIdle && !result.protected[runner.ID] && c.now().Sub(runner.LastSeen) > sessionRunnerHeartbeatTTL {
		err := telemetry.LoggedOperationErr(input.ctx, "session_pool.heartbeat.retire_stale_runner", func(operationCtx context.Context) error {
			return c.store.RetireRunner(operationCtx, input.pool.ManagerID, runner.ID)
		}, telemetry.String("session_manager.id", input.pool.ManagerID), telemetry.String("session_runner.id", runner.ID))
		if err != nil && !errors.Is(err, core.ErrConflict) {
			return sessionRunnerStoreError(err)
		}
		return nil
	}
	if runner.Status == core.RunnerDraining {
		return nil
	}
	result.registered = append(result.registered, runner.ID)
	if runner.Status == core.RunnerRunning || runner.Status == core.RunnerClaiming {
		result.protected[runner.ID] = true
	}
	if input.inventoryReported && !input.local[runner.ID] {
		return nil
	}
	result.pool.TotalRunners++
	if runner.Status == core.RunnerIdle && !result.protected[runner.ID] {
		result.pool.IdleRunners++
	}
	return nil
}

func (c *SessionPoolController) recordManagerHeartbeat(ctx context.Context, manager *core.Manager) error {
	if c.liveness != nil {
		if err := telemetry.LoggedOperationErr(ctx, "session_pool.heartbeat.touch_manager_liveness", func(operationCtx context.Context) error {
			return c.liveness.TouchManager(operationCtx, manager.ID, "runner")
		}, telemetry.String("session_manager.id", manager.ID)); err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "failed to refresh session manager liveness").SetInternal(err)
		}
		return nil
	}
	if err := telemetry.LoggedOperationErr(ctx, "session_pool.heartbeat.update_manager_liveness", func(operationCtx context.Context) error {
		return c.store.TouchManagerHeartbeat(operationCtx, manager.ID, c.now())
	}, telemetry.String("session_manager.id", manager.ID)); err != nil {
		return sessionRunnerStoreError(err)
	}
	return nil
}

// orphanedManagerSessionIDs compares a manager's local workload inventory with
// the parent's durable routes. The response is advisory: the manager performs
// cleanup through its normal idempotent DeleteSession path. If routes cannot be
// read, HeartbeatManager fails instead of risking deletion from an incomplete
// authoritative snapshot.
func (c *SessionPoolController) orphanedManagerSessionIDs(ctx context.Context, managerID string, statuses map[string]string) ([]string, error) {
	return telemetry.LoggedOperation(ctx, "session_pool.heartbeat.find_orphaned_sessions", func(operationCtx context.Context) ([]string, error) {
		return c.findOrphanedManagerSessionIDs(operationCtx, managerID, statuses)
	}, telemetry.String("session_manager.id", managerID), telemetry.Int64("session_manager.reported_session_count", int64(len(statuses))))
}

func (c *SessionPoolController) findOrphanedManagerSessionIDs(ctx context.Context, managerID string, statuses map[string]string) ([]string, error) {
	if c.routes == nil || len(statuses) == 0 {
		return []string{}, nil
	}
	routes, err := c.routes.List(ctx, "")
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(routes)*2)
	for _, route := range routes {
		if route == nil {
			continue
		}
		// Public IDs are globally unique. Preserve a matching route even while
		// its manager assignment is being repaired after a claim or restart.
		known[route.SessionID] = true
		if route.ManagerID == managerID && route.RemoteSessionID != "" {
			known[route.RemoteSessionID] = true
		}
	}
	orphaned := make([]string, 0)
	for id := range statuses {
		if id != "" && !known[id] {
			orphaned = append(orphaned, id)
		}
	}
	sort.Strings(orphaned)
	return orphaned, nil
}

func (c *SessionPoolController) reconcileManagerSessionStatuses(ctx context.Context, managerID string, statuses map[string]string) error {
	return telemetry.LoggedOperationErr(ctx, "session_pool.heartbeat.reconcile_session_statuses", func(operationCtx context.Context) error {
		return c.reconcileManagerSessionStatusesInner(operationCtx, managerID, statuses)
	}, telemetry.String("session_manager.id", managerID), telemetry.Int64("session_manager.reported_session_count", int64(len(statuses))))
}
