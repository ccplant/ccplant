package controllers

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes/fake"
)

type memoryRouteStatusBus struct {
	mu     sync.Mutex
	nextID int
	subs   map[int]chan portrepos.StatusChangeEvent
}

func newMemoryRouteStatusBus() *memoryRouteStatusBus {
	return &memoryRouteStatusBus{subs: map[int]chan portrepos.StatusChangeEvent{}}
}

func (b *memoryRouteStatusBus) PublishRouteStatusChange(_ context.Context, event portrepos.StatusChangeEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- event:
		default:
		}
	}
	return nil
}

func (b *memoryRouteStatusBus) SubscribeRouteStatus(ctx context.Context) (<-chan portrepos.StatusChangeEvent, error) {
	ch := make(chan portrepos.StatusChangeEvent, 16)
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subs[id] = ch
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
		close(ch)
	}()
	return ch, nil
}

func (b *memoryRouteStatusBus) waitSubscribers(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b.mu.Lock()
		got := len(b.subs)
		b.mu.Unlock()
		if got >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("route status subscribers = %d, want %d", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newRouteStatusTestReplica(t *testing.T, routes portrepos.SessionRouteRepository, bus portrepos.RouteStatusEventBus) *SessionController {
	t.Helper()
	controller := NewSessionController(nil, nil, WithSessionRouteRepository(routes), WithRouteStatusEventBus(bus))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	controller.StartRouteStatusRelay(ctx)
	return controller
}

func saveRouteStatusTestRoute(t *testing.T, routes portrepos.SessionRouteRepository, route *portrepos.SessionRoute) {
	t.Helper()
	if err := routes.Save(context.Background(), route); err != nil {
		t.Fatal(err)
	}
}

func expectRouteStatusEvent(t *testing.T, events <-chan portrepos.SessionStatusEvent, sessionID, status string) {
	t.Helper()
	select {
	case evt := <-events:
		if evt.SessionID != sessionID || evt.Status != status {
			t.Fatalf("event = %s/%s, want %s/%s", evt.SessionID, evt.Status, sessionID, status)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no event for %s/%s", sessionID, status)
	}
}

func expectNoRouteStatusEvent(t *testing.T, events <-chan portrepos.SessionStatusEvent) {
	t.Helper()
	select {
	case evt := <-events:
		t.Fatalf("unexpected event %s/%s", evt.SessionID, evt.Status)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestRemoteStatusReachesSubscribersOnOtherReplicas(t *testing.T) {
	bus := newMemoryRouteStatusBus()
	routes := repositories.NewKubernetesSessionRouteRepository(fake.NewSimpleClientset(), "test")
	route := &portrepos.SessionRoute{SessionID: "s1", Status: "starting", StatusUpdatedAt: time.Now()}
	saveRouteStatusTestRoute(t, routes, route)

	replicaA := newRouteStatusTestReplica(t, routes, bus)
	replicaB := newRouteStatusTestReplica(t, routes, bus)
	replicaC := newRouteStatusTestReplica(t, routes, bus)
	bus.waitSubscribers(t, 3)

	browserOnA, cancelA := replicaA.subscribeRemoteStatusEvents()
	defer cancelA()
	browserOnB, cancelB := replicaB.subscribeRemoteStatusEvents()
	defer cancelB()
	browserOnC, cancelC := replicaC.subscribeRemoteStatusEvents()
	defer cancelC()

	if err := replicaA.RecordRemoteSessionStatus(context.Background(), route, "stable"); err != nil {
		t.Fatal(err)
	}

	expectRouteStatusEvent(t, browserOnA, "s1", "active")
	expectRouteStatusEvent(t, browserOnB, "s1", "active")
	expectRouteStatusEvent(t, browserOnC, "s1", "active")
	expectNoRouteStatusEvent(t, browserOnA)
	expectNoRouteStatusEvent(t, browserOnB)
}

func TestRemoteStatusStaysProcessLocalWithoutRouteStatusBus(t *testing.T) {
	routes := repositories.NewKubernetesSessionRouteRepository(fake.NewSimpleClientset(), "test")
	route := &portrepos.SessionRoute{SessionID: "s1", Status: "starting", StatusUpdatedAt: time.Now()}
	saveRouteStatusTestRoute(t, routes, route)

	replicaA := NewSessionController(nil, nil, WithSessionRouteRepository(routes))
	replicaB := NewSessionController(nil, nil, WithSessionRouteRepository(routes))
	replicaA.StartRouteStatusRelay(context.Background())

	browserOnA, cancelA := replicaA.subscribeRemoteStatusEvents()
	defer cancelA()
	browserOnB, cancelB := replicaB.subscribeRemoteStatusEvents()
	defer cancelB()

	if err := replicaA.RecordRemoteSessionStatus(context.Background(), route, "stable"); err != nil {
		t.Fatal(err)
	}

	expectRouteStatusEvent(t, browserOnA, "s1", "active")
	expectNoRouteStatusEvent(t, browserOnA)
	expectNoRouteStatusEvent(t, browserOnB)
}

func TestManagerHeartbeatStatusChangeReachesOtherReplicas(t *testing.T) {
	ctx := context.Background()
	bus := newMemoryRouteStatusBus()
	routes := repositories.NewKubernetesSessionRouteRepository(fake.NewSimpleClientset(), "test")
	saveRouteStatusTestRoute(t, routes, &portrepos.SessionRoute{
		SessionID: "s1", RemoteSessionID: "runtime", ManagerID: "manager",
		Status: "starting", StatusUpdatedAt: time.Now().Add(-time.Minute).UTC(),
	})

	replicaA := newRouteStatusTestReplica(t, routes, bus)
	replicaB := newRouteStatusTestReplica(t, routes, bus)
	bus.waitSubscribers(t, 2)
	pool := NewSessionPoolController(nil, routes).WithRouteStatusPublisher(replicaA)

	browserOnA, cancelA := replicaA.subscribeRemoteStatusEvents()
	defer cancelA()
	browserOnB, cancelB := replicaB.subscribeRemoteStatusEvents()
	defer cancelB()

	if err := pool.reconcileManagerSessionStatuses(ctx, "manager", map[string]string{"runtime": "stable"}); err != nil {
		t.Fatal(err)
	}
	expectRouteStatusEvent(t, browserOnA, "s1", "active")
	expectRouteStatusEvent(t, browserOnB, "s1", "active")

	if err := pool.reconcileManagerSessionStatuses(ctx, "manager", map[string]string{"runtime": "stable"}); err != nil {
		t.Fatal(err)
	}
	expectNoRouteStatusEvent(t, browserOnA)
	expectNoRouteStatusEvent(t, browserOnB)
}
