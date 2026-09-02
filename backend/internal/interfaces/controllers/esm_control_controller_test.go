package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	"k8s.io/client-go/kubernetes/fake"
)

func TestESMControlAuthorizesUnifiedManagerConnectionToken(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(context.Background(), manager); err != nil {
		t.Fatal(err)
	}
	controller := NewESMControlController(nil, nil, store)

	req := httptest.NewRequest(http.MethodGet, "/internal/external-session-manager/control/commands", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	ctx := echo.New().NewContext(req, httptest.NewRecorder())
	managerID, ok := controller.authorize(ctx)
	if !ok || managerID != manager.ID {
		t.Fatalf("authorize() = (%q, %v), want (%q, true)", managerID, ok, manager.ID)
	}

	req = httptest.NewRequest(http.MethodGet, "/internal/external-session-manager/control/commands", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	ctx = echo.New().NewContext(req, httptest.NewRecorder())
	if managerID, ok := controller.authorize(ctx); ok {
		t.Fatalf("authorize() accepted wrong token for manager %q", managerID)
	}
}
