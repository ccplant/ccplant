package app

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
)

type connectionControllers struct {
	apiKey         *repositories.KubernetesPersonalAPIKeyRepository
	adminSettings  *controllers.AdminSettingsController
	github         *controllers.GitHubConnectionsController
	google         *controllers.GoogleConnectionsController
	teamMembership *controllers.TeamMembershipController
	membershipRepo *repositories.KVStoreTeamMembershipRepository
}

func buildConnectionControllers(server *Server) connectionControllers {
	var result connectionControllers
	if server.persistenceClient == nil {
		return result
	}
	result.apiKey = repositories.NewKubernetesPersonalAPIKeyRepository(server.GetPersistenceClient(), server.namespace)
	if server.kvStore != nil {
		result.adminSettings = controllers.NewAdminSettingsController(server.kvStore, server.namespace, server.GetConfig()).WithRuntimeConfigProvider(server.GetConfigProvider())
	}
	encryptedStorage := server.GetConfig() != nil && supportsGitHubSecretStorage(server.GetConfig().KVStore)
	result.github = controllers.NewGitHubConnectionsController(server.GetPersistenceClient(), server.namespace, "", encryptedStorage)
	if cfg := server.GetConfig(); cfg != nil && cfg.Auth.GitHub != nil {
		result.github.SetBuiltInAPIURL(cfg.Auth.GitHub.BaseURL)
	}
	result.membershipRepo = repositories.NewKVStoreTeamMembershipRepository(server.GetApplicationKVStore(), server.namespace)
	result.teamMembership = controllers.NewTeamMembershipController(server.teamConfigRepo, result.membershipRepo, result.github)
	result.github.SetMembershipRepository(result.membershipRepo)
	result.github.SetMembershipSyncer(result.teamMembership)
	result.google = controllers.NewGoogleConnectionsController(server.GetPersistenceClient(), server.namespace, "", encryptedStorage, result.github)
	if simpleAuth, ok := server.container.AuthService.(*services.SimpleAuthService); ok {
		simpleAuth.SetGitHubMembershipResolver(result.github)
		simpleAuth.SetGitHubConnectionCredentialResolver(result.github)
	}
	return result
}

func buildDeviceAuthControllers(server *Server) (*controllers.CodexDeviceAuthController, *controllers.AntigravityAuthController) {
	if server.credentialsRepo == nil {
		return nil, nil
	}
	codex := controllers.NewCodexDeviceAuthController(server.credentialsRepo, server.codexDeviceAuthLauncher)
	if server.persistenceClient != nil {
		codex.WithAttemptStore(repositories.NewKubernetesCodexAuthAttemptRepository(server.GetPersistenceClient(), server.namespace))
	}
	if cfg := server.GetConfig(); cfg != nil {
		codex.WithCallbackBaseURL(cfg.CodexDeviceAuthCallbackBaseURL)
	}
	log.Printf("[ROUTER] Codex device auth controller initialized")
	if server.codexDeviceAuthLauncher == nil {
		return codex, nil
	}
	antigravity := controllers.NewAntigravityAuthController(server.credentialsRepo, server.codexDeviceAuthLauncher)
	if server.persistenceClient != nil {
		antigravity.WithAttemptStore(repositories.NewKubernetesAntigravityAuthAttemptRepository(server.GetPersistenceClient(), server.namespace))
	}
	if cfg := server.GetConfig(); cfg != nil {
		antigravity.WithCallbackBaseURL(cfg.CodexDeviceAuthCallbackBaseURL)
	}
	log.Printf("[ROUTER] Antigravity auth controller initialized")
	return codex, antigravity
}
