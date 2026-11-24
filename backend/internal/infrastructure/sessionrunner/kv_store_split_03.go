package sessionrunner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

func runnerMatchesAllocation(runner *core.Runner, allocation *core.Allocation) bool {
	if runner == nil || allocation == nil {
		return false
	}
	if len(allocation.AuthorizedManagerIDs) > 0 && !managerIDAllowed(runner.ManagerID, allocation.AuthorizedManagerIDs) {
		return false
	}
	// DinD changes the Pod shape and cannot be enabled after a stock runner has
	// started. Treat omitted values as false for records created before
	// capability-aware matching was introduced.
	return capabilityEnabled(runner.Capabilities, "dind") == capabilityEnabled(allocation.Requirements, "dind")
}

func managerIDAllowed(managerID string, allowed []string) bool {
	for _, candidate := range allowed {
		if candidate == managerID {
			return true
		}
	}
	return false
}

func capabilityEnabled(values map[string]string, name string) bool {
	return strings.EqualFold(values[name], "true")
}

func (s *Store) Acknowledge(ctx context.Context, sessionID, runnerID, leaseID string) (*core.Allocation, error) {
	return s.transitionLease(ctx, sessionID, runnerID, leaseID, core.AllocationClaimed, "")
}

func (s *Store) Fail(ctx context.Context, sessionID, runnerID, leaseID string) (*core.Allocation, error) {
	return s.transitionLease(ctx, sessionID, runnerID, leaseID, core.AllocationPending, "failed")
}

func (s *Store) transitionLease(ctx context.Context, sessionID, runnerID, leaseID string, status core.AllocationStatus, _ string) (*core.Allocation, error) {
	for attempts := 0; attempts < 5; attempts++ {
		var allocation core.Allocation
		record, err := s.get(ctx, allocationName(sessionID), &allocation)
		if err != nil {
			return nil, err
		}
		if status == core.AllocationClaimed && allocation.RunnerID == runnerID && allocation.LeaseID == leaseID && allocation.Status == core.AllocationRunning {
			_ = s.changeRunnerStatus(ctx, runnerID, core.RunnerClaiming, core.RunnerRunning)
			return &allocation, nil
		}
		if allocation.RunnerID != runnerID || allocation.LeaseID != leaseID || allocation.Status != core.AllocationLeased {
			return nil, core.ErrConflict
		}
		if s.now().After(allocation.LeaseExpiresAt) {
			return nil, core.ErrConflict
		}
		allocation.Status = status
		allocation.UpdatedAt = s.now()
		if status == core.AllocationPending {
			allocation.RunnerID, allocation.LeaseID = "", ""
			allocation.LeaseExpiresAt = time.Time{}
			allocation.Generation++
		}
		raw, err := s.encodeResource(ctx, &allocation)
		if err != nil {
			return nil, err
		}
		var document secretDocument
		if err := json.Unmarshal(record.Value, &document); err != nil {
			return nil, err
		}
		document.Data[dataKey] = raw
		addSessionRunnerQueryLabels(document.Metadata.Labels, &allocation)
		record.Labels = document.Metadata.Labels
		record.Value, _ = json.Marshal(document)
		if _, err := s.kv.Update(ctx, record); errors.Is(err, kvstore.ErrConflict) {
			continue
		} else if err != nil {
			return nil, err
		}
		target := core.RunnerRunning
		if status == core.AllocationPending {
			target = core.RunnerIdle
		}
		if err := s.changeRunnerStatus(ctx, runnerID, core.RunnerClaiming, target); err != nil {
			return nil, err
		}
		return &allocation, nil
	}
	return nil, core.ErrConflict
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

var _ core.Store = (*Store)(nil)

func ValidateSubject(kind core.SubjectType, id string) error {
	if (kind != core.SubjectUser && kind != core.SubjectTeam) || id == "" {
		return fmt.Errorf("subject_type must be user or team and subject_id is required")
	}
	return nil
}
