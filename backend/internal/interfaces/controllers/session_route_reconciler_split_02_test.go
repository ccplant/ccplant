package controllers_test

import (
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func TestDeleteRemoteSessionRejectsNonOwner(t *testing.T) {
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "runner-id", ManagerID: "manager-a",
		UserID: "user-2", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, _ := routeContext(echo.New(), http.MethodDelete, "/sessions/public-id", "public-id")

	err := controller.DeleteSession(ctx)
	if he, ok := err.(*echo.HTTPError); !ok || he.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %#v", err)
	}
	if routeRepo.deleted || tunnel.path != "" {
		t.Fatalf("non-owner delete reached storage or manager: deleted=%t path=%q", routeRepo.deleted, tunnel.path)
	}
}

func TestDeleteRemoteSessionFromWorkerBypassesOwnership(t *testing.T) {
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "runner-id", ManagerID: "manager-gone",
		UserID: "user-2", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(&lifecycleTunnel{}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodDelete, "/sessions/public-id", "public-id")

	if err := controller.DeleteSessionFromWorker(ctx); err != nil {
		t.Fatalf("worker delete returned error: %v", err)
	}
	if rec.Code != http.StatusOK || !routeRepo.deleted {
		t.Fatalf("status=%d deleted=%t, want worker cleanup to finalize", rec.Code, routeRepo.deleted)
	}
}
