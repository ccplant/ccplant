package sessionrunner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Resolver struct {
	store        Store
	heartbeatTTL time.Duration
	now          func() time.Time
}

func NewResolver(store Store, heartbeatTTL time.Duration) *Resolver {
	return &Resolver{store: store, heartbeatTTL: heartbeatTTL, now: func() time.Time { return time.Now().UTC() }}
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
	managerByID := make(map[string]*Manager, len(managers))
	for _, manager := range managers {
		managerByID[manager.ID] = manager
	}
	healthy := make(map[string]bool)
	explicitOnly := make(map[string]bool)
	automaticEligible := make(map[string]bool)
	for _, supplier := range suppliers {
		manager := managerByID[supplier.ManagerID]
		if supplier.Enabled && !supplier.Draining && r.managerAvailable(manager) {
			healthy[supplier.Pool] = true
			if managerHasCapability(manager, CapabilityDirectRuntimeV1) {
				explicitOnly[supplier.Pool] = true
			} else {
				automaticEligible[supplier.Pool] = true
			}
		}
	}
	result := make([]*ResolvedPool, 0, len(pools))
	for _, pool := range pools {
		binding := effectiveBinding(bindings, pool.Name, subject)
		if binding == nil || !pool.Enabled || !healthy[pool.Name] {
			continue
		}
		resolvedBinding := *binding
		resolvedBinding.ExplicitOnly = resolvedBinding.ExplicitOnly || (explicitOnly[pool.Name] && !automaticEligible[pool.Name])
		result = append(result, &ResolvedPool{Pool: pool, Binding: &resolvedBinding})
	}
	return result, nil
}

func managerHasCapability(manager *Manager, capability string) bool {
	if manager == nil {
		return false
	}
	for _, candidate := range manager.Capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
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

func (r *Resolver) Resolve(ctx context.Context, subject Subject, tags map[string]string) (*ResolvedPool, error) {
	available, err := r.availablePools(ctx, subject)
	if err != nil {
		return nil, err
	}
	requested := strings.TrimSpace(tags["allocator.pool"])
	var candidates []*ResolvedPool
	for _, resolved := range available {
		if requested == "" && resolved.Binding.ExplicitOnly {
			continue
		}
		if !poolMatchesTags(resolved.Pool, tags) {
			continue
		}
		if requested != "" && resolved.Pool.Name != requested {
			continue
		}
		candidates = append(candidates, resolved)
	}
	if requested != "" {
		if len(candidates) == 0 {
			return nil, fmt.Errorf("no authorized and healthy session pool matches %q", requested)
		}
		return firstPoolByPriority(candidates), nil
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	return firstPoolByPriority(candidates), nil
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

func (r *Resolver) managerAvailable(manager *Manager) bool {
	if manager == nil || !manager.Enabled || manager.Draining {
		return false
	}
	if r.heartbeatTTL <= 0 || manager.LastHeartbeatAt.IsZero() {
		return true
	}
	return r.now().Sub(manager.LastHeartbeatAt) <= r.heartbeatTTL
}

func effectiveBinding(bindings []*Binding, pool string, subject Subject) *Binding {
	var all *Binding
	for _, binding := range bindings {
		if !binding.Enabled || binding.Pool != pool {
			continue
		}
		if binding.SubjectType == subject.Type && binding.SubjectID == subject.ID {
			return binding
		}
		if binding.SubjectType == SubjectAll && binding.SubjectID == "" {
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
