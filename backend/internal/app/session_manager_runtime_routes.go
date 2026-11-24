package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionmanagerapi"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	externalmanager "github.com/takutakahashi/agentapi-proxy/internal/modules/sessionmanager"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
)

type sessionManagerRouteDependencies struct {
	config         *config.Config
	manager        *services.KubernetesSessionManager
	redis          *redis.Client
	state          services.SessionStateStore
	settings       portrepos.SettingsRepository
	authentication *repositories.KubernetesCredentialsRepository
	control        sessioncontrol.Store
	remote         bool
}

func newSessionManagerRuntimeRouter(deps sessionManagerRouteDependencies) (*echo.Echo, error) {
	cfg, manager, redisClient := deps.config, deps.manager, deps.redis
	stateStore, credentialsRepo := deps.state, deps.authentication
	controlStore, remoteMode := deps.control, deps.remote
	e := echo.New()
	registerSessionManagerHealthRoutes(e, manager, redisClient)
	privateHandler, err := sessionmanagerapi.NewHandler(manager, cfg.SessionManager.InternalAPIToken, sessionmanagerapi.WithSessionStateStore(stateStore))
	if err != nil {
		return nil, err
	}
	privateHandler.RegisterRoutes(e)
	registerSessionManagerProvisionerRoutes(e, deps)
	if controlStore != nil {
		control := controllers.NewSessionControlController(controlStore, manager)
		e.GET("/internal/session-control/:sessionId/commands", control.WaitCommands)
		e.POST("/internal/session-control/:sessionId/events", control.AppendEvents)
	}
	managedFiles := controllers.NewManagedFilesController(manager, credentialsRepo)
	if remoteMode {
		managedFiles.WithProxy(newUpstreamManagedFilesProxy(cfg.SessionManager.UpstreamURL, cfg.SessionManager.ID, cfg.SessionManager.ConnectionToken))
	}
	e.PUT("/internal/session-control/:sessionId/managed-files", managedFiles.Save)
	// Forwarding must follow private callbacks and cannot overlap /internal/*.
	if remoteMode {
		if err := externalmanager.NewHandlers(manager, cfg.SessionManager.HMACSecret, externalmanager.WithSessionStateStore(stateStore)).RegisterRoutes(e); err != nil {
			return nil, fmt.Errorf("register remote session-manager routes: %w", err)
		}
	}
	return e, nil
}

func registerSessionManagerProvisionerRoutes(e *echo.Echo, deps sessionManagerRouteDependencies) {
	// Session Pods authenticate with the narrower provisioner/session token.
	provisioner := controllers.NewProvisionerController(deps.manager, deps.manager, deps.settings, nil, deps.state)
	e.POST("/internal/session-provisioners/connect", provisioner.Connect)
	e.POST("/internal/session-provisioners/:sessionId/claim-pool-stock", provisioner.ClaimPoolStock)
	e.GET("/internal/session-provisioners/:sessionId/provision-requests", provisioner.GetProvisionRequest)
	e.POST("/internal/session-provisioners/:sessionId/provision-requests/:requestId/status", provisioner.UpdateProvisionRequestStatus)
	if deps.state != nil {
		e.PUT("/internal/session-state/:sessionId", provisioner.SaveSessionState)
		e.POST("/internal/session-state/:sessionId/suspend", provisioner.ScheduleSessionSuspend)
		e.POST("/internal/session-state/:sessionId/terminate", provisioner.SuspendSessionOnTermination)
		e.GET("/internal/session-state/:sessionId", provisioner.LoadSessionState)
		e.POST("/internal/session-state/:sessionId/uploads", provisioner.BeginSessionStateUpload)
		e.GET("/internal/session-state/:sessionId/uploads/:uploadId/parts/:partNumber", provisioner.PresignSessionStatePart)
		e.POST("/internal/session-state/:sessionId/uploads/:uploadId/complete", provisioner.CompleteSessionStateUpload)
		e.DELETE("/internal/session-state/:sessionId/uploads/:uploadId", provisioner.AbortSessionStateUpload)
		e.GET("/internal/session-state/:sessionId/download-url", provisioner.PresignSessionStateDownload)
	}
}

func registerSessionManagerHealthRoutes(e *echo.Echo, manager *services.KubernetesSessionManager, redisClient *redis.Client) {
	e.HideBanner = true
	e.Use(otelecho.Middleware("agentapi-session-manager", otelecho.WithSkipper(func(c echo.Context) bool {
		return c.Path() == "/livez" || c.Path() == "/healthz" || c.Path() == "/readyz"
	})))
	e.Use(middleware.Recover())
	e.GET("/livez", func(c echo.Context) error { return c.JSON(http.StatusOK, map[string]string{"status": "ok"}) })
	e.GET("/healthz", func(c echo.Context) error { return c.JSON(http.StatusOK, map[string]string{"status": "ok"}) })
	e.GET("/readyz", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 3*time.Second)
		defer cancel()
		if redisClient != nil {
			if err := redisClient.Ping(ctx).Err(); err != nil {
				return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "redis unavailable"})
			}
		}
		if _, err := manager.GetClient().Discovery().ServerVersion(); err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "kubernetes unavailable"})
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
	})
}
