package controllers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func (c *SessionController) finalizeRouteLocally(ctx context.Context, route *repositories.SessionRoute, usageDimensions *repositories.SessionUsageDimensions, deletePath string) error {
	c.recordSessionStatusUsage(ctx, repositories.SessionStatusEvent{SessionID: route.SessionID, Status: "terminating", Timestamp: time.Now()})
	if c.sessionRouteRepo != nil {
		if err := c.sessionRouteRepo.Delete(ctx, route.SessionID); err != nil {
			return err
		}
	}
	c.recordSessionTerminated(ctx, usageDimensions, deletePath, time.Now())
	c.cleanupRemoteProvisionRequest(ctx, route.SessionID)
	c.revokeGitHubBrokerLeases(ctx, route.SessionID)
	c.cleanupSessionConfiguration(ctx, route.SessionID)
	return nil
}

var reconcileEcho = echo.New()

// deleteSessionAsWorker runs the durable DELETE path with worker authorization,
// exactly as WorkerControlController does for TTL cleanup.
func (c *SessionController) deleteSessionAsWorker(ctx context.Context, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(sessionID), nil)
	if err != nil {
		return err
	}
	rec := &reconcileResponseRecorder{header: http.Header{}}
	ectx := reconcileEcho.NewContext(req, rec)
	ectx.SetParamNames("sessionId")
	ectx.SetParamValues(sessionID)
	ectx.Set(workerAuthorizedDeleteContextKey, true)
	if err := c.DeleteSession(ectx); err != nil {
		return err
	}
	if rec.status >= http.StatusMultipleChoices {
		return fmt.Errorf("delete returned status %d", rec.status)
	}
	return nil
}

type reconcileResponseRecorder struct {
	header http.Header
	status int
}

func (r *reconcileResponseRecorder) Header() http.Header { return r.header }

func (r *reconcileResponseRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}

func (r *reconcileResponseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return len(b), nil
}

// allocationsBySession returns every pool allocation keyed by public session
// ID, or nil when they cannot be listed completely.
func (c *SessionController) allocationsBySession(ctx context.Context) map[string]*sessionrunnercore.Allocation {
	lister, ok := c.sessionRunnerStore.(sessionAllocationLister)
	if !ok {
		return nil
	}
	allocations, err := lister.ListAllocations(ctx, "")
	if err != nil {
		log.Printf("[ROUTE_RECONCILE] Failed to list allocations: %v", err)
		return nil
	}
	bySession := make(map[string]*sessionrunnercore.Allocation, len(allocations))
	for _, allocation := range allocations {
		if allocation != nil && allocation.SessionID != "" {
			bySession[allocation.SessionID] = allocation
		}
	}
	return bySession
}

type routeFactsLookup struct {
	controller  *SessionController
	owners      routeOwnerLookup
	allocations map[string]*sessionrunnercore.Allocation
	managers    map[string]bool // manager ID -> definitively missing
	runners     map[string]bool // runner ID -> definitively missing
}

func (l *routeFactsLookup) facts(ctx context.Context, route *repositories.SessionRoute) routeReconcileFacts {
	if l.owners == nil || route.ManagerID == "" {
		return routeReconcileFacts{}
	}
	missing, cached := l.managers[route.ManagerID]
	if !cached {
		_, err := l.owners.GetManager(ctx, route.ManagerID)
		missing = errors.Is(err, sessionrunnercore.ErrNotFound)
		if err != nil && !missing {
			// Unknown is not missing: keep the route and retry next pass.
			log.Printf("[ROUTE_RECONCILE] Failed to read manager %s: %v", route.ManagerID, err)
		}
		l.managers[route.ManagerID] = missing
	}
	return routeReconcileFacts{managerMissing: missing, runnerMissing: l.runnerMissing(ctx, route)}
}

// runnerID resolves the runner executing a routed session. The allocation is
// authoritative; once it is gone a direct runtime route still names its runner,
// because for that transport the remote session ID is the runner workload ID.
// That fallback matters: deleting a runner from the admin API removes both the
// runner and its allocation, which previously left the route with no owner.
func (l *routeFactsLookup) runnerID(ctx context.Context, route *repositories.SessionRoute) (string, bool) {
	var allocation *sessionrunnercore.Allocation
	if l.allocations != nil {
		allocation = l.allocations[route.SessionID]
	} else if l.controller.sessionRunnerStore != nil {
		found, err := l.controller.sessionRunnerStore.GetAllocation(ctx, route.SessionID)
		switch {
		case err == nil:
			allocation = found
		case !errors.Is(err, sessionrunnercore.ErrNotFound):
			return "", false
		}
	} else {
		return "", false
	}
	if allocation != nil {
		return allocation.RunnerID, allocation.RunnerID != ""
	}
	if route.Transport == repositories.SessionRouteTransportDirectRuntime && route.RemoteSessionID != "" {
		return route.RemoteSessionID, true
	}
	return "", false
}

func (l *routeFactsLookup) runnerMissing(ctx context.Context, route *repositories.SessionRoute) bool {
	runnerID, ok := l.runnerID(ctx, route)
	if !ok {
		return false
	}
	missing, cached := l.runners[runnerID]
	if !cached {
		_, err := l.owners.GetRunner(ctx, runnerID)
		missing = errors.Is(err, sessionrunnercore.ErrNotFound)
		if err != nil && !missing {
			log.Printf("[ROUTE_RECONCILE] Failed to read runner %s: %v", runnerID, err)
		}
		l.runners[runnerID] = missing
	}
	return missing
}

// reconcileOrphanedConfigurations deletes saved configurations that belong to
// no route, allocation or local session. Configurations bound to a Kubernetes
// owner are left to the garbage collector. It only runs on a complete view of
// routes and allocations, because a partial view would make live sessions look
// orphaned.
func (c *SessionController) reconcileOrphanedConfigurations(ctx context.Context, routes []*repositories.SessionRoute, allocations map[string]*sessionrunnercore.Allocation, now time.Time, opts SessionRouteReconcileOptions) int {
	lister, ok := c.sessionRunnerStore.(configurationRefLister)
	if !ok || allocations == nil {
		return 0
	}
	deleter, ok := c.sessionRunnerStore.(sessionConfigurationDeleter)
	if !ok {
		return 0
	}
	refs, err := lister.ListConfigurationRefs(ctx)
	if err != nil {
		log.Printf("[ROUTE_RECONCILE] Failed to list configurations: %v", err)
		return 0
	}
	known := make(map[string]bool, len(routes)*2+len(allocations))
	for _, route := range routes {
		if route == nil {
			continue
		}
		known[route.SessionID] = true
		if route.RemoteSessionID != "" {
			known[route.RemoteSessionID] = true
		}
	}
	for sessionID := range allocations {
		known[sessionID] = true
	}
	if manager := c.getSessionManager(); manager != nil {
		for _, session := range manager.ListSessions(entities.SessionFilter{}) {
			known[session.ID()] = true
		}
	}
	deleted := 0
	for _, ref := range refs {
		if ref.HasOwner || known[ref.SessionID] || ref.UpdatedAt.IsZero() || now.Sub(ref.UpdatedAt) < opts.ConfigurationOrphanGrace {
			continue
		}
		if opts.DryRun {
			log.Printf("[ROUTE_RECONCILE] [DRY-RUN] Would delete orphaned configuration for session %s (updated %s)", ref.SessionID, ref.UpdatedAt.Format(time.RFC3339))
			deleted++
			continue
		}
		if err := deleter.DeleteConfiguration(ctx, ref.SessionID); err != nil {
			log.Printf("[ROUTE_RECONCILE] Failed to delete orphaned configuration for session %s: %v", ref.SessionID, err)
			continue
		}
		log.Printf("[ROUTE_RECONCILE] Deleted orphaned configuration for session %s (updated %s)", ref.SessionID, ref.UpdatedAt.Format(time.RFC3339))
		deleted++
	}
	return deleted
}
