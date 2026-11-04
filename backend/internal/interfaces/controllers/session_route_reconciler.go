package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// Reasons a session route is finalized by reconciliation.
//
// Route state is otherwise driven only by events from the owning manager and
// runtime (status reports, completion, deletion acknowledgements). When that
// owner disappears the events stop and the route freezes: it stays visible as a
// session that no TTL loop considers finished and no DELETE can complete. The
// reconciler is the level-triggered counterpart that converges such routes using
// facts the parent owns, so a lost event can no longer leave a stale session.
const (
	RouteReconcileManagerDeleted = "manager_deleted"
	RouteReconcileRunnerDeleted  = "runner_deleted"
	RouteReconcileStuckDeletion  = "stuck_deletion"
	// RouteReconcileRuntimeLost: a TTL session's runtime stayed disconnected from
	// its control channel for RuntimeDisconnectGrace.
	RouteReconcileRuntimeLost = "runtime_lost"
	// RouteReconcileWorkloadGone: the owning manager answered that the workload
	// does not exist.
	RouteReconcileWorkloadGone = "workload_gone"
)

// routeStatusProbeTimeout bounds one status request so a slow runtime cannot
// stall a reconciliation pass.
const routeStatusProbeTimeout = 15 * time.Second

// SessionRouteReconcileOptions controls which routes are considered abandoned.
// Every rule relies on durable records the parent owns. Two plausible signals
// are deliberately not used because they are not liveness signals:
//   - Runner.LastSeen is refreshed only by runner API calls, not while a
//     runtime is busy executing a turn.
//   - Manager.LastHeartbeatAt is not written when heartbeats are tracked in the
//     ESM liveness store, so it can be days old for a healthy manager.
type SessionRouteReconcileOptions struct {
	// DryRun logs the routes and configurations that would be finalized.
	DryRun bool
	// OrphanGrace protects routes whose last status change is recent, covering
	// in-flight creation, claim and manager re-enrollment.
	OrphanGrace time.Duration
	// StuckDeletionGrace finalizes routes left terminating/delete_failed after a
	// deletion was requested but never acknowledged by the manager.
	StuckDeletionGrace time.Duration
	// RuntimeDisconnectGrace is how long a TTL session's runtime must stay
	// continuously disconnected before the session is treated as lost. The
	// connection lease expires after 75s, so this tolerates reconnects.
	RuntimeDisconnectGrace time.Duration
	// ConfigurationOrphanGrace deletes saved configurations that belong to no
	// route, allocation or local session and were not updated for this long.
	ConfigurationOrphanGrace time.Duration
	// Now overrides the clock in tests.
	Now func() time.Time
}

func (o SessionRouteReconcileOptions) withDefaults() SessionRouteReconcileOptions {
	if o.OrphanGrace <= 0 {
		o.OrphanGrace = 15 * time.Minute
	}
	if o.StuckDeletionGrace <= 0 {
		o.StuckDeletionGrace = 45 * time.Minute
	}
	if o.RuntimeDisconnectGrace <= 0 {
		o.RuntimeDisconnectGrace = 10 * time.Minute
	}
	if o.ConfigurationOrphanGrace <= 0 {
		o.ConfigurationOrphanGrace = 45 * time.Minute
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// SessionRouteReconcileResult summarizes one reconciliation pass.
type SessionRouteReconcileResult struct {
	Scanned int
	// Finalized counts finalized routes by reason (planned ones in dry-run mode).
	Finalized             map[string]int
	Failed                int
	ConfigurationsDeleted int
	// StatusesRefreshed counts routes whose frozen status was corrected from the
	// runtime's live status (e.g. a lost completion: running -> active).
	StatusesRefreshed int
}

// routeReconcileState survives between passes on the elected replica.
type routeReconcileState struct {
	mu                sync.Mutex
	disconnectedSince map[string]time.Time
}

// observeDisconnected records a disconnected observation and returns since when
// the runtime has been continuously disconnected.
func (s *routeReconcileState) observeDisconnected(sessionID string, now time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disconnectedSince == nil {
		s.disconnectedSince = map[string]time.Time{}
	}
	since, ok := s.disconnectedSince[sessionID]
	if !ok {
		s.disconnectedSince[sessionID] = now
		return now
	}
	return since
}

// retain forgets every session that is not continuously disconnected anymore.
func (s *routeReconcileState) retain(disconnected map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sessionID := range s.disconnectedSince {
		if !disconnected[sessionID] {
			delete(s.disconnectedSince, sessionID)
		}
	}
}

type routeOwnerLookup interface {
	GetManager(context.Context, string) (*sessionrunnercore.Manager, error)
	GetRunner(context.Context, string) (*sessionrunnercore.Runner, error)
}

type sessionAllocationLister interface {
	ListAllocations(context.Context, string) ([]*sessionrunnercore.Allocation, error)
}

type configurationRefLister interface {
	ListConfigurationRefs(context.Context) ([]sessionrunnercore.ConfigurationRef, error)
}

// routeReconcileFacts is what classification needs to know about a route's owners.
type routeReconcileFacts struct {
	// managerMissing is true only when the manager record is definitively gone.
	managerMissing bool
	// runnerMissing is true only when the record of the runner executing the
	// session is definitively gone.
	runnerMissing bool
}

func routeLastChange(route *repositories.SessionRoute) time.Time {
	if route.StatusUpdatedAt.After(route.StartedAt) {
		return route.StatusUpdatedAt
	}
	return route.StartedAt
}

// routeStatusAwaitsRuntime reports whether a route's status is one that only the
// runtime moves forward (it is processing or starting). Such a route freezes if
// the runtime's completion report is lost, and is the only kind worth probing:
// idle, terminal and parent-controlled states are already handled by TTL cleanup
// or by the rules above.
func routeStatusAwaitsRuntime(status string) bool {
	switch status {
	case "active", "stopped", "error", "timeout", "suspended", "resuming",
		"terminating", "delete_failed", "terminated", "templated":
		return false
	}
	return true
}

// classifyRouteForReconcile returns the reason a route must be finalized from
// durable records alone, or whether its live runtime status must be probed.
// It is pure so the policy can be tested exhaustively.
func classifyRouteForReconcile(route *repositories.SessionRoute, facts routeReconcileFacts, now time.Time, opts SessionRouteReconcileOptions) (reason string, probe bool) {
	// Local aliases and pending routes are owned by this process, and templated
	// routes are intentionally retained records.
	if route == nil || route.ManagerID == "" || route.Status == "templated" {
		return "", false
	}
	last := routeLastChange(route)
	if !last.IsZero() && now.Sub(last) < opts.OrphanGrace {
		return "", false
	}
	if (route.Status == "terminating" || route.Status == "delete_failed") &&
		!last.IsZero() && now.Sub(last) >= opts.StuckDeletionGrace {
		return RouteReconcileStuckDeletion, false
	}
	if facts.managerMissing {
		return RouteReconcileManagerDeleted, false
	}
	// Suspension releases the runner on purpose; a resumable session must not be
	// judged by the runner it no longer uses.
	if route.Status == "suspended" || route.Status == "resuming" {
		return "", false
	}
	if facts.runnerMissing {
		return RouteReconcileRunnerDeleted, false
	}
	// Time alone cannot tell a lost completion from a long turn, so ask the
	// runtime instead of guessing.
	return "", routeStatusAwaitsRuntime(route.Status)
}

type routeProbeOutcome int

const (
	// probeUnknown: the probe could not decide; keep the route as is.
	probeUnknown routeProbeOutcome = iota
	// probeAnswered: the runtime reported its live status, which was recorded.
	probeAnswered
	// probeDisconnected: the runtime/manager holds no control connection.
	probeDisconnected
	// probeWorkloadGone: the manager answered that the workload does not exist.
	probeWorkloadGone
)

// probeRouteRuntime asks the runtime behind a route for its live status over the
// same control channel user requests use. A reported status is persisted with
// the normal /status bookkeeping, so "stable" heals a frozen "running" route to
// "active" (with a fresh completion timestamp for TTL cleanup) while a real
// "running" leaves it untouched.
func (c *SessionController) probeRouteRuntime(ctx context.Context, route *repositories.SessionRoute, dryRun bool) routeProbeOutcome {
	if c.esmControlTunnel == nil {
		return probeUnknown
	}
	direct := route.Transport == repositories.SessionRouteTransportDirectRuntime
	connectionID, target := route.ManagerID, (&url.URL{Scheme: "http", Host: "esm.local", Path: "/" + route.RemoteSessionID + "/status"})
	if direct {
		// A direct runtime holds its own control connection keyed by the public ID.
		connectionID, target = route.SessionID, (&url.URL{Scheme: "http", Host: "session.local", Path: "/status"})
	} else if route.RemoteSessionID == "" {
		return probeUnknown
	}
	if !c.esmControlTunnel.IsConnected(ctx, connectionID) {
		return probeDisconnected
	}
	probeCtx, cancel := context.WithTimeout(ctx, routeStatusProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		return probeUnknown
	}
	if route.UserID != "" {
		req.Header.Set("X-Forwarded-User", route.UserID)
	}
	if route.TeamID != "" {
		req.Header.Set("X-Forwarded-Team", route.TeamID)
	}
	resp, err := c.esmControlTunnel.Do(probeCtx, connectionID, route.SessionID, route.RemoteSessionID, req)
	if err != nil {
		log.Printf("[ROUTE_RECONCILE] Status probe for session %s failed: %v", route.SessionID, err)
		return probeUnknown
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound && !direct {
		return probeWorkloadGone
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return probeUnknown
	}
	if dryRun {
		var payload struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload)
		log.Printf("[ROUTE_RECONCILE] [DRY-RUN] Session %s runtime reports %q (route status %q)", route.SessionID, payload.Status, route.Status)
		return probeAnswered
	}
	c.rememberRemoteSessionStatus(probeCtx, route, resp)
	return probeAnswered
}

// ReconcileSessionRoutes finalizes routes whose owner is gone and garbage
// collects configurations that no longer belong to any session. It is safe to
// run repeatedly; callers should run it on a single leader.
func (c *SessionController) ReconcileSessionRoutes(ctx context.Context, opts SessionRouteReconcileOptions) SessionRouteReconcileResult {
	opts = opts.withDefaults()
	result := SessionRouteReconcileResult{Finalized: map[string]int{}}
	if c.sessionRouteRepo == nil {
		return result
	}
	routes, err := c.sessionRouteRepo.List(ctx, "")
	if err != nil {
		// Never act on a partial view of the authoritative routes.
		log.Printf("[ROUTE_RECONCILE] Failed to list session routes: %v", err)
		return result
	}
	now := opts.Now()
	owners, _ := c.sessionRunnerStore.(routeOwnerLookup)
	allocations := c.allocationsBySession(ctx)
	lookup := &routeFactsLookup{controller: c, owners: owners, allocations: allocations,
		managers: map[string]bool{}, runners: map[string]bool{}}

	prefix := ""
	if opts.DryRun {
		prefix = "[DRY-RUN] "
	}
	disconnected := map[string]bool{}
	for _, route := range routes {
		if route == nil {
			continue
		}
		result.Scanned++
		reason, probe := classifyRouteForReconcile(route, lookup.facts(ctx, route), now, opts)
		if reason == "" && probe {
			reason = c.reconcileByLiveStatus(ctx, route, now, opts, disconnected, &result)
		}
		if reason == "" {
			continue
		}
		log.Printf("[ROUTE_RECONCILE] %sFinalizing session %s (reason=%s manager=%s remote=%s status=%q last_change=%s)",
			prefix, route.SessionID, reason, route.ManagerID, route.RemoteSessionID, route.Status, routeLastChange(route).Format(time.RFC3339))
		if opts.DryRun {
			result.Finalized[reason]++
			continue
		}
		if err := c.finalizeReconciledRoute(ctx, route, reason); err != nil {
			log.Printf("[ROUTE_RECONCILE] Failed to finalize session %s: %v", route.SessionID, err)
			result.Failed++
			continue
		}
		result.Finalized[reason]++
	}
	c.routeReconcile.retain(disconnected)
	result.ConfigurationsDeleted = c.reconcileOrphanedConfigurations(ctx, routes, allocations, now, opts)
	return result
}

// reconcileByLiveStatus decides a frozen route from its runtime's live state.
// It returns a finalization reason only when the workload is definitively gone:
// the manager says so, or a TTL session's runtime has stayed disconnected for
// RuntimeDisconnectGrace. A runtime that answers is never ended here.
func (c *SessionController) reconcileByLiveStatus(ctx context.Context, route *repositories.SessionRoute, now time.Time, opts SessionRouteReconcileOptions, disconnected map[string]bool, result *SessionRouteReconcileResult) string {
	previous := route.Status
	switch c.probeRouteRuntime(ctx, route, opts.DryRun) {
	case probeAnswered:
		if route.Status != previous {
			log.Printf("[ROUTE_RECONCILE] Refreshed session %s status from runtime: %q -> %q", route.SessionID, previous, route.Status)
			result.StatusesRefreshed++
		}
	case probeWorkloadGone:
		return RouteReconcileWorkloadGone
	case probeDisconnected:
		disconnected[route.SessionID] = true
		since := c.routeReconcile.observeDisconnected(route.SessionID, now)
		// Only sessions that are meant to end on their own are reclaimed for a
		// lost runtime; an interactive session stays deletable by its owner.
		if route.Tags["session_ttl"] != "" && now.Sub(since) >= opts.RuntimeDisconnectGrace {
			return RouteReconcileRuntimeLost
		}
	}
	return ""
}

func (c *SessionController) finalizeReconciledRoute(ctx context.Context, route *repositories.SessionRoute, reason string) error {
	if reason == RouteReconcileStuckDeletion {
		// The manager was asked to delete and never acknowledged. Retrying the
		// same request cannot make progress, so finalize locally; a manager that
		// still holds the workload reaps it through orphaned_session_ids.
		deletePath := "route_reconcile_" + reason
		return c.finalizeRouteLocally(ctx, route, c.resolveSessionUsageDimensions(ctx, route.SessionID, deletePath), deletePath)
	}
	// Use the canonical deletion path: it tears the workload down through the
	// manager when it is reachable and finalizes locally when it is not.
	return c.deleteSessionAsWorker(ctx, route.SessionID)
}

// finalizeRouteLocally removes the parent's record of a routed session without
// contacting its manager. Removing the route makes the workload an orphan that
// the manager deletes on its next heartbeat, so this never leaks a live runtime.
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
