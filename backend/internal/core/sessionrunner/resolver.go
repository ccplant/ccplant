package sessionrunner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Resolver struct {
	store        ResolverStore
	liveness     ManagerLiveness
	heartbeatTTL time.Duration
	now          func() time.Time
}

// ResolverStore is the read-only pool inventory required to make an
// authorization and routing decision. Keeping this boundary small lets every
// workload type use the same resolver without depending on allocation writes.
type ResolverStore interface {
	ListLogicalPools(context.Context) ([]*LogicalPool, error)
	ListManagers(context.Context) ([]*Manager, error)
	ListBindings(context.Context, string) ([]*Binding, error)
	ListPoolSuppliers(context.Context) ([]*PoolSupplier, error)
}

type ManagerLiveness interface {
	IsManagerConnected(context.Context, string) (bool, error)
}

// ResolutionTrace explains the scheduling decision made by ResolveWithTrace.
// Candidates only contains pools the subject is authorized to use, so the trace
// does not disclose pools belonging to other users or teams.
type ResolutionTrace struct {
	RequestedPool string                    `json:"requested_pool,omitempty"`
	SelectedPool  string                    `json:"selected_pool,omitempty"`
	Reason        string                    `json:"reason"`
	Candidates    []PoolCandidateResolution `json:"candidates"`
}

type PoolCandidateResolution struct {
	Pool             string            `json:"pool"`
	BindingID        string            `json:"binding_id"`
	Priority         int               `json:"priority"`
	ExplicitOnly     bool              `json:"explicit_only,omitempty"`
	HealthySuppliers int               `json:"healthy_suppliers"`
	Eligible         bool              `json:"eligible"`
	ExcludedBy       []string          `json:"excluded_by,omitempty"`
	RequiredLabels   map[string]string `json:"required_labels,omitempty"`
}

func NewResolver(store ResolverStore, heartbeatTTL time.Duration) *Resolver {
	return &Resolver{store: store, heartbeatTTL: heartbeatTTL, now: func() time.Time { return time.Now().UTC() }}
}

func (r *Resolver) WithManagerLiveness(liveness ManagerLiveness) *Resolver {
	r.liveness = liveness
	return r
}

func (r *Resolver) availablePools(ctx context.Context, subject Subject) ([]*ResolvedPool, error) {
	pools, err := r.store.ListLogicalPools(ctx)
	if err != nil {
		return nil, err
	}
	managers, err := r.store.ListManagers(ctx)
	if err != nil {
		return nil, err
	}
	bindings, err := r.store.ListBindings(ctx, "")
	if err != nil {
		return nil, err
	}
	suppliers, err := r.store.ListPoolSuppliers(ctx)
	if err != nil {
		return nil, err
	}
	healthyManagers := make(map[string]bool, len(managers))
	for _, manager := range managers {
		available, err := r.managerAvailable(ctx, manager)
		if err != nil {
			return nil, err
		}
		healthyManagers[manager.ID] = available
	}
	healthy := make(map[string]bool)
	for _, supplier := range suppliers {
		if supplier.Enabled && !supplier.Draining && healthyManagers[supplier.ManagerID] {
			healthy[supplier.Pool] = true
		}
	}
	result := make([]*ResolvedPool, 0, len(pools))
	for _, pool := range pools {
		binding := effectiveBinding(bindings, pool.Name, subject)
		if binding == nil || !binding.Role.GrantsUse() || !binding.Enabled || !pool.Enabled || !healthy[pool.Name] {
			continue
		}
		result = append(result, &ResolvedPool{Pool: pool, Binding: binding})
	}
	return result, nil
}

func (r *Resolver) AvailablePools(ctx context.Context, subject Subject) ([]*LogicalPool, error) {
	available, err := r.availablePools(ctx, subject)
	if err != nil {
		return nil, err
	}
	result := make([]*LogicalPool, 0, len(available))
	for _, resolved := range available {
		result = append(result, resolved.Pool)
	}
	return result, nil
}

func (r *Resolver) Resolve(ctx context.Context, subject Subject, requestedPool string, tags map[string]string) (*ResolvedPool, error) {
	resolved, _, err := r.ResolveWithTrace(ctx, subject, requestedPool, tags)
	return resolved, err
}

// ResolveRoute is the single authorization and routing entry point for direct
// manager workloads. It fails closed unless the subject has an enabled use
// binding to an enabled pool with at least one healthy, enabled supplier.
func (r *Resolver) ResolveRoute(ctx context.Context, subject Subject, requestedPool string, tags map[string]string) (*ResolvedRoute, error) {
	resolved, err := r.Resolve(ctx, subject, requestedPool, tags)
	if err != nil || resolved == nil {
		return nil, err
	}
	managers, err := r.store.ListManagers(ctx)
	if err != nil {
		return nil, err
	}
	suppliers, err := r.store.ListPoolSuppliers(ctx)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool)
	for _, supplier := range suppliers {
		if supplier != nil && supplier.Pool == resolved.Pool.Name && supplier.Enabled && !supplier.Draining {
			allowed[supplier.ManagerID] = true
		}
	}
	route := &ResolvedRoute{Pool: resolved.Pool, Binding: resolved.Binding}
	for _, manager := range managers {
		if manager == nil || !allowed[manager.ID] {
			continue
		}
		available, err := r.managerAvailable(ctx, manager)
		if err != nil {
			return nil, err
		}
		if available {
			route.Managers = append(route.Managers, manager)
		}
	}
	if len(route.Managers) == 0 {
		return nil, nil
	}
	sort.Slice(route.Managers, func(i, j int) bool { return route.Managers[i].ID < route.Managers[j].ID })
	return route, nil
}

// ResolveWithTrace applies the same scheduling algorithm as Resolve and also
// returns stable reason codes suitable for dry-run assertions.
func (r *Resolver) ResolveWithTrace(ctx context.Context, subject Subject, requestedPool string, tags map[string]string) (*ResolvedPool, *ResolutionTrace, error) {
	pools, err := r.store.ListLogicalPools(ctx)
	if err != nil {
		return nil, nil, err
	}
	managers, err := r.store.ListManagers(ctx)
	if err != nil {
		return nil, nil, err
	}
	bindings, err := r.store.ListBindings(ctx, "")
	if err != nil {
		return nil, nil, err
	}
	suppliers, err := r.store.ListPoolSuppliers(ctx)
	if err != nil {
		return nil, nil, err
	}
	healthyManagers := make(map[string]bool, len(managers))
	for _, manager := range managers {
		available, err := r.managerAvailable(ctx, manager)
		if err != nil {
			return nil, nil, err
		}
		healthyManagers[manager.ID] = available
	}
	healthySuppliers := make(map[string]int)
	for _, supplier := range suppliers {
		if supplier.Enabled && !supplier.Draining && healthyManagers[supplier.ManagerID] {
			healthySuppliers[supplier.Pool]++
		}
	}
	requested := strings.TrimSpace(requestedPool)
	trace := &ResolutionTrace{RequestedPool: requested, Reason: "no_eligible_pool", Candidates: []PoolCandidateResolution{}}
	var candidates []*ResolvedPool
	for _, pool := range pools {
		binding := effectiveBinding(bindings, pool.Name, subject)
		// Do not expose the existence or state of pools the caller cannot use.
		if binding == nil || !binding.Role.GrantsUse() || !binding.Enabled {
			continue
		}
		candidate := PoolCandidateResolution{Pool: pool.Name, BindingID: binding.ID, Priority: binding.Priority, ExplicitOnly: binding.ExplicitOnly, HealthySuppliers: healthySuppliers[pool.Name], RequiredLabels: allocatorLabels(tags)}
		if !pool.Enabled {
			candidate.ExcludedBy = append(candidate.ExcludedBy, "pool_disabled")
		}
		if candidate.HealthySuppliers == 0 {
			candidate.ExcludedBy = append(candidate.ExcludedBy, "no_healthy_supplier")
		}
		if requested == "" && binding.ExplicitOnly {
			candidate.ExcludedBy = append(candidate.ExcludedBy, "explicit_selection_required")
		}
		if !poolMatchesTags(pool, tags) {
			candidate.ExcludedBy = append(candidate.ExcludedBy, "allocator_labels_mismatch")
		}
		if requested != "" && pool.Name != requested {
			candidate.ExcludedBy = append(candidate.ExcludedBy, "not_requested_pool")
		}
		candidate.Eligible = len(candidate.ExcludedBy) == 0
		trace.Candidates = append(trace.Candidates, candidate)
		if !candidate.Eligible {
			continue
		}
		resolved := &ResolvedPool{Pool: pool, Binding: binding}
		candidates = append(candidates, resolved)
	}
	sort.Slice(trace.Candidates, func(i, j int) bool { return trace.Candidates[i].Pool < trace.Candidates[j].Pool })
	if requested != "" {
		if len(candidates) == 0 {
			trace.Reason = "requested_pool_not_eligible"
			return nil, trace, fmt.Errorf("no authorized and healthy session pool matches %q", requested)
		}
		selected := firstPoolByPriority(candidates)
		trace.SelectedPool, trace.Reason = selected.Pool.Name, "requested_pool_selected"
		return selected, trace, nil
	}
	if len(candidates) == 0 {
		return nil, trace, nil
	}
	selected := firstPoolByPriority(candidates)
	trace.SelectedPool, trace.Reason = selected.Pool.Name, "highest_priority_eligible_binding"
	return selected, trace, nil
}

func allocatorLabels(tags map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range tags {
		if strings.HasPrefix(key, "allocator.") && key != "allocator.pool" {
			result[strings.TrimPrefix(key, "allocator.")] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func firstPoolByPriority(pools []*ResolvedPool) *ResolvedPool {
	sort.Slice(pools, func(i, j int) bool {
		if pools[i].Binding.Priority != pools[j].Binding.Priority {
			return pools[i].Binding.Priority > pools[j].Binding.Priority
		}
		return pools[i].Pool.Name < pools[j].Pool.Name
	})
	return pools[0]
}

func (r *Resolver) managerAvailable(ctx context.Context, manager *Manager) (bool, error) {
	if manager == nil || !manager.Enabled || manager.Draining {
		return false, nil
	}
	if r.liveness != nil {
		return r.liveness.IsManagerConnected(ctx, manager.ID)
	}
	return ManagerAvailable(manager, r.heartbeatTTL, r.now()), nil
}

// ManagerAvailable reports whether a manager may participate in scheduling.
// Heartbeat health is derived at scheduling time so a recovered manager becomes
// eligible again without mutating its configured priority or enabled state.
func ManagerAvailable(manager *Manager, heartbeatTTL time.Duration, now time.Time) bool {
	if manager == nil || !manager.Enabled || manager.Draining {
		return false
	}
	if heartbeatTTL <= 0 {
		return true
	}
	if manager.LastHeartbeatAt.IsZero() {
		return false
	}
	return now.Sub(manager.LastHeartbeatAt) <= heartbeatTTL
}

func effectiveBinding(bindings []*Binding, pool string, subject Subject) *Binding {
	var all *Binding
	for _, binding := range bindings {
		if binding.Pool != pool {
			continue
		}
		if binding.SubjectType == subject.Type && binding.SubjectID == subject.ID {
			return binding
		}
		if binding.Enabled && binding.SubjectType == SubjectAll && binding.SubjectID == "" {
			all = binding
		}
	}
	return all
}

func poolMatchesTags(pool *LogicalPool, tags map[string]string) bool {
	for key, value := range tags {
		if !strings.HasPrefix(key, "allocator.") || key == "allocator.pool" {
			continue
		}
		label := strings.TrimPrefix(key, "allocator.")
		if pool.Labels[label] != value {
			return false
		}
	}
	return true
}
