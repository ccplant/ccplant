package controllers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	"k8s.io/client-go/kubernetes/fake"
)

type allocationReadCountingStore struct {
	*infra.Store
	managerLists int
	poolLists    int
	settingReads int
}

func (s *allocationReadCountingStore) ListManagerAllocations(ctx context.Context, managerID string) ([]*core.Allocation, error) {
	s.managerLists++
	return s.Store.ListManagerAllocations(ctx, managerID)
}

func (s *allocationReadCountingStore) ListAllocations(ctx context.Context, pool string) ([]*core.Allocation, error) {
	s.poolLists++
	return s.Store.ListAllocations(ctx, pool)
}

func (s *allocationReadCountingStore) GetAllocation(ctx context.Context, id string) (*core.Allocation, error) {
	s.settingReads++
	return s.Store.GetAllocation(ctx, id)
}

func TestSessionManagerHeartbeatReadsAllocationsOnceWithoutSettings(t *testing.T) {
	ctx := context.Background()
	store := &allocationReadCountingStore{Store: infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")}
	token, tokenHash, err := newSessionRunnerToken()
	require.NoError(t, err)
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	require.NoError(t, store.CreateManager(ctx, manager))
	require.NoError(t, store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: true}))
	require.NoError(t, store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "linux", ManagerID: manager.ID, Enabled: true}))

	const sessions = 25
	localIDs := make([]string, 0, sessions)
	for i := range sessions {
		runnerID := fmt.Sprintf("runner-%02d", i)
		sessionID := fmt.Sprintf("session-%02d", i)
		localIDs = append(localIDs, runnerID)
		require.NoError(t, store.CreateRunner(ctx, &core.Runner{ID: runnerID, ManagerID: manager.ID, Pool: "linux"}))
		settings := fmt.Sprintf(`{"session":{"id":%q,"user_id":"alice","scope":"user","auto_suspend_minutes":30},"initial_message":"hello"}`, sessionID)
		require.NoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: sessionID, Pool: "linux", ProvisionSettings: []byte(settings)}))
		claimed, found, err := store.ClaimNext(ctx, "linux", runnerID, time.Minute)
		require.NoError(t, err)
		require.True(t, found)
		_, err = store.Acknowledge(ctx, claimed.SessionID, runnerID, claimed.LeaseID)
		require.NoError(t, err)
		require.NoError(t, store.MarkStarted(ctx, claimed.SessionID, claimed.Generation))
	}
	store.managerLists, store.poolLists, store.settingReads = 0, 0, 0

	controller := NewSessionPoolController(store, nil)
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		map[string]any{"local_runner_ids": localIDs}, map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())

	require.Equal(t, 1, store.managerLists)
	require.Zero(t, store.poolLists)
	require.Zero(t, store.settingReads)

	var response struct {
		AllocatedRunnerIDs      []string `json:"allocated_runner_ids"`
		AllocatedRunnerPolicies map[string]struct {
			ID                 string `json:"id"`
			UserID             string `json:"user_id"`
			AutoSuspendMinutes int    `json:"auto_suspend_minutes"`
		} `json:"allocated_runner_policies"`
		Pools []struct {
			TotalRunners int `json:"total_runners"`
			IdleRunners  int `json:"idle_runners"`
		} `json:"pools"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &response))
	require.Len(t, response.AllocatedRunnerIDs, sessions)
	require.Len(t, response.AllocatedRunnerPolicies, sessions)
	for runnerID, policy := range response.AllocatedRunnerPolicies {
		require.Equal(t, "alice", policy.UserID, runnerID)
		require.Equal(t, 30, policy.AutoSuspendMinutes, runnerID)
		require.NotEmpty(t, policy.ID, runnerID)
	}
	require.Len(t, response.Pools, 1)
	require.Equal(t, sessions, response.Pools[0].TotalRunners)
	require.Zero(t, response.Pools[0].IdleRunners)
}

func TestMeasureSessionManagerHeartbeat(t *testing.T) {
	if os.Getenv("ALLOCATION_LIST_MEASURE") == "" {
		t.Skip("set ALLOCATION_LIST_MEASURE=1 to run the measurement")
	}
	const settingsBytes = 128 * 1024
	ctx := context.Background()
	for _, sessions := range []int{72, 300, 1000} {
		backend, err := kvstore.NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "heartbeat.db"), "")
		require.NoError(t, err)
		key := make([]byte, 32)
		_, err = rand.Read(key)
		require.NoError(t, err)
		keyring, err := kvstore.NewLocalKeyring("current", map[string]string{"current": base64.StdEncoding.EncodeToString(key)})
		require.NoError(t, err)
		encrypted, err := kvstore.NewEncryptedStore(backend, keyring)
		require.NoError(t, err)
		store := &allocationReadCountingStore{Store: infra.NewStore(encrypted, "test")}

		token, tokenHash, err := newSessionRunnerToken()
		require.NoError(t, err)
		manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
		require.NoError(t, store.CreateManager(ctx, manager))
		require.NoError(t, store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: true}))
		require.NoError(t, store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "linux", ManagerID: manager.ID, Enabled: true}))
		filler := make([]byte, settingsBytes*3/4)
		localIDs := make([]string, 0, sessions)
		for i := range sessions {
			runnerID := fmt.Sprintf("runner-%04d", i)
			sessionID := fmt.Sprintf("session-%04d", i)
			localIDs = append(localIDs, runnerID)
			_, err = rand.Read(filler)
			require.NoError(t, err)
			settings, err := json.Marshal(map[string]any{
				"session":         map[string]any{"id": sessionID, "user_id": "alice", "scope": "user"},
				"initial_message": base64.StdEncoding.EncodeToString(filler),
			})
			require.NoError(t, err)
			require.NoError(t, store.CreateRunner(ctx, &core.Runner{ID: runnerID, ManagerID: manager.ID, Pool: "linux"}))
			require.NoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: sessionID, Pool: "linux", ProvisionSettings: settings}))
			claimed, found, err := store.ClaimNext(ctx, "linux", runnerID, time.Minute)
			require.NoError(t, err)
			require.True(t, found)
			_, err = store.Acknowledge(ctx, claimed.SessionID, runnerID, claimed.LeaseID)
			require.NoError(t, err)
			require.NoError(t, store.MarkStarted(ctx, claimed.SessionID, claimed.Generation))
		}

		controller := NewSessionPoolController(store, nil)
		samples := make([]time.Duration, 0, 5)
		for range 5 {
			store.managerLists = 0
			started := time.Now()
			result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
				map[string]any{"local_runner_ids": localIDs}, map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
			samples = append(samples, time.Since(started))
			require.Equal(t, http.StatusOK, result.Code, result.Body.String())
			require.Equal(t, 1, store.managerLists)
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		t.Logf("runners=%d allocations=%d settings=%dKB  heartbeat median=%s max=%s",
			sessions, sessions, settingsBytes/1024, samples[2].Round(100*time.Microsecond), samples[4].Round(100*time.Microsecond))
		require.NoError(t, encrypted.Close())
	}
}
