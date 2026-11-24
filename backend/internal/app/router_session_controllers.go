package app

import (
	"context"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func buildSessionPoolController(server *Server) *controllers.SessionPoolController {
	var controller *controllers.SessionPoolController
	if provider, ok := server.sessionManager.(interface {
		ExternalRuntimeProfile() *sessionsettings.RuntimeProfile
	}); ok {
		controller = controllers.NewSessionPoolController(server.sessionRunnerStore, server.sessionRouteRepo, provider)
	} else {
		controller = controllers.NewSessionPoolController(server.sessionRunnerStore, server.sessionRouteRepo)
	}
	if server.esmControlStore != nil {
		controller.WithManagerLiveness(server.esmControlStore)
	}
	if server.esmControlTunnel != nil {
		controller.WithManagerTunnel(server.esmControlTunnel)
	}
	if server.sessionAllocationNotifier != nil {
		controller.WithAllocationNotifier(server.sessionAllocationNotifier)
	}
	if managedFiles, ok := server.credentialsRepo.(interface {
		SaveFiles(context.Context, string, []sessionsettings.ManagedFile) error
	}); ok {
		controller.WithManagedFileStore(managedFiles)
	}
	return controller
}

func buildSessionController(server *Server, github *controllers.GitHubConnectionsController, pool *controllers.SessionPoolController) *controllers.SessionController {
	options := []controllers.SessionControllerOption{
		controllers.WithSessionRouteRepository(server.GetSessionRouteRepository()), controllers.WithSettingsRepository(server.settingsRepo),
		controllers.WithSessionProfileRepository(server.sessionProfileRepo), controllers.WithSessionContextTemplates(server.contextTemplateRepo, server.sessionStateStore),
		controllers.WithESMControlTunnel(server.esmControlTunnel), controllers.WithSessionRunnerStore(server.sessionRunnerStore),
		controllers.WithSessionTokenDebug(server.config.SessionTokenDebug), controllers.WithGitHubBrokerBaseURL(server.config.GitHubBrokerBaseURL),
		controllers.WithScheduleSessionTTL(server.config.ScheduleSessionTTL), controllers.WithWebhookSessionTTL(server.config.WebhookSessionTTL),
		controllers.WithInteractiveSessionTTL(server.config.InteractiveSessionTTL), controllers.WithRouteStatusEventBus(server.routeStatusBus),
	}
	if github != nil {
		options = append(options, controllers.WithGitHubTokenResolver(github))
	}
	controller := controllers.NewSessionController(server, server, options...)
	pool.WithRouteStatusPublisher(controller)
	server.startRouteStatusRelay(controller)
	return controller
}

type auxiliaryControllers struct {
	settings     *controllers.SettingsController
	teamConfig   *controllers.TeamConfigController
	googleOAuth  *controllers.GoogleOAuthController
	credentials  *controllers.CredentialsController
	codex        *controllers.CodexDeviceAuthController
	antigravity  *controllers.AntigravityAuthController
	acp          *controllers.ACPController
	usage        *controllers.UsageController
	sessionUsage *controllers.SessionUsageController
}

func buildAuxiliaryControllers(server *Server, connections connectionControllers) auxiliaryControllers {
	settings := controllers.NewSettingsController(server.settingsRepo, server.notificationSvc)
	settings.SetESMControlTunnel(server.esmControlTunnel)
	settings.SetSessionRunnerStore(server.sessionRunnerStore)
	teamConfig := controllers.NewTeamConfigController(server.teamConfigRepo)
	if connections.membershipRepo != nil {
		teamConfig.WithMembershipRepository(connections.membershipRepo)
	}
	var googleOAuth *controllers.GoogleOAuthController
	if cfg := server.GetConfig(); cfg != nil {
		googleOAuth = controllers.NewGoogleOAuthController(cfg.Scia, server.GetPersistenceClient(), server.namespace)
		if connections.apiKey != nil {
			googleOAuth.WithPersonalAPIKeyRepository(connections.apiKey)
		}
	}
	var credentials *controllers.CredentialsController
	if server.credentialsRepo != nil {
		credentials = controllers.NewCredentialsController(server.credentialsRepo)
		log.Printf("[ROUTER] Credentials controller initialized")
	}
	codex, antigravity := buildDeviceAuthControllers(server)
	acp := controllers.NewACPController(server, server, server.GetSessionRouteRepository())
	acp.SetESMControlTunnel(server.esmControlTunnel)
	var usage *controllers.UsageController
	if server.usageRepo != nil {
		usage = controllers.NewUsageController(server.usageRepo, server.sessionManager)
	}
	var sessionUsage *controllers.SessionUsageController
	if repository, ok := server.sessionCountRepo.(portrepos.SessionRuntimeRepository); ok {
		sessionUsage = controllers.NewSessionUsageController(repository, server.teamConfigRepo)
	}
	return auxiliaryControllers{settings: settings, teamConfig: teamConfig, googleOAuth: googleOAuth, credentials: credentials, codex: codex, antigravity: antigravity, acp: acp, usage: usage, sessionUsage: sessionUsage}
}
