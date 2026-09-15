package app

import (
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/di"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestNewRouterInitializesSessionRuntimeForUnifiedManagerRegistry(t *testing.T) {
	server := &Server{
		config:           &config.Config{},
		container:        &di.Container{},
		esmControlStore:  connectedManagerStore{connected: true},
		sessionRouteRepo: &recordingSessionRouteRepository{},
	}

	router := NewRouter(echo.New(), server)

	if router.handlers.sessionRuntimeController == nil {
		t.Fatal("session runtime controller is nil for unified session-manager registry")
	}
}

// Exercise the production route table, including the catch-all that previously
// interpreted "sessions" as a session ID for these lifecycle requests.
func TestSessionRestartRoutesTakePrecedenceOverSessionProxy(t *testing.T) {
	server := &Server{config: &config.Config{}, container: &di.Container{}}
	e := echo.New()
	router := NewRouter(e, server)
	if err := router.registerCoreRoutes(); err != nil {
		t.Fatal(err)
	}
	router.registerSessionProxyRoutes()
	for _, tc := range []struct{ method, path, want string }{
		{"POST", "/sessions/example/restart", "/sessions/:sessionId/restart"},
		{"GET", "/sessions/example/restart", "/sessions/:sessionId/restart"},
		{"POST", "/sessions/example/pause", "/sessions/:sessionId/pause"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			c := e.NewContext(nil, nil)
			e.Router().Find(tc.method, tc.path, c)
			if c.Path() != tc.want || c.Param("sessionId") != "example" {
				t.Fatalf("request routed to %q with session ID %q", c.Path(), c.Param("sessionId"))
			}
		})
	}
}
