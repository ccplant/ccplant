package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/acp/bridge"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type herdrBrowserSession struct {
	mockWaitSession
	addr string
}

func (s *herdrBrowserSession) Addr() string { return s.addr }

// Opt-in local browser harness: real Web UI -> actual session proxy controller
// -> actual ACP runtime routes -> actual Herdr socket/TUI. Identity, logical
// session lookup and parent ACP metadata are fixtures; nested agent IO is real.
// Run with CCPLANT_HERDR_BROWSER_TEST=1, point the UI's AGENTAPI_PROXY_URL to
// http://127.0.0.1:18381 and visit /sessions/herdr-local. Ctrl+C stops the harness.
func TestHerdrLocalBrowser(t *testing.T) {
	if os.Getenv("CCPLANT_HERDR_BROWSER_TEST") != "1" {
		t.Skip("manual local browser harness")
	}
	t.Setenv("AGENTAPI_HERDR_ENABLED", "1")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	runtime := bridge.NewServer(bridge.New(nil, "local-browser", false, "", false), false)
	errors := make(chan error, 2)
	go func() { errors <- runtime.Start(ctx, "127.0.0.1:18382") }()
	e := echo.New()
	e.HideBanner = true
	user := entities.NewUser("local-test-owner", entities.UserTypeRegular, "local-test-owner")
	user.SetPermissions([]entities.Permission{entities.PermissionSessionRead, entities.PermissionSessionCreate})
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Request().Header.Get("Authorization") != "Bearer ccplant-local-herdr-smoke-only" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Local smoke token required")
			}
			c.Set("authz_context", &auth.AuthorizationContext{User: user, PersonalScope: auth.PersonalScopeAuth{UserID: string(user.ID())}})
			return next(c)
		}
	})
	session := &herdrBrowserSession{mockWaitSession: mockWaitSession{id: "herdr-local", userID: string(user.ID())}, addr: "127.0.0.1:18382"}
	controller := NewSessionController(&mockWaitProvider{manager: newMockWaitSessionManager(session)}, nil)
	e.GET("/user/info", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"principal_id": string(user.ID()), "username": "local-test-owner", "teams": []string{}})
	})
	e.GET("/search", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"sessions": []any{map[string]string{"session_id": "herdr-local", "status": "active", "scope": "user", "user_id": string(user.ID())}}})
	})
	// Parent ACP metadata is a fixture: this smoke exercises the nested agents,
	// not an additional paid parent model.
	e.GET("/herdr-local/session", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"sessionId": "local-browser", "status": "ready"})
	})
	e.Any("/:sessionId/*", controller.RouteToSession)
	// Exercise the same ownership/permission gate before serving the browser.
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/herdr-local/herdr/input", strings.NewReader("{}")), httptest.NewRecorder())
	c.SetParamNames("sessionId")
	c.SetParamValues("herdr-local")
	require.Error(t, controller.authorizeHerdrRequest(c))
	go func() { errors <- e.Start("127.0.0.1:18381") }()
	t.Log("Starting Herdr browser harness on 127.0.0.1:18381")
	select {
	case <-ctx.Done():
	case err := <-errors:
		if err != http.ErrServerClosed {
			require.NoError(t, err)
		}
	}
	require.NoError(t, e.Close())
}
