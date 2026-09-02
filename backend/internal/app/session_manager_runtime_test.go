package app

import (
	"context"
	"errors"
	"testing"
	"time"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestNewSessionManagerRuntimeRequiresRedis(t *testing.T) {
	_, err := NewSessionManagerRuntime(context.Background(), &config.Config{}, false)
	if err == nil || err.Error() != "session-manager Redis is required" {
		t.Fatalf("error = %v, want Redis required", err)
	}
}

func TestNewSessionManagerRuntimeRemoteModeDoesNotRequireRedis(t *testing.T) {
	cfg := &config.Config{}
	cfg.SessionManager.UpstreamURL = "https://parent.example.com"
	cfg.SessionManager.ConnectionToken = "connection-token"
	cfg.SessionManager.ID = "manager-id"
	cfg.SessionManager.RunnerPool = "managed"

	_, err := NewSessionManagerRuntime(context.Background(), cfg, false)
	if err == nil || err.Error() != "session-manager internal API token is required" {
		t.Fatalf("error = %v, want internal API token required after Redis validation is skipped", err)
	}
}

type fakeRunnerInfrastructure struct {
	idle, total int
	created     int
	registered  map[string]struct{}
	operations  []string
}

func (f *fakeRunnerInfrastructure) DeleteRunnerSessionsNotRegistered(_ context.Context, registered map[string]struct{}) error {
	f.registered = registered
	f.operations = append(f.operations, "cleanup")
	return nil
}

func (f *fakeRunnerInfrastructure) CountStockSessionsForPool(context.Context, string, bool) (int, error) {
	return f.idle, nil
}

func (f *fakeRunnerInfrastructure) CountRunnerSessionsForPool(context.Context, string) (int, error) {
	return f.total, nil
}

func (f *fakeRunnerInfrastructure) CreateStockSessionForPool(context.Context, string, bool) error {
	f.created++
	f.operations = append(f.operations, "create")
	return nil
}

func TestReconcileSessionRunnerHeartbeatCleansBeforeReplenishing(t *testing.T) {
	manager := &fakeRunnerInfrastructure{}
	registered := []string{"existing-runner"}
	reconcileSessionRunnerHeartbeat(context.Background(), manager, []*sessionrunnercore.PoolSupplier{{
		Pool: "managed", Enabled: true, MinIdle: 1, MaxRunners: 20,
	}}, &registered)

	if len(manager.operations) != 2 || manager.operations[0] != "cleanup" || manager.operations[1] != "create" {
		t.Fatalf("operations = %v, want [cleanup create]", manager.operations)
	}
	if manager.created != 1 {
		t.Fatalf("created %d runners, want 1", manager.created)
	}
}

func TestReconcileSessionRunnerPoolsUsesInfrastructureInventory(t *testing.T) {
	manager := &fakeRunnerInfrastructure{}
	reconcileSessionRunnerPools(context.Background(), manager, []*sessionrunnercore.PoolSupplier{{
		Pool: "managed", Enabled: true, MinIdle: 3, MaxRunners: 20,
		// The upstream registry is deliberately stale. Local infrastructure is
		// authoritative for capacity reconciliation.
		IdleRunners: 3, TotalRunners: 3,
	}})
	if manager.created != 3 {
		t.Fatalf("created %d runners, want 3", manager.created)
	}
}

func TestReconcileSessionRunnerPoolsReplacesLocallyStaleRunner(t *testing.T) {
	manager := &fakeRunnerInfrastructure{idle: 1, total: 1}
	reconcileSessionRunnerPools(context.Background(), manager, []*sessionrunnercore.PoolSupplier{{
		Pool: "managed", Enabled: true, MinIdle: 1, MaxRunners: 20,
		IdleRunners: 0, TotalRunners: 0,
	}})
	if manager.created != 1 {
		t.Fatalf("created %d runners, want 1", manager.created)
	}
}

func TestReconcileOrphanedSessionRunnersUsesParentInventory(t *testing.T) {
	manager := &fakeRunnerInfrastructure{}
	reconcileOrphanedSessionRunners(context.Background(), manager, []string{"runner-a", "runner-b"})
	if len(manager.registered) != 2 {
		t.Fatalf("registered runner count = %d, want 2", len(manager.registered))
	}
	if _, ok := manager.registered["runner-a"]; !ok {
		t.Fatal("runner-a missing from registered inventory")
	}
}

type durableAllocationQueue struct {
	allocation *coreallocation.AllocationRequest
	getErr     error
	completed  bool
}

func (q *durableAllocationQueue) SubmitExternalSessionAllocation(context.Context, string, string, *sessionsettings.SessionSettings, *entities.RunServerRequest, *coreallocation.RuntimeBootstrap) error {
	return nil
}

func (q *durableAllocationQueue) GetSessionAllocation(context.Context, string) (*coreallocation.AllocationRequest, error) {
	return q.allocation, q.getErr
}

func (q *durableAllocationQueue) NextSessionAllocation(context.Context, time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	return q.allocation, q.allocation != nil, nil
}

func (q *durableAllocationQueue) CompleteSessionAllocation(context.Context, string, coreallocation.AllocationResult) (*coreallocation.AllocationRequest, error) {
	q.completed = true
	return q.allocation, nil
}

func (q *durableAllocationQueue) NextExternalSessionAllocation(context.Context, string, time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	return nil, false, nil
}

func (q *durableAllocationQueue) CompleteExternalSessionAllocation(context.Context, string, coreallocation.AllocationResult) (*coreallocation.AllocationRequest, error) {
	return nil, nil
}

type recordingSessionRouteRepository struct {
	route   *portrepos.SessionRoute
	saveErr error
}

func (r *recordingSessionRouteRepository) Save(_ context.Context, route *portrepos.SessionRoute) error {
	r.route = route
	return r.saveErr
}

func (r *recordingSessionRouteRepository) Get(context.Context, string) (*portrepos.SessionRoute, error) {
	return nil, nil
}

func (r *recordingSessionRouteRepository) List(context.Context, string) ([]*portrepos.SessionRoute, error) {
	return nil, nil
}

func (r *recordingSessionRouteRepository) Delete(context.Context, string) error { return nil }

func TestLocalAllocationClientCompleteRestoresRouteFromDurableAllocation(t *testing.T) {
	queue := &durableAllocationQueue{allocation: &coreallocation.AllocationRequest{
		SessionID: "public-id",
		Request: &entities.RunServerRequest{
			UserID:         "user-id",
			Scope:          entities.ScopeTeam,
			TeamID:         "org/team",
			Tags:           map[string]string{"source": "webhook"},
			InitialMessage: "review this",
		},
	}}
	routes := &recordingSessionRouteRepository{}
	client := newLocalAllocationClient(queue, routes)

	err := client.Complete(context.Background(), "public-id", coreallocation.AllocationResult{
		Status:             coreallocation.StatusAssigned,
		AllocatedSessionID: "runtime-id",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if routes.route == nil {
		t.Fatal("Complete() did not persist a session route")
	}
	if routes.route.SessionID != "public-id" || routes.route.RemoteSessionID != "runtime-id" {
		t.Fatalf("route IDs = %q -> %q", routes.route.SessionID, routes.route.RemoteSessionID)
	}
	if routes.route.UserID != "user-id" || routes.route.Scope != string(entities.ScopeTeam) || routes.route.TeamID != "org/team" {
		t.Fatalf("route owner metadata = %#v", routes.route)
	}
	if routes.route.InitialMessage != "review this" || routes.route.Tags["source"] != "webhook" {
		t.Fatalf("route session metadata = %#v", routes.route)
	}
	if !queue.completed {
		t.Fatal("Complete() did not acknowledge the allocation")
	}
}

func TestRunLeaderElectionUntilCanceledRetriesAfterElectionStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runs := make(chan int, 2)
	runCount := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		runLeaderElectionUntilCanceled(ctx, time.Millisecond, func(context.Context) {
			runCount++
			runs <- runCount
			if runCount == 2 {
				cancel()
			}
		})
	}()

	for want := 1; want <= 2; want++ {
		select {
		case got := <-runs:
			if got != want {
				t.Fatalf("election run = %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for election run %d", want)
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("leader election retry loop did not stop after cancellation")
	}
}

func TestLocalAllocationClientCompleteKeepsAllocationWhenRouteSaveFails(t *testing.T) {
	queue := &durableAllocationQueue{allocation: &coreallocation.AllocationRequest{SessionID: "public-id"}}
	routes := &recordingSessionRouteRepository{saveErr: errors.New("route unavailable")}
	client := newLocalAllocationClient(queue, routes)

	err := client.Complete(context.Background(), "public-id", coreallocation.AllocationResult{
		Status:             coreallocation.StatusAssigned,
		AllocatedSessionID: "runtime-id",
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want route save error")
	}
	if queue.completed {
		t.Fatal("Complete() acknowledged allocation after route save failure")
	}
}

func TestLocalAllocationClientCompleteFailsWhenDurableAllocationIsMissing(t *testing.T) {
	queue := &durableAllocationQueue{getErr: errors.New("not found")}
	client := newLocalAllocationClient(queue, &recordingSessionRouteRepository{})

	err := client.Complete(context.Background(), "public-id", coreallocation.AllocationResult{
		Status:             coreallocation.StatusAssigned,
		AllocatedSessionID: "runtime-id",
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want durable allocation error")
	}
	if queue.completed {
		t.Fatal("Complete() acknowledged missing allocation")
	}
}
