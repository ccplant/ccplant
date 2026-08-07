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
	if err := repo.Save(ctx, &portrepos.SessionRoute{SessionID: "session-a", ManagerID: "manager-a", RemoteSessionID: "remote-a"}); err != nil {
		t.Fatal(err)
	}
	route, err := repo.Get(ctx, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if route == nil || route.RemoteSessionID != "remote-a" {
		t.Fatalf("route was not updated: %#v", route)
	}
}
