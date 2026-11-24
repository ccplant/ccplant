package app

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/spec"
)

func (r *Router) registerCoreRoutes() error {
	// Health check endpoint
	r.echo.GET("/health", r.handlers.healthController.HealthCheck)

	// Static file serving for /public/* (no authentication required)
	// Embedded from spec/openapi.json - independent of working directory
	// Must be registered before the /:sessionId/* catch-all route
	r.echo.StaticFS("/public", spec.FS())
	log.Printf("[ROUTES] Static file serving registered at /public/*")

	// ACP (Agent Client Protocol) JSON-RPC 2.0 endpoints
	log.Printf("[ROUTES] Registering ACP endpoints...")
	r.echo.POST("/acp", r.handlers.acpController.HandleRPC, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	r.echo.GET("/acp", r.handlers.acpController.HandleSessionSSE, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	r.echo.OPTIONS("/acp", func(c echo.Context) error {
		log.Printf("[ACP] OPTIONS /acp preflight: origin=%s", c.Request().Header.Get("Origin"))
		c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, Acp-Session-Id")
		c.Response().Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		return c.NoContent(http.StatusNoContent)
	})
	log.Printf("[ROUTES] ACP endpoints registered")

	// Session management routes
	log.Printf("[ROUTES] Registering session management endpoints...")
	r.echo.POST("/start", r.handlers.sessionController.StartSession)
	r.echo.GET("/search", r.handlers.sessionController.SearchSessions)
	if r.handlers.usageController != nil {
		r.echo.POST("/internal/usage-events", r.handlers.usageController.Create,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/usage", r.handlers.usageController.Get,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/usage/export.parquet", r.handlers.usageController.ExportParquet,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/sessions/:sessionId/usage", r.handlers.usageController.GetSession,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	}
	if r.handlers.sessionUsageController != nil {
		r.echo.GET("/session-usage/dashboard", r.handlers.sessionUsageController.GetDashboard,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	}
	r.echo.PATCH("/sessions/:sessionId/annotations", r.handlers.sessionController.UpdateSessionAnnotations)
	r.echo.POST("/sessions/:sessionId/resume", r.handlers.sessionController.ResumeSession)
	r.echo.POST("/sessions/:sessionId/restart", r.handlers.sessionController.RestartSession)
	r.echo.GET("/sessions/:sessionId/restart", r.handlers.sessionController.RestartStatus)
	r.echo.POST("/sessions/:sessionId/pause", r.handlers.sessionController.PauseSession)
	r.echo.POST("/sessions/:sessionId/suspend", r.handlers.sessionController.SuspendSession)
	r.echo.POST("/sessions/:sessionId/templateize", r.handlers.sessionController.TemplateizeSession)
	r.echo.POST("/sessions/:sessionId/workspace", r.handlers.sessionController.SaveSessionAsWorkspace)
	r.echo.GET("/workspaces", r.handlers.sessionController.ListWorkspaces)
	r.echo.GET("/workspaces/:workspaceId", r.handlers.sessionController.GetWorkspace)
	r.echo.PATCH("/workspaces/:workspaceId", r.handlers.sessionController.UpdateWorkspace)
	r.echo.DELETE("/workspaces/:workspaceId", r.handlers.sessionController.DeleteWorkspace)
	r.echo.GET("/session-context-templates", r.handlers.sessionController.ListSessionContextTemplates)
	r.echo.GET("/session-context-templates/:templateId", r.handlers.sessionController.GetSessionContextTemplate)
	r.echo.PATCH("/session-context-templates/:templateId", r.handlers.sessionController.UpdateSessionContextTemplate)
	r.echo.DELETE("/session-context-templates/:templateId", r.handlers.sessionController.DeleteSessionContextTemplate)
	r.echo.DELETE("/sessions/:sessionId", r.handlers.sessionController.DeleteSession)
	if r.handlers.sessionSecretController != nil {
		r.echo.GET("/sessions/:sessionId/secrets", r.handlers.sessionSecretController.List,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/sessions/:sessionId/secrets", r.handlers.sessionSecretController.Create,
			auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/sessions/:sessionId/secrets/:secretId/reauthorize", r.handlers.sessionSecretController.Reauthorize,
			auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/internal/session-control/:sessionId/secrets/next", r.handlers.sessionSecretController.ConsumeNext)
		r.echo.GET("/internal/session-control/:sessionId/secrets/:secretId", r.handlers.sessionSecretController.Consume)
	}
	if r.handlers.sessionPoolController != nil {
		r.echo.GET("/available-session-pools", r.handlers.sessionPoolController.ListAvailablePools,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/internal/session-runners/register", r.handlers.sessionPoolController.RegisterRunner)
		r.echo.GET("/internal/session-runners/allocations/next", r.handlers.sessionPoolController.ClaimRunnerAllocation)
		r.echo.POST("/internal/session-runners/allocations/:sessionId/ack", r.handlers.sessionPoolController.AckRunnerAllocation)
		r.echo.POST("/internal/session-runners/allocations/:sessionId/fail", r.handlers.sessionPoolController.FailRunnerAllocation)
		r.echo.POST("/internal/session-managers/:id/runners/:runnerId/retire", r.handlers.sessionPoolController.RetireRunner)
		r.echo.POST("/internal/session-managers/:id/heartbeat", r.handlers.sessionPoolController.HeartbeatManager)
		r.echo.GET("/internal/session-managers/:id/runtime-profile", r.handlers.sessionPoolController.GetManagerRuntimeProfile)
		r.echo.PUT("/internal/session-managers/:id/sessions/:sessionId/managed-files", r.handlers.sessionPoolController.SaveManagerManagedFiles)
		r.echo.GET("/internal/session-managers/:id/version", r.handlers.sessionPoolController.GetManagerVersion)
	}

	// Proxy-wide session status push endpoints (registered before /:sessionId/* catch-all)
	r.echo.GET("/sessions/status/stream", r.handlers.sessionController.StreamSessionsStatus)
	r.echo.GET("/sessions/status/wait", r.handlers.sessionController.WaitSessionsStatus)
	// Per-session message update long-poll endpoint (must be before /:sessionId/* catch-all)
	r.echo.GET("/sessions/:sessionId/messages/wait", r.handlers.sessionController.WaitSessionMessages)
	// Sandbox domain viewer (must be before /:sessionId/* catch-all)
	r.echo.GET("/sessions/:sessionId/sandbox-domains", r.handlers.sessionController.GetSessionSandboxDomains,
		auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	log.Printf("[ROUTES] Session status/message push endpoints registered (SSE + long-poll)")

	if r.handlers.resourceTransferController != nil {
		log.Printf("[ROUTES] Registering resource transfer endpoint...")
		r.echo.POST("/resources/transfer", r.handlers.resourceTransferController.TransferResource, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/resources/*", func(c echo.Context) error {
			if c.Param("*") != "transfer" {
				return echo.NewHTTPError(http.StatusNotFound, "resource endpoint not found")
			}
			return r.handlers.resourceTransferController.TransferResource(c)
		}, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Resource transfer endpoint registered")
	}

	if r.handlers.provisionerController != nil {
		r.echo.POST("/internal/session-provisioners/connect", r.handlers.provisionerController.Connect)
		r.echo.GET("/internal/session-provisioners/:sessionId/provision-requests", r.handlers.provisionerController.GetProvisionRequest)
		r.echo.POST("/internal/session-provisioners/:sessionId/provision-requests/:requestId/status", r.handlers.provisionerController.UpdateProvisionRequestStatus)
		r.echo.GET("/internal/session-allocations/next", r.handlers.provisionerController.GetNextSessionAllocation)
		r.echo.POST("/internal/session-allocations/:sessionId/result", r.handlers.provisionerController.CompleteSessionAllocation)
		r.echo.PUT("/internal/session-state/:sessionId", r.handlers.provisionerController.SaveSessionState)
		r.echo.POST("/internal/session-state/:sessionId/suspend", r.handlers.provisionerController.ScheduleSessionSuspend)
		r.echo.POST("/internal/session-state/:sessionId/terminate", r.handlers.provisionerController.SuspendSessionOnTermination)
		r.echo.GET("/internal/session-state/:sessionId", r.handlers.provisionerController.LoadSessionState)
		r.echo.POST("/internal/session-state/:sessionId/uploads", r.handlers.provisionerController.BeginSessionStateUpload)
		r.echo.GET("/internal/session-state/:sessionId/uploads/:uploadId/parts/:partNumber", r.handlers.provisionerController.PresignSessionStatePart)
		r.echo.POST("/internal/session-state/:sessionId/uploads/:uploadId/complete", r.handlers.provisionerController.CompleteSessionStateUpload)
		r.echo.DELETE("/internal/session-state/:sessionId/uploads/:uploadId", r.handlers.provisionerController.AbortSessionStateUpload)
		r.echo.GET("/internal/session-state/:sessionId/download-url", r.handlers.provisionerController.PresignSessionStateDownload)
		log.Printf("[ROUTES] Internal provisioner endpoints registered")
	}
	if r.handlers.workerControlController != nil {
		worker := r.echo.Group("/internal/worker", r.handlers.workerControlController.Authenticate)
		worker.POST("/sessions/:sessionId", r.handlers.workerControlController.CreateSession)
		worker.GET("/sessions", r.handlers.workerControlController.ListSessions)
		worker.DELETE("/sessions/:sessionId", r.handlers.workerControlController.DeleteSession)
		worker.POST("/sessions/:sessionId/messages", r.handlers.workerControlController.SendMessage)
		worker.POST("/sessions/:sessionId/stop", r.handlers.workerControlController.StopAgent)
		worker.GET("/stock", r.handlers.workerControlController.Stock)
		worker.POST("/stock", r.handlers.workerControlController.Stock)
		worker.DELETE("/stock", r.handlers.workerControlController.Stock)
		worker.POST("/leases/:leaseName", r.handlers.workerControlController.Lease)
		worker.POST("/schedules/claim-due", r.handlers.workerControlController.ClaimDueSchedules)
		worker.POST("/schedules/:id/finalize", r.handlers.workerControlController.FinalizeSchedule)
		worker.GET("/slackbots", r.handlers.workerControlController.ListSlackBots)
		worker.GET("/slackbots/:id/credentials", r.handlers.workerControlController.GetSlackBotCredentials)
		log.Printf("[ROUTES] Isolated worker-control endpoints registered")
	}
	if r.handlers.sessionControlController != nil {
		r.echo.GET("/internal/session-control/:sessionId/commands", r.handlers.sessionControlController.WaitCommands)
		r.echo.POST("/internal/session-control/:sessionId/events", r.handlers.sessionControlController.AppendEvents)
		log.Printf("[ROUTES] Internal session control long-poll endpoints registered")
	}
	if r.handlers.sessionControlReaderController != nil {
		r.echo.GET("/sessions/:sessionId/control/events/wait", r.handlers.sessionControlReaderController.WaitEvents,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	}
	if r.handlers.esmControlController != nil {
		r.echo.GET("/internal/external-session-manager/control/commands", r.handlers.esmControlController.WaitCommands)
		r.echo.POST("/internal/external-session-manager/control/frames", r.handlers.esmControlController.AppendFrames)
		r.echo.GET("/internal/external-session-managers/:managerId/control/commands", r.handlers.esmControlController.WaitCommands)
		r.echo.POST("/internal/external-session-managers/:managerId/control/frames", r.handlers.esmControlController.AppendFrames)
		log.Printf("[ROUTES] Internal outbound ESM control endpoints registered")
	}
	if r.handlers.sessionRuntimeController != nil {
		r.echo.GET("/internal/session-runtime/:sessionId/requests", r.handlers.sessionRuntimeController.WaitRequests)
		r.echo.POST("/internal/session-runtime/:sessionId/frames", r.handlers.sessionRuntimeController.AppendFrames)
		r.echo.POST("/internal/session-runtime/:sessionId/status", r.handlers.sessionRuntimeController.UpdateStatus)
		r.echo.POST("/internal/session-runtime/:sessionId/checkpoint", r.handlers.sessionRuntimeController.Checkpoint)
		log.Printf("[ROUTES] Direct Session Pod runtime endpoints registered")
	}

	// Session sharing routes
	if r.handlers.shareController != nil {
		log.Printf("[ROUTES] Registering session sharing endpoints...")
		r.echo.POST("/sessions/:sessionId/share", r.handlers.shareController.CreateShare)
		r.echo.GET("/sessions/:sessionId/share", r.handlers.shareController.GetShare)
		r.echo.DELETE("/sessions/:sessionId/share", r.handlers.shareController.DeleteShare)
		// Add OPTIONS handler for session share endpoints (CORS preflight)
		r.echo.OPTIONS("/sessions/:sessionId/share", func(c echo.Context) error {
			c.Response().Header().Set("Access-Control-Allow-Origin", "*")
			c.Response().Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
			c.Response().Header().Set("Access-Control-Max-Age", "86400")
			return c.NoContent(http.StatusNoContent)
		})
		// Shared session access route (read-only)
		r.echo.Any("/s/:shareToken/*", r.handlers.shareController.RouteToSharedSession)
		r.echo.OPTIONS("/s/:shareToken/*", func(c echo.Context) error {
			c.Response().Header().Set("Access-Control-Allow-Origin", "*")
			c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
			c.Response().Header().Set("Access-Control-Max-Age", "86400")
			return c.NoContent(http.StatusNoContent)
		})
		log.Printf("[ROUTES] Session sharing endpoints registered")
	}

	// Add explicit OPTIONS handler for DELETE endpoint to ensure CORS preflight works
	r.echo.OPTIONS("/sessions/:sessionId", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	log.Printf("[ROUTES] Session management endpoints registered")

	return nil
}
