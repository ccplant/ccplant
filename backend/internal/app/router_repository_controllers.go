package app

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	apitokenuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/api_token"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/personal_api_key"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/resource_transfer"
)

type repositoryControllers struct {
	share            *controllers.ShareController
	personalAPIKey   *controllers.PersonalAPIKeyController
	apiToken         *controllers.APITokenController
	adminLocalUser   *controllers.AdminLocalUserController
	sandboxPolicy    *controllers.SandboxPolicyController
	storage          *controllers.StorageController
	resourceTransfer *controllers.ResourceTransferController
	file             *controllers.FileController
	sessionProfile   *controllers.SessionProfileController
}

func buildRepositoryControllers(server *Server, apiKeyRepo *repositories.KubernetesPersonalAPIKeyRepository) repositoryControllers {
	var result repositoryControllers
	if server.shareRepo != nil {
		result.share = controllers.NewShareController(server, server.shareRepo)
		result.share.SetSessionRuntimeRouting(server.sessionRouteRepo, server.esmControlTunnel)
	}
	if apiKeyRepo != nil {
		useCase := personal_api_key.NewGetOrCreatePersonalAPIKeyUseCase(apiKeyRepo)
		var authService controllers.AuthServiceForPersonalAPIKey
		if simpleAuth, ok := server.container.AuthService.(*services.SimpleAuthService); ok {
			authService = simpleAuth
		}
		result.personalAPIKey = controllers.NewPersonalAPIKeyController(useCase, authService)
	}
	result.apiToken, result.adminLocalUser = buildAPITokenControllers(server)
	if server.sandboxPolicyRepo != nil {
		result.sandboxPolicy = controllers.NewSandboxPolicyController(server.sandboxPolicyRepo, server.sandboxDomainRepo)
	}
	if server.storageRepo != nil {
		result.storage = controllers.NewStorageController(server.storageRepo, server.storageObjectStore)
	}
	result.resourceTransfer = buildResourceTransferController(server)
	if server.userFileRepo != nil {
		result.file = controllers.NewFileController(server.userFileRepo)
	}
	if server.sessionProfileRepo != nil {
		result.sessionProfile = controllers.NewSessionProfileController(server.sessionProfileRepo, server.settingsRepo)
	}
	return result
}

func buildAPITokenControllers(server *Server) (*controllers.APITokenController, *controllers.AdminLocalUserController) {
	if server.apiTokenRepo == nil {
		return nil, nil
	}
	var authService apitokenuc.AuthService
	if simpleAuth, ok := server.container.AuthService.(*services.SimpleAuthService); ok {
		authService = simpleAuth
	}
	controller := controllers.NewAPITokenController(
		apitokenuc.NewCreateAPITokenUseCase(server.apiTokenRepo, authService), apitokenuc.NewListAPITokenUseCase(server.apiTokenRepo),
		apitokenuc.NewGetAPITokenUseCase(server.apiTokenRepo), apitokenuc.NewDeleteAPITokenUseCase(server.apiTokenRepo, authService),
	)
	if server.localUserRepo == nil {
		return controller, nil
	}
	return controller, controllers.NewAdminLocalUserController(server.localUserRepo, server.apiTokenRepo, authService)
}

func buildResourceTransferController(server *Server) *controllers.ResourceTransferController {
	options := []resource_transfer.Option{
		resource_transfer.WithSessionProfileRepository(server.sessionProfileRepo),
		resource_transfer.WithSandboxPolicyRepository(server.sandboxPolicyRepo),
	}
	if server.persistenceClient != nil {
		client, namespace := server.GetPersistenceClient(), server.namespace
		options = append(options,
			resource_transfer.WithWebhookRepository(repositories.NewKubernetesWebhookRepository(client, namespace)),
			resource_transfer.WithSlackBotRepository(repositories.NewKubernetesSlackBotRepository(client, namespace)),
		)
	}
	log.Printf("[ROUTER] Resource transfer controller initialized")
	return controllers.NewResourceTransferController(resource_transfer.New(options...))
}
