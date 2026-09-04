package repositories

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func TestKubernetesSessionRouteRepositorySaveUpdatesExistingSecret(t *testing.T) {
	repo := NewKubernetesSessionRouteRepository(fake.NewSimpleClientset(), "test")
	ctx := context.Background()
	if err := repo.Save(ctx, &portrepos.SessionRoute{SessionID: "session-a", ManagerID: "manager-a"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, &portrepos.SessionRoute{SessionID: "session-a", ManagerID: "manager-a", RemoteSessionID: "remote-a", Transport: "direct_session_runtime", RuntimeTokenHash: "hash-a", Generation: 2}); err != nil {
		t.Fatal(err)
	}
	route, err := repo.Get(ctx, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if route == nil || route.RemoteSessionID != "remote-a" || route.Transport != "direct_session_runtime" || route.RuntimeTokenHash != "hash-a" || route.Generation != 2 {
		t.Fatalf("route was not updated: %#v", route)
	}
}

func TestKubernetesSessionRouteRepositoryCachesGets(t *testing.T) {
	client := fake.NewSimpleClientset()
	writer := NewKubernetesSessionRouteRepository(client, "test")
	ctx := context.Background()
	if err := writer.Save(ctx, &portrepos.SessionRoute{SessionID: "session-a", ManagerID: "manager-a", Tags: map[string]string{"env": "test"}}); err != nil {
		t.Fatal(err)
	}
	repo := NewKubernetesSessionRouteRepository(client, "test")
	before := len(client.Actions())
	first, err := repo.Get(ctx, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	first.Tags["env"] = "mutated"
	second, err := repo.Get(ctx, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(client.Actions()) - before; got != 1 {
		t.Fatalf("Kubernetes calls = %d, want 1", got)
	}
	if second.Tags["env"] != "test" {
		t.Fatalf("cached route was mutated: %#v", second.Tags)
	}
}
