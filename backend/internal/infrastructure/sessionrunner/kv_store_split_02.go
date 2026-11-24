package sessionrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

func (s *Store) ListLogicalPools(ctx context.Context) ([]*core.LogicalPool, error) {
	var result []*core.LogicalPool
	err := s.list(ctx, "logical-pool", func(raw []byte) error {
		var value core.LogicalPool
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		result = append(result, &value)
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, err
}

func (s *Store) UpdateLogicalPool(ctx context.Context, pool *core.LogicalPool) error {
	current, err := s.GetLogicalPool(ctx, pool.Name)
	if err != nil {
		return err
	}
	pool.CreatedAt, pool.UpdatedAt = current.CreatedAt, s.now()
	return s.update(ctx, logicalPoolName(pool.Name), pool)
}

func (s *Store) DeleteLogicalPool(ctx context.Context, name string) error {
	return s.delete(ctx, logicalPoolName(name))
}

func (s *Store) CreatePoolSupplier(ctx context.Context, supplier *core.PoolSupplier) error {
	now := s.now()
	supplier.CreatedAt, supplier.UpdatedAt = now, now
	return s.create(ctx, poolSupplierName(supplier.ManagerID, supplier.Pool), "pool-supplier", supplier.Pool, supplier)
}

func (s *Store) GetPoolSupplier(ctx context.Context, managerID, pool string) (*core.PoolSupplier, error) {
	var value core.PoolSupplier
	_, err := s.get(ctx, poolSupplierName(managerID, pool), &value)
	return &value, err
}

func (s *Store) ListPoolSuppliers(ctx context.Context) ([]*core.PoolSupplier, error) {
	var result []*core.PoolSupplier
	err := s.list(ctx, "pool-supplier", func(raw []byte) error {
		var value core.PoolSupplier
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		result = append(result, &value)
		return nil
	})
	sort.Slice(result, func(i, j int) bool {
		if result[i].Pool == result[j].Pool {
			return result[i].ManagerID < result[j].ManagerID
		}
		return result[i].Pool < result[j].Pool
	})
	return result, err
}

func (s *Store) UpdatePoolSupplier(ctx context.Context, supplier *core.PoolSupplier) error {
	current, err := s.GetPoolSupplier(ctx, supplier.ManagerID, supplier.Pool)
	if err != nil {
		return err
	}
	supplier.CreatedAt, supplier.UpdatedAt = current.CreatedAt, s.now()
	return s.update(ctx, poolSupplierName(supplier.ManagerID, supplier.Pool), supplier)
}

func (s *Store) DeletePoolSupplier(ctx context.Context, managerID, pool string) error {
	return s.delete(ctx, poolSupplierName(managerID, pool))
}

func (s *Store) CreateBinding(ctx context.Context, binding *core.Binding) error {
	binding.NormalizeRoles()
	existing, err := s.ListBindings(ctx, binding.Pool)
	if err != nil {
		return err
	}
	for _, candidate := range existing {
		if candidate.SubjectType == binding.SubjectType && candidate.SubjectID == binding.SubjectID {
			return core.ErrConflict
		}
	}
	binding.ID = "binding-" + hashName(binding.Pool+"\x00"+string(binding.SubjectType)+"\x00"+binding.SubjectID)
	now := s.now()
	binding.CreatedAt, binding.UpdatedAt = now, now
	return s.create(ctx, bindingName(binding.ID), "binding", binding.Pool, binding)
}

func (s *Store) ListBindings(ctx context.Context, pool string) ([]*core.Binding, error) {
	var result []*core.Binding
	err := s.list(ctx, "binding", func(raw []byte) error {
		var value core.Binding
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if pool == "" || value.Pool == pool {
			if value.Role == "" {
				value.Role = core.BindingRoleUse
			}
			value.NormalizeRoles()
			result = append(result, &value)
		}
		return nil
	}, pool)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority == result[j].Priority {
			return result[i].ID < result[j].ID
		}
		return result[i].Priority > result[j].Priority
	})
	return result, err
}

func (s *Store) UpdateBinding(ctx context.Context, binding *core.Binding) error {
	binding.NormalizeRoles()
	bindings, err := s.ListBindings(ctx, binding.Pool)
	if err != nil {
		return err
	}
	for _, current := range bindings {
		if current.ID == binding.ID {
			binding.CreatedAt, binding.UpdatedAt = current.CreatedAt, s.now()
			return s.update(ctx, bindingName(binding.ID), binding)
		}
	}
	return core.ErrNotFound
}

func (s *Store) DeleteBinding(ctx context.Context, id string) error {
	return s.delete(ctx, bindingName(id))
}

func (s *Store) CreateRunner(ctx context.Context, runner *core.Runner) error {
	if runner.ID == "" {
		runner.ID = uuid.NewString()
	}
	now := s.now()
	runner.CreatedAt, runner.UpdatedAt, runner.LastSeen = now, now, now
	if runner.Status == "" {
		runner.Status = core.RunnerIdle
	}
	return s.create(ctx, runnerName(runner.ID), "runner", runner.Pool, runner)
}

func (s *Store) GetRunner(ctx context.Context, id string) (*core.Runner, error) {
	var value core.Runner
	_, err := s.get(ctx, runnerName(id), &value)
	return &value, err
}

// TouchRunner updates liveness without overwriting a concurrent claim or retirement.
func (s *Store) TouchRunner(ctx context.Context, id string, seen time.Time) error {
	var current core.Runner
	return s.mutate(ctx, runnerName(id), &current, func() error {
		current.LastSeen = seen
		current.UpdatedAt = s.now()
		return nil
	})
}

func (s *Store) ListRunners(ctx context.Context, pool string) ([]*core.Runner, error) {
	var result []*core.Runner
	err := s.list(ctx, "runner", func(raw []byte) error {
		var value core.Runner
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if pool == "" || value.Pool == pool {
			result = append(result, &value)
		}
		return nil
	}, pool)
	return result, err
}

// ListManagerRunners uses the manager ownership label so heartbeat work does
// not grow with runners belonging to other managers. The legacy query keeps
// pre-index records visible and lazily backfills their labels.
func (s *Store) ListManagerRunners(ctx context.Context, managerID string) ([]*core.Runner, error) {
	var result []*core.Runner
	decode := func(raw []byte) error {
		var value core.Runner
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if value.ManagerID == managerID {
			result = append(result, &value)
		}
		return nil
	}
	if err := s.listWithSelector(ctx, "runner", decode, labelManagerHash+"="+hashName(managerID)); err != nil {
		return nil, err
	}
	if err := s.listWithSelector(ctx, "runner", func(raw []byte) error {
		var value core.Runner
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if err := s.backfillManagerQueryLabel(ctx, runnerName(value.ID), value.ManagerID); err != nil {
			return err
		}
		if value.ManagerID == managerID {
			result = append(result, &value)
		}
		return nil
	}, "!"+labelManagerHash); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) DeleteRunner(ctx context.Context, id string) error {
	return s.delete(ctx, runnerName(id))
}

func (s *Store) Enqueue(ctx context.Context, allocation *core.Allocation) error {
	now := s.now()
	allocation.Status = core.AllocationPending
	if allocation.Generation <= 0 {
		allocation.Generation = 1
	}
	allocation.CreatedAt, allocation.UpdatedAt = now, now
	if _, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, allocationName(allocation.SessionID)); err == nil {
		return core.ErrConflict
	} else if !errors.Is(err, kvstore.ErrNotFound) {
		return err
	}
	return s.create(ctx, allocationName(allocation.SessionID), "allocation", allocation.Pool, allocation)
}

func (s *Store) GetAllocation(ctx context.Context, id string) (*core.Allocation, error) {
	var value core.Allocation
	if _, err := s.get(ctx, allocationName(id), &value); err != nil {
		return &value, err
	}
	return &value, s.attachAllocationSettings(ctx, &value)
}

func (s *Store) ListAllocations(ctx context.Context, pool string) ([]*core.Allocation, error) {
	var result []*core.Allocation
	err := s.list(ctx, "allocation", func(raw []byte) error {
		var value core.Allocation
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if pool == "" || value.Pool == pool {
			result = append(result, &value)
		}
		return nil
	}, pool)
	return result, err
}

// ListManagerAllocations is the allocation counterpart to ListManagerRunners.
func (s *Store) ListManagerAllocations(ctx context.Context, managerID string) ([]*core.Allocation, error) {
	var result []*core.Allocation
	decode := func(raw []byte) error {
		var value core.Allocation
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if value.ManagerID == managerID {
			result = append(result, &value)
		}
		return nil
	}
	if err := s.listWithSelector(ctx, "allocation", decode, labelManagerHash+"="+hashName(managerID)); err != nil {
		return nil, err
	}
	if err := s.listWithSelector(ctx, "allocation", func(raw []byte) error {
		var value core.Allocation
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if err := s.backfillManagerQueryLabel(ctx, allocationName(value.SessionID), value.ManagerID); err != nil {
			return err
		}
		if value.ManagerID == managerID {
			result = append(result, &value)
		}
		return nil
	}, "!"+labelManagerHash); err != nil {
		return nil, err
	}
	if err := s.migrateManagerAllocations(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) DeleteAllocation(ctx context.Context, sessionID string) error {
	if err := s.delete(ctx, allocationName(sessionID)); err != nil {
		return err
	}
	s.metaRepairAttempted.Delete(sessionID)
	return s.delete(ctx, allocationSettingsName(sessionID))
}

func (s *Store) ClaimNext(ctx context.Context, pool, runnerID string, lease time.Duration) (*core.Allocation, bool, error) {
	runner, err := s.GetRunner(ctx, runnerID)
	if err != nil {
		return nil, false, err
	}
	if runner.Pool != pool {
		return nil, false, core.ErrUnauthorized
	}
	// NotIn intentionally includes legacy rows without a materialized status.
	// Capability authorization remains a post-decryption check so older DinD
	// allocations without query labels cannot be starved during migration.
	selector := labelResource + "=allocation," + labelPoolHash + "=" + hashName(pool) +
		"," + labelStatus + " notin (claimed,running,completed,failed)"
	records, err := s.kv.List(ctx, kvstore.Query{
		Kind:          kvstore.KindSecret,
		Namespace:     s.namespace,
		KeyPrefix:     allocationPrefix,
		LabelSelector: selector,
	})
	if err != nil {
		return nil, false, err
	}
	type candidate struct {
		record     kvstore.Record
		document   secretDocument
		allocation core.Allocation
	}
	var candidates []candidate
	for _, record := range records {
		var document secretDocument
		if json.Unmarshal(record.Value, &document) != nil ||
			document.Metadata.Labels[labelResource] != "allocation" ||
			document.Metadata.Labels[labelPoolHash] != hashName(pool) {
			continue
		}
		var allocation core.Allocation
		if json.Unmarshal(document.Data[dataKey], &allocation) == nil {
			candidates = append(candidates, candidate{record: record, document: document, allocation: allocation})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].allocation.CreatedAt.Before(candidates[j].allocation.CreatedAt)
	})
	now := s.now()
	for i := range candidates {
		item := &candidates[i]
		allocation := item.allocation
		if !runnerMatchesAllocation(runner, &allocation) {
			continue
		}
		claimable := allocation.Status == core.AllocationPending || (allocation.Status == core.AllocationLeased && now.After(allocation.LeaseExpiresAt))
		if !claimable {
			continue
		}
		if err := s.changeRunnerStatus(ctx, runnerID, core.RunnerIdle, core.RunnerClaiming); err != nil {
			if errors.Is(err, core.ErrConflict) {
				return nil, false, nil
			}
			return nil, false, err
		}
		committed := false
		defer func() {
			if !committed {
				_ = s.changeRunnerStatus(context.WithoutCancel(ctx), runnerID, core.RunnerClaiming, core.RunnerIdle)
			}
		}()
		leaseID, err := randomToken(24)
		if err != nil {
			return nil, false, err
		}
		if allocation.Status == core.AllocationLeased {
			token, err := randomToken(32)
			if err != nil {
				return nil, false, err
			}
			digest := sha256.Sum256([]byte(token))
			allocation.RuntimeToken = token
			allocation.RuntimeTokenHash = hex.EncodeToString(digest[:])
			allocation.Generation++
		}
		allocation.Status = core.AllocationLeased
		allocation.ManagerID = runner.ManagerID
		allocation.RunnerID = runnerID
		allocation.LeaseID = leaseID
		allocation.LeaseExpiresAt = now.Add(lease)
		allocation.Attempts++
		allocation.UpdatedAt = now
		raw, err := s.encodeResource(ctx, &allocation)
		if err != nil {
			return nil, false, err
		}
		item.document.Data[dataKey] = raw
		addSessionRunnerQueryLabels(item.document.Metadata.Labels, &allocation)
		item.record.Labels = item.document.Metadata.Labels
		item.record.Value, _ = json.Marshal(item.document)
		if _, err := s.kv.Update(ctx, item.record); errors.Is(err, kvstore.ErrConflict) {
			return nil, false, nil
		} else if err != nil {
			// The write may have committed despite a transport/replication error.
			// Keep the runner fenced against deletion until reconciliation resolves it.
			committed = true
			return nil, false, err
		}
		committed = true
		if err := s.attachAllocationSettings(ctx, &allocation); err != nil {
			return nil, false, err
		}
		return &allocation, true, nil
	}
	return nil, false, nil
}
