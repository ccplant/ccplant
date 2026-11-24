package app

import (
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
)

// Router handles route registration and management
type Router struct {
	echo     *echo.Echo
	server   *Server
	handlers *HandlerRegistry
}

// HandlerRegistry contains all handlers
type HandlerRegistry struct {
	notificationHandlers           *controllers.NotificationHandlers
	healthController               *controllers.HealthController
	sessionController              *controllers.SessionController
	acpController                  *controllers.ACPController
	settingsController             *controllers.SettingsController
	adminSettingsController        *controllers.AdminSettingsController
	githubConnectionsController    *controllers.GitHubConnectionsController
	googleConnectionsController    *controllers.GoogleConnectionsController
	teamConfigController           *controllers.TeamConfigController
	teamMembershipController       *controllers.TeamMembershipController
	googleOAuthController          *controllers.GoogleOAuthController
	credentialsController          *controllers.CredentialsController
	codexDeviceAuthController      *controllers.CodexDeviceAuthController
	antigravityAuthController      *controllers.AntigravityAuthController
	userController                 *controllers.UserController
	shareController                *controllers.ShareController
	personalAPIKeyController       *controllers.PersonalAPIKeyController
	apiTokenController             *controllers.APITokenController
	adminLocalUserController       *controllers.AdminLocalUserController
	sandboxPolicyController        *controllers.SandboxPolicyController
	storageController              *controllers.StorageController
	resourceTransferController     *controllers.ResourceTransferController
	fileController                 *controllers.FileController
	sessionProfileController       *controllers.SessionProfileController
	provisionerController          *controllers.ProvisionerController
	externalAllocationController   *controllers.ProvisionerController
	workerControlController        *controllers.WorkerControlController
	sessionControlController       *controllers.SessionControlController
	sessionControlReaderController *controllers.SessionControlReaderController
	esmControlController           *controllers.ESMControlController
	sessionRuntimeController       *controllers.SessionRuntimeController
	sessionSecretController        *controllers.SessionSecretController
	sessionPoolController          *controllers.SessionPoolController
	usageController                *controllers.UsageController
	sessionUsageController         *controllers.SessionUsageController
	customHandlers                 []CustomHandler
}

// CustomHandler interface for adding custom routes
type CustomHandler interface {
	RegisterRoutes(e *echo.Echo) error
	GetName() string
}

// NewRouter creates a new Router instance
func NewRouter(e *echo.Echo, server *Server) *Router {
	connections := buildConnectionControllers(server)
	auxiliary := buildAuxiliaryControllers(server, connections)
	sessionPoolController := buildSessionPoolController(server)
	sessionController := buildSessionController(server, connections.github, sessionPoolController)

	repositoryControllers := buildRepositoryControllers(server, connections.apiKey)

	runtime := buildRuntimeControllers(server, sessionController)

	return &Router{
		echo:   e,
		server: server,
		handlers: &HandlerRegistry{
			notificationHandlers:           controllers.NewNotificationHandlers(server.notificationSvc, server.sessionManager, server.sessionRouteRepo),
			healthController:               controllers.NewHealthController(),
			sessionController:              sessionController,
			acpController:                  auxiliary.acp,
			settingsController:             auxiliary.settings,
			adminSettingsController:        connections.adminSettings,
			githubConnectionsController:    connections.github,
			googleConnectionsController:    connections.google,
			teamConfigController:           auxiliary.teamConfig,
			teamMembershipController:       connections.teamMembership,
			googleOAuthController:          auxiliary.googleOAuth,
			credentialsController:          auxiliary.credentials,
			codexDeviceAuthController:      auxiliary.codex,
			antigravityAuthController:      auxiliary.antigravity,
			userController:                 controllers.NewUserController(server.teamConfigRepo),
			shareController:                repositoryControllers.share,
			personalAPIKeyController:       repositoryControllers.personalAPIKey,
			apiTokenController:             repositoryControllers.apiToken,
			adminLocalUserController:       repositoryControllers.adminLocalUser,
			sandboxPolicyController:        repositoryControllers.sandboxPolicy,
			storageController:              repositoryControllers.storage,
			resourceTransferController:     repositoryControllers.resourceTransfer,
			fileController:                 repositoryControllers.file,
			sessionProfileController:       repositoryControllers.sessionProfile,
			provisionerController:          runtime.provisioner,
			externalAllocationController:   runtime.externalAllocation,
			workerControlController:        runtime.worker,
			sessionControlController:       runtime.sessionControl,
			sessionControlReaderController: runtime.sessionControlReader,
			esmControlController:           runtime.esm,
			sessionRuntimeController:       runtime.sessionRuntime,
			sessionSecretController:        runtime.sessionSecret,
			sessionPoolController:          sessionPoolController,
			usageController:                auxiliary.usage,
			sessionUsageController:         auxiliary.sessionUsage,
			customHandlers:                 make([]CustomHandler, 0),
		},
	}
}
