package sessionrunner

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

func newEncryptedLibSQLTestStore(t testing.TB) *Store {
	t.Helper()
	backend, err := kvstore.NewLibSQLStore(context.Background(), "file://"+filepath.Join(t.TempDir(), "allocations.db"), "")
	require.NoError(t, err)
	key := make([]byte, 32)
	_, err = rand.Read(key)
	require.NoError(t, err)
	keyring, err := kvstore.NewLocalKeyring("current", map[string]string{"current": base64.StdEncoding.EncodeToString(key)})
	require.NoError(t, err)
	encrypted, err := kvstore.NewEncryptedStore(backend, keyring)
	require.NoError(t, err)
	t.Cleanup(func() { _ = encrypted.Close() })
	return NewStore(encrypted, "test")
}

func testProvisionSettings(t testing.TB, userID string, payloadBytes int) []byte {
	t.Helper()
	filler := make([]byte, payloadBytes*3/4)
	_, err := rand.Read(filler)
	require.NoError(t, err)
	settings, err := json.Marshal(map[string]any{
		"session":         map[string]any{"user_id": userID, "agent_type": "claude-acp"},
		"initial_message": base64.StdEncoding.EncodeToString(filler),
	})
	require.NoError(t, err)
	return settings
}

func writeLegacyAllocation(t testing.TB, s *Store, allocation *core.Allocation) {
	t.Helper()
	raw, err := json.Marshal(allocation)
	require.NoError(t, err)
	name := allocationName(allocation.SessionID)
	labels := map[string]string{labelResource: "allocation", labelPoolHash: hashName(allocation.Pool)}
	addSessionRunnerQueryLabels(labels, allocation)
	document, err := json.Marshal(secretDocument{APIVersion: "v1", Kind: "Secret",
		Metadata: documentMetadata{Name: name, Namespace: s.namespace, Labels: labels},
		Type:     "Opaque", Data: map[string][]byte{dataKey: raw}})
	require.NoError(t, err)
	_, err = s.kv.Create(context.Background(), kvstore.Record{Kind: kvstore.KindSecret, Namespace: s.namespace, Key: name, Labels: labels, Value: document})
	require.NoError(t, err)
}

func storedAllocation(t testing.TB, s *Store, sessionID string) core.Allocation {
	t.Helper()
	var stored core.Allocation
	_, err := s.get(context.Background(), allocationName(sessionID), &stored)
	require.NoError(t, err)
	return stored
}

func legacyListManagerAllocations(t testing.TB, s *Store, managerID string) []*core.Allocation {
	t.Helper()
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
	ctx := context.Background()
	require.NoError(t, s.listWithSelector(ctx, "allocation", decode, labelManagerHash+"="+hashName(managerID)))
	require.NoError(t, s.listWithSelector(ctx, "allocation", decode, "!"+labelManagerHash))
	return result
}

func TestAllocationSettingsAreStoredOutsideTheAllocationRecord(t *testing.T) {
	ctx := context.Background()
	s := newEncryptedLibSQLTestStore(t)
	settings := testProvisionSettings(t, "alice", 4096)
	require.NoError(t, s.CreateRunner(ctx, &core.Runner{ID: "runner", ManagerID: "manager", Pool: "pool"}))
	require.NoError(t, s.Enqueue(ctx, &core.Allocation{SessionID: "session", Pool: "pool", ProvisionSettings: settings}))

	stored := storedAllocation(t, s, "session")
	require.Empty(t, stored.ProvisionSettings)
	require.JSONEq(t, `{"user_id":"alice","agent_type":"claude-acp"}`, string(stored.SessionMeta))

	fetched, err := s.GetAllocation(ctx, "session")
	require.NoError(t, err)
	require.Equal(t, settings, fetched.ProvisionSettings)

	claimed, found, err := s.ClaimNext(ctx, "pool", "runner", time.Minute)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, settings, claimed.ProvisionSettings)

	listed, err := s.ListManagerAllocations(ctx, "manager")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Empty(t, listed[0].ProvisionSettings)
	require.JSONEq(t, `{"user_id":"alice","agent_type":"claude-acp"}`, string(listed[0].SessionMeta))

	require.ErrorIs(t, s.Enqueue(ctx, &core.Allocation{SessionID: "session", Pool: "pool", ProvisionSettings: []byte(`{"session":{"user_id":"mallory"}}`)}), core.ErrConflict)
	unchanged, err := s.GetAllocation(ctx, "session")
	require.NoError(t, err)
	require.Equal(t, settings, unchanged.ProvisionSettings)

	require.NoError(t, s.DeleteAllocation(ctx, "session"))
	leftover, err := s.loadAllocationSettings(ctx, "session")
	require.NoError(t, err)
	require.Nil(t, leftover)
}

func TestUpdateProvisionSettingsReplacesSettingsAndSessionMeta(t *testing.T) {
	ctx := context.Background()
	s := newEncryptedLibSQLTestStore(t)
	require.NoError(t, s.Enqueue(ctx, &core.Allocation{SessionID: "session", Pool: "pool", ProvisionSettings: testProvisionSettings(t, "alice", 1024)}))

	next := testProvisionSettings(t, "bob", 2048)
	require.NoError(t, s.UpdateProvisionSettings(ctx, "session", next))

	stored := storedAllocation(t, s, "session")
	require.Empty(t, stored.ProvisionSettings)
	require.JSONEq(t, `{"user_id":"bob","agent_type":"claude-acp"}`, string(stored.SessionMeta))
	fetched, err := s.GetAllocation(ctx, "session")
	require.NoError(t, err)
	require.Equal(t, next, fetched.ProvisionSettings)
}

func TestLegacyInlineAllocationStaysReadableAndMigratesOnList(t *testing.T) {
	ctx := context.Background()
	s := newEncryptedLibSQLTestStore(t)
	total := allocationMigrationBatch + 4
	settings := make(map[string][]byte, total)
	for i := range total {
		id := fmt.Sprintf("session-%02d", i)
		settings[id] = testProvisionSettings(t, id, 2048)
		writeLegacyAllocation(t, s, &core.Allocation{SessionID: id, Pool: "pool", ManagerID: "manager", RunnerID: "runner-" + id,
			Status: core.AllocationRunning, Generation: 1, ProvisionSettings: settings[id]})
	}

	before, err := s.GetAllocation(ctx, "session-00")
	require.NoError(t, err)
	require.Equal(t, settings["session-00"], before.ProvisionSettings)

	first, err := s.ListManagerAllocations(ctx, "manager")
	require.NoError(t, err)
	require.Len(t, first, total)
	inline := 0
	for _, allocation := range first {
		if len(allocation.ProvisionSettings) > 0 {
			inline++
		}
	}
	require.Equal(t, total-allocationMigrationBatch, inline)

	second, err := s.ListManagerAllocations(ctx, "manager")
	require.NoError(t, err)
	require.Len(t, second, total)
	for _, allocation := range second {
		require.Empty(t, allocation.ProvisionSettings, allocation.SessionID)
		require.NotEmpty(t, allocation.SessionMeta, allocation.SessionID)
		require.Equal(t, core.AllocationRunning, allocation.Status)
		require.Equal(t, "runner-"+allocation.SessionID, allocation.RunnerID)
		require.Empty(t, storedAllocation(t, s, allocation.SessionID).ProvisionSettings)
		fetched, err := s.GetAllocation(ctx, allocation.SessionID)
		require.NoError(t, err)
		require.Equal(t, settings[allocation.SessionID], fetched.ProvisionSettings)
	}
}

func TestLegacyInlineAllocationIsSplitWhenClaimed(t *testing.T) {
	ctx := context.Background()
	s := newEncryptedLibSQLTestStore(t)
	settings := testProvisionSettings(t, "alice", 2048)
	require.NoError(t, s.CreateRunner(ctx, &core.Runner{ID: "runner", ManagerID: "manager", Pool: "pool"}))
	writeLegacyAllocation(t, s, &core.Allocation{SessionID: "session", Pool: "pool", Status: core.AllocationPending, Generation: 1,
		CreatedAt: time.Now().UTC(), ProvisionSettings: settings})

	claimed, found, err := s.ClaimNext(ctx, "pool", "runner", time.Minute)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, settings, claimed.ProvisionSettings)
	require.Empty(t, storedAllocation(t, s, "session").ProvisionSettings)

	requeued, err := s.RequeueUnstarted(ctx, "session", "runner")
	require.NoError(t, err)
	require.Equal(t, settings, requeued.ProvisionSettings)
}

func TestSessionMetaDroppedByAnOlderWriterIsRestoredOnList(t *testing.T) {
	ctx := context.Background()
	s := newEncryptedLibSQLTestStore(t)
	require.NoError(t, s.Enqueue(ctx, &core.Allocation{SessionID: "session", Pool: "pool", ManagerID: "manager", RunnerID: "runner",
		ProvisionSettings: testProvisionSettings(t, "alice", 1024)}))
	var allocation core.Allocation
	require.NoError(t, s.mutate(ctx, allocationName("session"), &allocation, func() error {
		allocation.SessionMeta = nil
		return nil
	}))
	require.Empty(t, storedAllocation(t, s, "session").SessionMeta)

	listed, err := s.ListManagerAllocations(ctx, "manager")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.JSONEq(t, `{"user_id":"alice","agent_type":"claude-acp"}`, string(listed[0].SessionMeta))
	require.Empty(t, listed[0].ProvisionSettings)
}

func TestMeasureManagerAllocationList(t *testing.T) {
	if os.Getenv("ALLOCATION_LIST_MEASURE") == "" {
		t.Skip("set ALLOCATION_LIST_MEASURE=1 to run the measurement")
	}
	const settingsBytes = 128 * 1024
	ctx := context.Background()
	for _, count := range []int{72, 300, 1000} {
		legacy := newEncryptedLibSQLTestStore(t)
		split := newEncryptedLibSQLTestStore(t)
		for i := range count {
			id := fmt.Sprintf("session-%04d", i)
			settings := testProvisionSettings(t, id, settingsBytes)
			allocation := core.Allocation{SessionID: id, Pool: "pool", ManagerID: "manager", RunnerID: "runner-" + id,
				Status: core.AllocationRunning, Generation: 1, ProvisionSettings: settings}
			legacyCopy := allocation
			writeLegacyAllocation(t, legacy, &legacyCopy)
			require.NoError(t, split.Enqueue(ctx, &allocation))
		}

		median := func(run func()) time.Duration {
			samples := make([]time.Duration, 0, 5)
			for range 5 {
				started := time.Now()
				run()
				samples = append(samples, time.Since(started))
			}
			for i := range samples {
				for j := i + 1; j < len(samples); j++ {
					if samples[j] < samples[i] {
						samples[i], samples[j] = samples[j], samples[i]
					}
				}
			}
			return samples[len(samples)/2]
		}

		before := median(func() {
			require.Len(t, legacyListManagerAllocations(t, legacy, "manager"), count)
			require.Len(t, legacyListManagerAllocations(t, legacy, "manager"), count)
		})
		after := median(func() {
			listed, err := split.ListManagerAllocations(ctx, "manager")
			require.NoError(t, err)
			require.Len(t, listed, count)
		})
		one, err := split.GetAllocation(ctx, "session-0000")
		require.NoError(t, err)
		require.Greater(t, len(one.ProvisionSettings), settingsBytes)
		single := median(func() {
			_, err := split.GetAllocation(ctx, "session-0000")
			require.NoError(t, err)
		})
		t.Logf("allocations=%d settings=%dKB  before(2 full reads)=%s  after(1 light read)=%s  speedup=%.0fx  single settings read=%s",
			count, settingsBytes/1024, before.Round(time.Millisecond), after.Round(100*time.Microsecond), float64(before)/float64(after), single.Round(100*time.Microsecond))
	}
}
