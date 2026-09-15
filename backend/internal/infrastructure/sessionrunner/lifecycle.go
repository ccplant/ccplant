package sessionrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

func (s *Store) mutate(ctx context.Context, name string, value any, change func() error) error {
	for range 5 {
		record, err := s.get(ctx, name, value)
		if err != nil {
			return err
		}
		if err = change(); err != nil {
			return err
		}
		var doc secretDocument
		if err = json.Unmarshal(record.Value, &doc); err != nil {
			return err
		}
		doc.Data[dataKey], err = json.Marshal(value)
		if err != nil {
			return err
		}
		record.Value, err = json.Marshal(doc)
		if err != nil {
			return err
		}
		_, err = s.kv.Update(ctx, record)
		if errors.Is(err, kvstore.ErrConflict) {
			continue
		}
		return err
	}
	return core.ErrConflict
}

// Retirement and claim reservation contend on the same runner record. A
// successful retirement remains a tombstone so delayed registrations cannot
// make a deleting Pod claimable again.
func (s *Store) RetireRunner(ctx context.Context, managerID, id string) error {
	var runner core.Runner
	err := s.mutate(ctx, runnerName(id), &runner, func() error {
		if runner.ManagerID != managerID {
			return core.ErrUnauthorized
		}
		if runner.Status != core.RunnerIdle && runner.Status != core.RunnerDraining {
			return core.ErrConflict
		}
		runner.Status = core.RunnerDraining
		runner.UpdatedAt = s.now()
		return nil
	})
	if errors.Is(err, core.ErrNotFound) {
		err = s.CreateRunner(ctx, &core.Runner{ID: id, ManagerID: managerID, Status: core.RunnerDraining})
		if errors.Is(err, core.ErrConflict) {
			return s.RetireRunner(ctx, managerID, id)
		}
	}
	return err
}

func (s *Store) changeRunnerStatus(ctx context.Context, id string, from, to core.RunnerStatus) error {
	var runner core.Runner
	return s.mutate(ctx, runnerName(id), &runner, func() error {
		if runner.Status != from {
			return core.ErrConflict
		}
		runner.Status = to
		runner.UpdatedAt = s.now()
		return nil
	})
}

// Requeue only allocations whose runtime never connected. CAS arbitrates with
// MarkStarted, so a runtime that has started is never automatically duplicated.
func (s *Store) RequeueUnstarted(ctx context.Context, id, runnerID string) (*core.Allocation, error) {
	var a core.Allocation
	err := s.mutate(ctx, allocationName(id), &a, func() error {
		if a.RunnerID != runnerID || (a.Status != core.AllocationLeased && a.Status != core.AllocationClaimed) {
			return core.ErrConflict
		}
		token, err := randomToken(32)
		if err != nil {
			return err
		}
		hash := sha256.Sum256([]byte(token))
		a.RuntimeToken = token
		a.RuntimeTokenHash = hex.EncodeToString(hash[:])
		a.Generation++
		a.Status = core.AllocationPending
		a.ManagerID = ""
		a.RunnerID = ""
		a.LeaseID = ""
		a.LeaseExpiresAt = time.Time{}
		a.UpdatedAt = s.now()
		return nil
	})
	return &a, err
}

func (s *Store) MarkStarted(ctx context.Context, id string, generation int64) error {
	existing, err := s.GetAllocation(ctx, id)
	if err != nil {
		return err
	}
	if existing.Generation == generation && existing.Status == core.AllocationRunning {
		return nil
	}
	var a core.Allocation
	return s.mutate(ctx, allocationName(id), &a, func() error {
		if a.Generation != generation || (a.Status != core.AllocationLeased && a.Status != core.AllocationClaimed && a.Status != core.AllocationRunning) {
			return core.ErrConflict
		}
		a.Status = core.AllocationRunning
		a.UpdatedAt = s.now()
		return nil
	})
}
