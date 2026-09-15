package sessionrunner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
)

func TestClaimAndRetirementAreMutuallyExclusive(t *testing.T) {
	for range 50 {
		s := newVersionedTestStore(t)
		ctx := context.Background()
		require.NoError(t, s.CreateRunner(ctx, &core.Runner{ID: "runner", ManagerID: "manager", Pool: "pool"}))
		require.NoError(t, s.Enqueue(ctx, &core.Allocation{SessionID: "session", Pool: "pool"}))
		ready := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var claimed bool
		var claimErr, retireErr error
		go func() {
			defer wg.Done()
			<-ready
			_, claimed, claimErr = s.ClaimNext(ctx, "pool", "runner", time.Minute)
		}()
		go func() { defer wg.Done(); <-ready; retireErr = s.RetireRunner(ctx, "manager", "runner") }()
		close(ready)
		wg.Wait()
		require.NoError(t, claimErr)
		if claimed {
			require.ErrorIs(t, retireErr, core.ErrConflict)
		} else {
			require.NoError(t, retireErr)
		}
		runner, err := s.GetRunner(ctx, "runner")
		require.NoError(t, err)
		if claimed {
			require.Equal(t, core.RunnerClaiming, runner.Status)
		} else {
			require.Equal(t, core.RunnerDraining, runner.Status)
		}
		require.NoError(t, s.TouchRunner(ctx, "runner", time.Now()))
		after, err := s.GetRunner(ctx, "runner")
		require.NoError(t, err)
		require.Equal(t, runner.Status, after.Status)
	}
}

func TestRetirementFencesDelayedRegistration(t *testing.T) {
	s := newVersionedTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.RetireRunner(ctx, "manager", "runner"))
	require.ErrorIs(t, s.CreateRunner(ctx, &core.Runner{ID: "runner", ManagerID: "manager", Pool: "pool"}), core.ErrConflict)
	require.NoError(t, s.RetireRunner(ctx, "manager", "runner"))
	require.ErrorIs(t, s.RetireRunner(ctx, "other-manager", "runner"), core.ErrUnauthorized)
}

func TestUnstartedRecoveryRetainsSettingsAndFencesOldRuntime(t *testing.T) {
	s := newVersionedTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"old", "new"} {
		require.NoError(t, s.CreateRunner(ctx, &core.Runner{ID: id, ManagerID: "manager", Pool: "pool"}))
	}
	settings := []byte(`{"initial_message":"hello","session":{"agent_type":"codex-acp"}}`)
	require.NoError(t, s.Enqueue(ctx, &core.Allocation{SessionID: "session", Pool: "pool", RuntimeToken: "original-token", ProvisionSettings: settings, BindingID: "binding", Requirements: map[string]string{"agent_type": "codex-acp"}}))
	a, ok, err := s.ClaimNext(ctx, "pool", "old", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = s.Acknowledge(ctx, a.SessionID, "old", a.LeaseID)
	require.NoError(t, err)
	require.ErrorIs(t, s.RetireRunner(ctx, "manager", "old"), core.ErrConflict)
	recovered, err := s.RequeueUnstarted(ctx, a.SessionID, "old")
	require.NoError(t, err)
	require.Equal(t, a.Generation+1, recovered.Generation)
	require.Equal(t, settings, recovered.ProvisionSettings)
	require.Equal(t, a.CreatedAt, recovered.CreatedAt)
	require.Equal(t, a.Requirements, recovered.Requirements)
	require.Equal(t, "binding", recovered.BindingID)
	require.NotEqual(t, a.RuntimeToken, recovered.RuntimeToken)
	require.Empty(t, recovered.RunnerID)
	require.ErrorIs(t, s.MarkStarted(ctx, a.SessionID, a.Generation), core.ErrConflict)
	next, ok, err := s.ClaimNext(ctx, "pool", "new", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, a.SessionID, next.SessionID)
	require.NoError(t, s.MarkStarted(ctx, next.SessionID, next.Generation))
	_, err = s.Acknowledge(ctx, next.SessionID, "new", next.LeaseID)
	require.NoError(t, err)
	_, err = s.RequeueUnstarted(ctx, next.SessionID, "new")
	require.ErrorIs(t, err, core.ErrConflict)
}

func TestRuntimeStartAndRecoveryAreMutuallyExclusive(t *testing.T) {
	for range 30 {
		s := newVersionedTestStore(t)
		ctx := context.Background()
		require.NoError(t, s.CreateRunner(ctx, &core.Runner{ID: "runner", ManagerID: "manager", Pool: "pool"}))
		require.NoError(t, s.Enqueue(ctx, &core.Allocation{SessionID: "session", Pool: "pool"}))
		a, ok, err := s.ClaimNext(ctx, "pool", "runner", time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		var startErr, retryErr error
		ready := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-ready; startErr = s.MarkStarted(ctx, a.SessionID, a.Generation) }()
		go func() { defer wg.Done(); <-ready; _, retryErr = s.RequeueUnstarted(ctx, a.SessionID, "runner") }()
		close(ready)
		wg.Wait()
		require.True(t, (startErr == nil && errors.Is(retryErr, core.ErrConflict)) || (retryErr == nil && errors.Is(startErr, core.ErrConflict)))
	}
}
