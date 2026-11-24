package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

func (c *SessionPoolController) reconcileManagerSessionStatusesInner(ctx context.Context, managerID string, statuses map[string]string) error {
	if c.routes == nil {
		return nil
	}
	routes, err := c.routes.List(ctx, "")
	if err != nil {
		return err
	}
	for _, route := range routes {
		status, ok := statuses[route.RemoteSessionID]
		if !ok || route.ManagerID != managerID || status == "" || route.DeletionRequestID != "" {
			continue
		}
		if status == "stable" {
			status = "active"
		}
		// Direct runtimes report turn completion themselves. The manager can
		// still see a healthy, active Pod while the agent is processing a turn;
		// that coarse heartbeat must not mark running work eligible for TTL cleanup.
		// Runtime status is authoritative even while running, so a heartbeat
		// cannot race completion using an earlier route snapshot. A new turn is
		// reported through RecordRemoteSessionStatus instead.
		// A lagging manager replica may still report startup after the turn has
		// finished; those statuses must not reset the completion/TTL timestamp.
		if route.Transport == portrepos.SessionRouteTransportDirectRuntime &&
			(route.Tags["session_ttl"] != "" || route.Tags["slackbot_id"] != "") &&
			(status == "active" || status == "running" || status == "starting" || status == "creating") {
			continue
		}
		// Suspension is a parent-controlled lifecycle state. A manager may still
		// report the workload's terminal status from a watcher that observed the
		// Pod deletion after suspension completed. Keep the route resumable until
		// an explicit resume changes it to resuming.
		if route.Status == "suspended" && status != "suspended" {
			continue
		}
		// While resume owns the lifecycle transition, stale replicas can still
		// report the deleted pre-resume workload as stopped. Only a live status
		// may complete the transition.
		if route.Status == "resuming" && status != "active" && status != "running" && status != "stable" && status != "resuming" {
			continue
		}
		if route.Transport == portrepos.SessionRouteTransportDirectRuntime &&
			route.Status == "error" && status != "suspended" && status != "stopped" {
			continue
		}
		if route.Status == status {
			continue
		}
		route.Status = status
		route.StatusUpdatedAt = c.now()
		if err := c.routes.Save(ctx, route); err != nil {
			return err
		}
		c.notifyRouteStatus(route)
	}
	return nil
}

func allocationSessionMeta(allocation *core.Allocation) (sessionsettings.SessionMeta, bool) {
	var meta sessionsettings.SessionMeta
	if len(allocation.SessionMeta) > 0 {
		return meta, json.Unmarshal(allocation.SessionMeta, &meta) == nil
	}
	var settings sessionsettings.SessionSettings
	if json.Unmarshal(allocation.ProvisionSettings, &settings) != nil {
		return meta, false
	}
	return settings.Session, true
}

func (c *SessionPoolController) reconcileMissingManagerRunners(ctx context.Context, managerID string, localRunnerIDs []string) error {
	allocations, err := c.listManagerAllocations(ctx, managerID)
	if err != nil {
		return err
	}
	runners, err := c.listManagerRunners(ctx, managerID)
	if err != nil {
		return err
	}
	_, _, err = c.reconcileMissingManagerRunnersWith(ctx, managerID, localRunnerIDs, runners, allocations)
	return err
}

func (c *SessionPoolController) reconcileMissingManagerRunnersWith(ctx context.Context, managerID string, localRunnerIDs []string, runners []*core.Runner, allocations []*core.Allocation) ([]*core.Runner, []*core.Allocation, error) {
	err := telemetry.LoggedOperationErr(ctx, "session_pool.heartbeat.reconcile_missing_runners", func(operationCtx context.Context) error {
		var reconcileErr error
		runners, allocations, reconcileErr = c.reconcileMissingManagerRunnersInner(operationCtx, managerID, localRunnerIDs, runners, allocations)
		return reconcileErr
	}, telemetry.String("session_manager.id", managerID), telemetry.Int64("session_manager.local_runner_count", int64(len(localRunnerIDs))))
	return runners, allocations, err
}

func (c *SessionPoolController) reconcileMissingManagerRunnersInner(ctx context.Context, managerID string, localRunnerIDs []string, runners []*core.Runner, allocations []*core.Allocation) ([]*core.Runner, []*core.Allocation, error) {
	local := make(map[string]bool, len(localRunnerIDs))
	for _, id := range localRunnerIDs {
		local[id] = true
	}
	allocated := make(map[string]bool, len(allocations))
	for _, allocation := range allocations {
		if allocation.RunnerID != "" {
			allocated[allocation.RunnerID] = true
		}
	}
	remainingRunners := make([]*core.Runner, 0, len(runners))
	for _, runner := range runners {
		if runner.ManagerID != managerID || runner.Status != core.RunnerDraining || local[runner.ID] || allocated[runner.ID] {
			remainingRunners = append(remainingRunners, runner)
			continue
		}
		if c.now().Sub(runner.UpdatedAt) < sessionRunnerDrainingRetention {
			remainingRunners = append(remainingRunners, runner)
			continue
		}
		if err := c.store.DeleteRunner(ctx, runner.ID); err != nil && !errors.Is(err, core.ErrNotFound) {
			return nil, nil, err
		}
	}
	remainingAllocations := make([]*core.Allocation, 0, len(allocations))
	for _, allocation := range allocations {
		remainingAllocations = append(remainingAllocations, allocation)
		if allocation.ManagerID != managerID || allocation.RunnerID == "" || local[allocation.RunnerID] {
			continue
		}
		// Do not requeue a running allocation: its runtime may already have
		// performed work. If resume was interrupted after changing the route to
		// resuming, roll back only that stale lifecycle marker. The next runtime
		// access will retry resume with the same allocation and durable workspace.
		if allocation.Status == core.AllocationRunning && c.routes != nil {
			route, e := c.routes.Get(ctx, allocation.SessionID)
			if e != nil {
				return nil, nil, e
			}
			if route != nil && route.Status == "resuming" && !route.StatusUpdatedAt.IsZero() &&
				c.now().Sub(route.StatusUpdatedAt) >= missingResumingRunnerRecoveryDelay {
				log.Printf("[SESSION_RUNNER] Recovering stale resuming session %s after runner %s disappeared from manager %s inventory", allocation.SessionID, allocation.RunnerID, managerID)
				route.Status = "suspended"
				route.StatusUpdatedAt = c.now()
				if e = c.routes.Save(ctx, route); e != nil {
					return nil, nil, e
				}
				c.notifyRouteStatus(route)
			}
			continue
		}
		// A heartbeat is only a snapshot. Preserve in-flight leases and wait
		// through the startup lease before considering an acknowledged but
		// never-connected runtime for recovery.
		if allocation.Status != core.AllocationClaimed || c.now().Before(allocation.LeaseExpiresAt) {
			continue
		}
		// Preserve legacy sessions that reported a status before start fencing existed.
		if c.routes != nil {
			route, e := c.routes.Get(ctx, allocation.SessionID)
			if e != nil {
				return nil, nil, e
			}
			if route != nil && route.Status != "" && route.Status != "starting" && route.Status != "creating" {
				continue
			}
		}
		retried, e := c.store.RequeueUnstarted(ctx, allocation.SessionID, allocation.RunnerID)
		if errors.Is(e, core.ErrConflict) {
			continue
		}
		if errors.Is(e, core.ErrNotFound) {
			remainingAllocations = remainingAllocations[:len(remainingAllocations)-1]
			continue
		}
		if e != nil {
			return nil, nil, e
		}
		remainingAllocations = remainingAllocations[:len(remainingAllocations)-1]
		if c.notifier != nil {
			if e = c.notifier.Notify(ctx, retried.Pool); e != nil {
				return nil, nil, e
			}
		}
	}

	return remainingRunners, remainingAllocations, nil
}

// RetireRunner must succeed before a manager deletes an idle workload.
func (c *SessionPoolController) RetireRunner(ctx echo.Context) error {
	manager, err := c.authenticateManager(ctx)
	if err != nil {
		return err
	}
	if err = c.store.RetireRunner(ctx.Request().Context(), manager.ID, ctx.Param("runnerId")); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionPoolController) authenticateManager(ctx echo.Context) (*core.Manager, error) {
	token := bearerToken(ctx.Request())
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err == nil && verifySessionRunnerToken(manager.ConnectionTokenHash, token) {
		return manager, nil
	}

	// The connection token is the manager credential and remains authoritative
	// when a manager is re-enrolled under a new generated ID. Resolve the current
	// ID from that credential so a stale deployment value cannot permanently
	// break heartbeats and stock reconciliation.
	managers, listErr := c.store.ListManagers(ctx.Request().Context())
	if listErr != nil {
		return nil, sessionRunnerStoreError(listErr)
	}
	for _, candidate := range managers {
		if verifySessionRunnerToken(candidate.ConnectionTokenHash, token) {
			return candidate, nil
		}
	}
	return nil, echo.NewHTTPError(http.StatusUnauthorized, "invalid manager token")
}

func (c *SessionPoolController) authenticateRunner(ctx echo.Context) (*core.Runner, error) {
	runnerID := ctx.Request().Header.Get("X-Session-Runner-ID")
	runner, err := c.store.GetRunner(ctx.Request().Context(), runnerID)
	if err != nil || !verifySessionRunnerToken(runner.TokenHash, bearerToken(ctx.Request())) {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "invalid runner token")
	}
	if err := c.store.TouchRunner(ctx.Request().Context(), runner.ID, c.now()); err != nil {
		return nil, sessionRunnerStoreError(err)
	}
	return runner, nil
}

func (c *SessionPoolController) ensureLogicalPoolExists(ctx context.Context, name string) error {
	_, err := c.store.GetLogicalPool(ctx, name)
	return err
}

func (c *SessionPoolController) prepareClaimRoute(ctx context.Context, allocation *core.Allocation, runner *core.Runner) error {
	if allocation == nil || runner == nil ||
		(len(allocation.AuthorizedManagerIDs) > 0 && !containsString(allocation.AuthorizedManagerIDs, runner.ManagerID)) {
		return core.ErrUnauthorized
	}
	if c.routes == nil {
		return nil
	}
	route, err := c.routes.Get(ctx, allocation.SessionID)
	if err != nil || route == nil {
		return err
	}
	if route.Generation != allocation.Generation {
		// A replacement must not inherit the previous runtime's completion TTL.
		route.Status = "starting"
		route.StatusUpdatedAt = c.now()
	}
	route.ManagerID = runner.ManagerID
	route.RemoteSessionID = runner.ID
	route.Generation = allocation.Generation
	route.RuntimeTokenHash = allocation.RuntimeTokenHash
	return c.routes.Save(ctx, route)
}

func (c *SessionPoolController) repairManagerRoutes(ctx context.Context, managerID string, allocations []*core.Allocation) error {
	if c.routes == nil {
		return nil
	}
	for _, allocation := range allocations {
		if allocation.ManagerID != managerID || allocation.RunnerID == "" ||
			(allocation.Status != core.AllocationLeased && allocation.Status != core.AllocationClaimed && allocation.Status != core.AllocationRunning) {
			continue
		}
		route, err := c.routes.Get(ctx, allocation.SessionID)
		if err != nil {
			return err
		}
		if route == nil || (route.RemoteSessionID != "" && route.Generation >= allocation.Generation) {
			continue
		}
		route.ManagerID = managerID
		route.RemoteSessionID = allocation.RunnerID
		route.Generation = allocation.Generation
		route.RuntimeTokenHash = allocation.RuntimeTokenHash
		if err := c.routes.Save(ctx, route); err != nil {
			return err
		}
	}
	return nil
}

func (c *SessionPoolController) listManagerRunners(ctx context.Context, managerID string) ([]*core.Runner, error) {
	if scoped, ok := c.store.(managerScopedSessionRunnerStore); ok {
		return scoped.ListManagerRunners(ctx, managerID)
	}
	runners, err := c.store.ListRunners(ctx, "")
	if err != nil {
		return nil, err
	}
	result := make([]*core.Runner, 0)
	for _, runner := range runners {
		if runner.ManagerID == managerID {
			result = append(result, runner)
		}
	}
	return result, nil
}

func (c *SessionPoolController) listManagerAllocations(ctx context.Context, managerID string) ([]*core.Allocation, error) {
	if scoped, ok := c.store.(managerScopedSessionRunnerStore); ok {
		return scoped.ListManagerAllocations(ctx, managerID)
	}
	allocations, err := c.store.ListAllocations(ctx, "")
	if err != nil {
		return nil, err
	}
	result := make([]*core.Allocation, 0)
	for _, allocation := range allocations {
		if allocation.ManagerID == managerID {
			result = append(result, allocation)
		}
	}
	return result, nil
}

func runnerClaimResponse(allocation *core.Allocation, runner *core.Runner) map[string]any {
	var settings *sessionsettings.SessionSettings
	if len(allocation.ProvisionSettings) > 0 {
		_ = json.Unmarshal(allocation.ProvisionSettings, &settings)
	}
	if settings != nil {
		settings.ParentRuntime = &sessionsettings.ParentRuntimeConfig{Enabled: true, SessionID: allocation.SessionID, ManagerID: runner.ManagerID, Token: allocation.RuntimeToken, Generation: allocation.Generation}
	}
	copy := *allocation
	copy.RuntimeToken, copy.RuntimeTokenHash, copy.ProvisionSettings = "", "", nil
	return map[string]any{"allocation": &copy, "lease_id": allocation.LeaseID, "runtime_token": allocation.RuntimeToken, "settings": settings}
}

func redactManager(manager *core.Manager) *core.Manager {
	copy := *manager
	copy.ConnectionTokenHash = ""
	copy.RegistrationTokenHash = ""
	return &copy
}
