package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes/fake"
)

func TestManagerHeartbeatStillReplacesErrorOfNonDirectRoute(t *testing.T) {
	ctx := context.Background()
	routes := repositories.NewKubernetesSessionRouteRepository(fake.NewSimpleClientset(), "test")
	route := &portrepos.SessionRoute{
		SessionID: "public", RemoteSessionID: "runtime", ManagerID: "manager",
		Status: "error", StatusUpdatedAt: time.Now().Add(-time.Minute).UTC(),
	}
	if err := routes.Save(ctx, route); err != nil {
		t.Fatal(err)
	}
	controller := NewSessionPoolController(nil, routes)
	if err := controller.reconcileManagerSessionStatuses(ctx, "manager", map[string]string{"runtime": "active"}); err != nil {
		t.Fatal(err)
	}
	got, err := routes.Get(ctx, "public")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "active" {
		t.Fatalf("status=%s, want active", got.Status)
	}
}
