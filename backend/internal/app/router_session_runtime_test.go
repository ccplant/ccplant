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
