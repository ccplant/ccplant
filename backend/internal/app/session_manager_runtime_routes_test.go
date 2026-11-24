package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestSessionManagerRuntimeRouterKeepsHealthPublicAndPrivateAPIProtected(t *testing.T) {
	cfg := &config.Config{}
	cfg.SessionManager.InternalAPIToken = "test-runtime-token"
	router, err := newSessionManagerRuntimeRouter(sessionManagerRouteDependencies{
		config: cfg, manager: &services.KubernetesSessionManager{},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/livez", "/healthz", "/internal/session-manager/v1/health"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		want := http.StatusOK
		if path == "/internal/session-manager/v1/health" {
			want = http.StatusUnauthorized
		}
		if recorder.Code != want {
			t.Errorf("GET %s status = %d, want %d", path, recorder.Code, want)
		}
	}
}

func TestSessionManagerRuntimeRouterStateRoutesRequirePersistence(t *testing.T) {
	cfg := &config.Config{}
	cfg.SessionManager.InternalAPIToken = "test-runtime-token"
	store, err := services.NewSessionStateStore(context.Background(), config.SessionPersistenceConfig{Backend: "volume"})
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		deps := sessionManagerRouteDependencies{config: cfg, manager: &services.KubernetesSessionManager{}}
		if enabled {
			deps.state = store
		}
		router, err := newSessionManagerRuntimeRouter(deps)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, route := range router.Routes() {
			if route.Method == http.MethodPut && route.Path == "/internal/session-state/:sessionId" {
				found = true
			}
		}
		if found != enabled {
			t.Errorf("state route registered = %v, persistence enabled = %v", found, enabled)
		}
	}
}
