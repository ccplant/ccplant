package app

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func (r *Router) registerConditionalRoutes() error {
	// User info endpoint (requires authentication)
	log.Printf("[ROUTES] Registering user info endpoint...")
	r.echo.GET("/user/info", r.handlers.userController.GetUserInfo, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	r.echo.GET("/teams", r.handlers.teamConfigController.List, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	r.echo.POST("/teams", r.handlers.teamConfigController.Create, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
	r.echo.PATCH("/teams/:team", r.handlers.teamConfigController.Rename, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
	r.echo.DELETE("/teams/:team", r.handlers.teamConfigController.Delete, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
	r.echo.GET("/teams/:team/config", r.handlers.teamConfigController.Get, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	r.echo.PUT("/teams/:team/config", r.handlers.teamConfigController.Update, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
	if r.handlers.teamMembershipController != nil {
		r.echo.GET("/teams/:team/members", r.handlers.teamMembershipController.Get, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/teams/:team/members/sync", r.handlers.teamMembershipController.Sync, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
	}
	log.Printf("[ROUTES] User info endpoint registered")

	// Add notification routes if service is available
	if r.server.notificationSvc != nil {
		log.Printf("[ROUTES] Registering notification endpoints...")
		// UI-compatible routes (proxied from agentapi-ui)
		r.echo.POST("/notification/subscribe", r.handlers.notificationHandlers.Subscribe, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/notification/subscribe", r.handlers.notificationHandlers.GetSubscriptions, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.DELETE("/notification/subscribe", r.handlers.notificationHandlers.DeleteSubscription, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))

		// Internal routes
		r.echo.POST("/notifications/webhook", r.handlers.notificationHandlers.Webhook, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/notifications/history", r.handlers.notificationHandlers.GetHistory, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/notifications/send", r.handlers.notificationHandlers.SendNotification, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		log.Printf("[ROUTES] Notification endpoints registered")
	} else {
		log.Printf("[ROUTES] Notification service not available, skipping notification routes")
	}

	// Add settings routes if settings repository is available (Kubernetes mode only)
	if r.server.settingsRepo != nil && r.handlers.settingsController != nil {
		log.Printf("[ROUTES] Registering settings endpoints...")
		r.echo.GET("/settings/managers", r.handlers.settingsController.GetAvailableManagers, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/settings/:name/secrets", r.handlers.settingsController.ListSecrets, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/settings/:name/secrets", r.handlers.settingsController.CreateSecret, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.PATCH("/settings/:name/secrets/:secretId", r.handlers.settingsController.UpdateSecret, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.PUT("/settings/:name/secrets/:secretId/values", r.handlers.settingsController.UpdateSecretValues, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/settings/:name/secrets/:secretId", r.handlers.settingsController.DeleteSecret, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/settings/:name", r.handlers.settingsController.GetSettings, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/settings/:name", r.handlers.settingsController.UpdateSettings, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/settings/:name", r.handlers.settingsController.DeleteSettings, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Settings endpoints registered")
	} else {
		log.Printf("[ROUTES] Settings repository not available, skipping settings routes")
	}

	if r.handlers.adminSettingsController != nil {
		log.Printf("[ROUTES] Registering admin system settings endpoints...")
		r.echo.GET("/admin/system-settings", r.handlers.adminSettingsController.Get, auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService))
		r.echo.GET("/admin/system-settings/versions", r.handlers.adminSettingsController.ListVersions, auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService))
		r.echo.PUT("/admin/system-settings", r.handlers.adminSettingsController.Put, auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService))
	}
	if r.handlers.adminLocalUserController != nil {
		admin := auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService)
		c := r.handlers.adminLocalUserController
		r.echo.POST("/admin/users", c.Create, admin)
		r.echo.GET("/admin/users/:id", c.Get, admin)
		r.echo.POST("/admin/users/:id/api-tokens", c.CreateToken, admin)
		r.echo.GET("/admin/users/:id/api-tokens", c.ListTokens, admin)
		r.echo.DELETE("/admin/users/:id/api-tokens/:tokenId", c.DeleteToken, admin)
	}
	if r.handlers.githubConnectionsController != nil {
		admin := auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService)
		read := auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService)
		write := auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService)
		controller := r.handlers.githubConnectionsController
		r.echo.GET("/github-connections/login-options", controller.ListLoginOptions)
		r.echo.POST("/github-connections/login", controller.StartLogin)
		r.echo.GET("/admin/github-connections", controller.List, admin)
		r.echo.POST("/admin/github-connections", controller.Create, admin)
		r.echo.GET("/admin/github-connections/:id", controller.Get, admin)
		r.echo.PATCH("/admin/github-connections/:id", controller.Update, admin)
		r.echo.DELETE("/admin/github-connections/:id", controller.Delete, admin)
		r.echo.PUT("/admin/github-connections/:id/secret", controller.UpdateSecret, admin)
		r.echo.DELETE("/admin/github-connections/:id/secret", controller.DeleteSecret, admin)
		r.echo.POST("/admin/github-connections/:id/test", controller.Test, admin)
		r.echo.PUT("/admin/github-connections/:id/github-app/private-key", controller.UpdateGitHubAppPrivateKey, admin)
		r.echo.DELETE("/admin/github-connections/:id/github-app/private-key", controller.DeleteGitHubAppPrivateKey, admin)
		r.echo.POST("/admin/github-connections/:id/github-app/test", controller.TestGitHubApp, admin)
		r.echo.DELETE("/admin/principals/:principalId", controller.DeletePrincipal, admin)
		r.echo.POST("/admin/principals/:principalId/repair", controller.RepairPrincipal, admin)
		r.echo.GET("/internal/sessions/:sessionId/github-credentials", controller.BrokerCredentials)
		r.echo.GET("/github-connections", controller.ListAvailable, read)
		r.echo.GET("/users/me/github-identities", controller.ListIdentities, read)
		r.echo.POST("/users/me/github-identities/link", controller.StartLink, write)
		r.echo.DELETE("/users/me/github-identities/:identity_id", controller.Unlink, write)
	}
	if r.handlers.googleConnectionsController != nil {
		admin := auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService)
		read := auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService)
		write := auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService)
		controller := r.handlers.googleConnectionsController
		r.echo.GET("/google-connections/login-options", controller.ListLoginOptions)
		r.echo.POST("/google-connections/login", controller.StartLogin)
		r.echo.GET("/admin/google-connections", controller.List, admin)
		r.echo.POST("/admin/google-connections", controller.Create, admin)
		r.echo.GET("/admin/google-connections/:id", controller.Get, admin)
		r.echo.PATCH("/admin/google-connections/:id", controller.Update, admin)
		r.echo.DELETE("/admin/google-connections/:id", controller.Delete, admin)
		r.echo.PUT("/admin/google-connections/:id/secret", controller.UpdateSecret, admin)
		r.echo.DELETE("/admin/google-connections/:id/secret", controller.DeleteSecret, admin)
		r.echo.POST("/admin/google-connections/:id/test", controller.Test, admin)
		r.echo.GET("/google-connections", controller.ListAvailable, read)
		r.echo.GET("/users/me/google-identities", controller.ListIdentities, read)
		r.echo.POST("/users/me/google-identities/link", controller.StartLink, write)
		r.echo.DELETE("/users/me/google-identities/:identity_id", controller.Unlink, write)
	}
	if r.handlers.sessionPoolController != nil {
		admin := auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService)
		poolRead := auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService)
		poolWrite := auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService)
		r.echo.POST("/session-managers/registration-tokens", r.handlers.sessionPoolController.IssueManagerRegistrationToken, poolWrite)
		r.echo.POST("/session-managers/enroll", r.handlers.sessionPoolController.EnrollManager)
		r.echo.GET("/session-managers", r.handlers.sessionPoolController.ListOwnedManagers, poolRead)
		r.echo.GET("/session-pools/status", r.handlers.sessionPoolController.ListManageablePoolStatus, poolRead)
		r.echo.GET("/session-managers/:id/logs", r.handlers.sessionPoolController.GetManagerLogs, poolRead)
		r.echo.GET("/session-runners/:id/logs", r.handlers.sessionPoolController.GetRunnerLogs, poolRead)
		r.echo.GET("/admin/session-runners", r.handlers.sessionPoolController.ListAdminRunners, admin)
		r.echo.GET("/admin/session-managers/:id/logs", r.handlers.sessionPoolController.GetAdminManagerLogs, admin)
		r.echo.GET("/admin/session-runners/:id/logs", r.handlers.sessionPoolController.GetAdminRunnerLogs, admin)
		r.echo.DELETE("/admin/session-runners/:id", r.handlers.sessionPoolController.DeleteAdminRunner, admin)
		r.echo.GET("/session-managers/:id", r.handlers.sessionPoolController.GetOwnedManager, poolRead)
		r.echo.PATCH("/session-managers/:id", r.handlers.sessionPoolController.PatchOwnedManager, poolWrite)
		r.echo.DELETE("/session-managers/:id", r.handlers.sessionPoolController.DeleteOwnedManager, poolWrite)
		r.echo.POST("/session-managers/:id/registration-token", r.handlers.sessionPoolController.RotateManagerRegistrationToken, poolWrite)
		r.echo.POST("/session-pools", r.handlers.sessionPoolController.CreateLogicalPool, poolWrite)
		r.echo.GET("/session-pools", r.handlers.sessionPoolController.ListPools, poolRead)
		r.echo.PATCH("/session-pools/:pool", r.handlers.sessionPoolController.PatchLogicalPool, poolWrite)
		r.echo.DELETE("/session-pools/:pool", r.handlers.sessionPoolController.DeleteLogicalPool, poolWrite)
		r.echo.POST("/session-pools/:pool/bindings", r.handlers.sessionPoolController.CreateBinding, poolWrite)
		r.echo.GET("/session-pools/:pool/bindings", r.handlers.sessionPoolController.ListBindings, poolRead)
		r.echo.PATCH("/session-pools/:pool/bindings/:bindingId", r.handlers.sessionPoolController.PatchBinding, poolWrite)
		r.echo.DELETE("/session-pools/:pool/bindings/:bindingId", r.handlers.sessionPoolController.DeleteBinding, poolWrite)
		r.echo.POST("/session-pools/:pool/suppliers/:id", r.handlers.sessionPoolController.CreatePoolSupplier, poolWrite)
		r.echo.GET("/session-pools/:pool/suppliers", r.handlers.sessionPoolController.ListPoolSuppliers, poolRead)
		r.echo.PATCH("/session-pools/:pool/suppliers/:id", r.handlers.sessionPoolController.PatchPoolSupplier, poolWrite)
		r.echo.DELETE("/session-pools/:pool/suppliers/:id", r.handlers.sessionPoolController.DeletePoolSupplier, poolWrite)
	}

	if r.handlers.googleOAuthController != nil {
		log.Printf("[ROUTES] Registering scia integration endpoints...")
		r.echo.GET("/integrations", r.handlers.googleOAuthController.GetIntegrations, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/integrations/:id/authorization-url", r.handlers.googleOAuthController.CreateAuthorizationURL, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/integrations/:id/revoke", r.handlers.googleOAuthController.RevokeIntegration, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/integrations/google-oauth/status", r.handlers.googleOAuthController.GetStatus, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		log.Printf("[ROUTES] scia integration endpoints registered")
	}

	// Add credentials routes if credentials repository is available (Kubernetes mode only)
	if r.server.credentialsRepo != nil && r.handlers.credentialsController != nil {
		log.Printf("[ROUTES] Registering credentials endpoints...")
		r.echo.GET("/credentials/:name", r.handlers.credentialsController.GetCredentials, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/credentials/:name", r.handlers.credentialsController.UploadCredentials, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/credentials/:name", r.handlers.credentialsController.DeleteCredentials, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Credentials endpoints registered")
	} else {
		log.Printf("[ROUTES] Credentials repository not available, skipping credentials routes")
	}

	// Add Codex device auth routes (requires credentials repo)
	if r.handlers.codexDeviceAuthController != nil {
		log.Printf("[ROUTES] Registering Codex device auth endpoints...")
		r.echo.GET("/codex/device-auth/config", r.handlers.codexDeviceAuthController.GetConfig, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/codex/device-auth", r.handlers.codexDeviceAuthController.StartDeviceAuth, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/codex/device-auth/token", r.handlers.codexDeviceAuthController.PollDeviceAuth, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/codex/device-auth/:attemptId", r.handlers.codexDeviceAuthController.GetAttempt, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/codex/device-auth/:attemptId", r.handlers.codexDeviceAuthController.CancelAttempt, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/internal/codex-device-auth/:attemptId/challenge", r.handlers.codexDeviceAuthController.ReportChallenge)
		r.echo.POST("/internal/codex-device-auth/:attemptId/result", r.handlers.codexDeviceAuthController.ReportResult)
		log.Printf("[ROUTES] Codex device auth endpoints registered")
	}
	if r.handlers.antigravityAuthController != nil {
		r.echo.GET("/antigravity/auth/config", r.handlers.antigravityAuthController.GetConfig, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/antigravity/auth/sso", r.handlers.antigravityAuthController.StartSSO, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/antigravity/auth/:attemptId", r.handlers.antigravityAuthController.GetAttempt, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/antigravity/auth/:attemptId/input", r.handlers.antigravityAuthController.SubmitInput, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/antigravity/auth/:attemptId", r.handlers.antigravityAuthController.CancelAttempt, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/internal/antigravity-auth/:attemptId/challenge", r.handlers.antigravityAuthController.ReportChallenge)
		r.echo.GET("/internal/antigravity-auth/:attemptId/input", r.handlers.antigravityAuthController.WorkerInput)
		r.echo.POST("/internal/antigravity-auth/:attemptId/result", r.handlers.antigravityAuthController.ReportResult)
	}

	// Add personal API key routes if controller is available (Kubernetes mode only)
	if r.handlers.personalAPIKeyController != nil {
		log.Printf("[ROUTES] Registering personal API key endpoints...")
		r.echo.GET("/users/me/api-key", r.handlers.personalAPIKeyController.GetPersonalAPIKeyMetadata, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/users/me/api-key", r.handlers.personalAPIKeyController.GetOrCreatePersonalAPIKey, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		log.Printf("[ROUTES] Personal API key endpoints registered")
	} else {
		log.Printf("[ROUTES] Personal API key controller not available, skipping personal API key routes")
	}

	// Unified multi API token endpoints (list/create/get/delete). Available
	// in Kubernetes mode only, where the API token repository is initialized.
	if r.handlers.apiTokenController != nil {
		log.Printf("[ROUTES] Registering API token endpoints...")
		r.echo.GET("/api-tokens", r.handlers.apiTokenController.List, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/api-tokens", r.handlers.apiTokenController.Create, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/api-tokens/:tokenId", r.handlers.apiTokenController.Get, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.DELETE("/api-tokens/:tokenId", r.handlers.apiTokenController.Delete, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		log.Printf("[ROUTES] API token endpoints registered")
	} else {
		log.Printf("[ROUTES] API token controller not available, skipping API token routes")
	}

	// Add sandbox policy routes if sandbox policy repository is available (Kubernetes mode only)
	if r.server.sandboxPolicyRepo != nil && r.handlers.sandboxPolicyController != nil {
		log.Printf("[ROUTES] Registering sandbox policy endpoints...")
		r.echo.POST("/sandbox-policies", r.handlers.sandboxPolicyController.CreateSandboxPolicy, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/sandbox-policies", r.handlers.sandboxPolicyController.ListSandboxPolicies, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/sandbox-policies/:id", r.handlers.sandboxPolicyController.GetSandboxPolicy, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/sandbox-policies/:id/domains", r.handlers.sandboxPolicyController.GetSandboxPolicyDomains, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/sandbox-policies/:id/domains/ignored", r.handlers.sandboxPolicyController.UpdateIgnoredDomains, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.PUT("/sandbox-policies/:id", r.handlers.sandboxPolicyController.UpdateSandboxPolicy, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/sandbox-policies/:id", r.handlers.sandboxPolicyController.DeleteSandboxPolicy, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Sandbox policy endpoints registered")
	} else {
		log.Printf("[ROUTES] Sandbox policy repository not available, skipping sandbox policy routes")
	}

	if r.server.storageRepo != nil && r.handlers.storageController != nil {
		log.Printf("[ROUTES] Registering storage endpoints...")
		r.echo.POST("/storages", r.handlers.storageController.Create, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/storages", r.handlers.storageController.List, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/storages/:id", r.handlers.storageController.Get, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PATCH("/storages/:id", r.handlers.storageController.Update, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/storages/:id", r.handlers.storageController.Delete, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/storages/:id/files", r.handlers.storageController.ListFiles, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/storages/:id/files/*", r.handlers.storageController.PutFile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/storages/:id/files/*", r.handlers.storageController.GetFile, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.DELETE("/storages/:id/files/*", r.handlers.storageController.DeleteFile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Storage endpoints registered")
	}

	// Add file routes if user file repository is available (Kubernetes mode only)
	if r.server.userFileRepo != nil && r.handlers.fileController != nil {
		log.Printf("[ROUTES] Registering user file endpoints...")
		r.echo.POST("/files", r.handlers.fileController.CreateFile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/files", r.handlers.fileController.ListFiles, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/files/:fileId", r.handlers.fileController.GetFile, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/files/:fileId", r.handlers.fileController.UpdateFile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/files/:fileId", r.handlers.fileController.DeleteFile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] User file endpoints registered")
	} else {
		log.Printf("[ROUTES] User file repository not available, skipping file routes")
	}

	// Add session profile routes if session profile repository is available (Kubernetes mode only)
	if r.server.sessionProfileRepo != nil && r.handlers.sessionProfileController != nil {
		log.Printf("[ROUTES] Registering session profile endpoints...")
		r.echo.POST("/session-profiles", r.handlers.sessionProfileController.CreateSessionProfile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/session-profiles", r.handlers.sessionProfileController.ListSessionProfiles, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/session-profiles/preview", r.handlers.sessionProfileController.PreviewSessionProfile, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/session-profiles/:id", r.handlers.sessionProfileController.GetSessionProfile, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/session-profiles/:id", r.handlers.sessionProfileController.UpdateSessionProfile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/session-profiles/:id", r.handlers.sessionProfileController.DeleteSessionProfile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Session profile endpoints registered")
	} else {
		log.Printf("[ROUTES] Session profile repository not available, skipping session profile routes")
	}

	return nil
}
