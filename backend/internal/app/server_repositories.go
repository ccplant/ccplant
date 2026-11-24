package app

import (
	"context"
	"log"

	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/di"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionmanagerapi"
	infrasessionrunner "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

type serverRepositoryConfig struct {
	config         *config.Config
	persistence    kubernetes.Interface
	namespace      string
	kvStore        kvstore.Store
	runtimeContext context.Context
	k8sManager     *services.KubernetesSessionManager
	remoteManager  *sessionmanagerapi.Client
	container      *di.Container
	logger         *logger.Logger
	encryption     *services.EncryptionServiceRegistry
}

type serverRepositories struct {
	settings        portrepos.SettingsRepository
	credentials     portrepos.CredentialsRepository
	share           portrepos.ShareRepository
	teamConfig      portrepos.TeamConfigRepository
	personalAPIKey  portrepos.PersonalAPIKeyRepository
	apiToken        portrepos.APITokenRepository
	localUser       portrepos.LocalUserRepository
	sandboxPolicy   portrepos.SandboxPolicyRepository
	storage         portrepos.StorageRepository
	sandboxDomain   *repositories.KubernetesSandboxDomainRepository
	sessionRoute    portrepos.SessionRouteRepository
	sessionRunner   sessionrunnercore.Store
	userFile        portrepos.UserFileRepository
	sessionProfile  portrepos.SessionProfileRepository
	contextTemplate portrepos.SessionContextTemplateRepository
}

func buildServerRepositories(input serverRepositoryConfig) serverRepositories {
	repos := buildCoreServerRepositories(input)
	buildSessionRepositories(input, &repos)
	configureSessionManagerRepositories(input, repos)
	return repos
}

func buildCoreServerRepositories(input serverRepositoryConfig) serverRepositories {
	repos := serverRepositories{
		settings:       repositories.NewKubernetesSettingsRepository(input.persistence, input.namespace, input.encryption),
		credentials:    repositories.NewKubernetesCredentialsRepository(input.persistence, input.namespace),
		share:          repositories.NewKubernetesShareRepository(input.persistence, input.namespace),
		teamConfig:     repositories.NewKubernetesTeamConfigRepository(input.persistence, input.namespace),
		personalAPIKey: repositories.NewKubernetesPersonalAPIKeyRepository(input.persistence, input.namespace),
		apiToken:       repositories.NewKubernetesAPITokenRepository(input.persistence, input.namespace),
		localUser:      repositories.NewKubernetesLocalUserRepository(input.persistence, input.namespace),
		sandboxPolicy:  repositories.NewKubernetesSandboxPolicyRepository(input.persistence, input.namespace),
		storage:        repositories.NewKubernetesStorageRepository(input.persistence, input.namespace),
		sandboxDomain:  repositories.NewKubernetesSandboxDomainRepository(input.persistence, input.namespace),
		sessionRoute:   repositories.NewKubernetesSessionRouteRepository(input.persistence, input.namespace),
	}
	if simpleAuth, ok := input.container.AuthService.(*services.SimpleAuthService); ok {
		simpleAuth.SetTeamMembershipResolver(repos.teamConfig, input.config.TeamDiscovery)
	}
	log.Printf("[SERVER] Core application repositories initialized")
	return repos
}

func buildSessionRepositories(input serverRepositoryConfig, repos *serverRepositories) {
	runnerKVStore := input.kvStore
	if runnerKVStore == nil {
		runnerKVStore = kvstore.NewKubernetesStore(input.persistence)
	}
	repos.sessionRunner = infrasessionrunner.NewStore(runnerKVStore, input.namespace)
	startBuiltInSessionManagerReconciler(input.runtimeContext, repos.sessionRunner, input.config)
	repos.userFile = repositories.NewKubernetesUserFileRepository(input.persistence, input.namespace)
	repos.sessionProfile = repositories.NewKubernetesSessionProfileRepository(input.persistence, input.namespace, input.encryption)
	repos.contextTemplate = repositories.NewKubernetesSessionContextTemplateRepository(input.persistence, input.namespace)
	log.Printf("[SERVER] Session repositories initialized")
}

func configureSessionManagerRepositories(input serverRepositoryConfig, repos serverRepositories) {
	if input.k8sManager != nil {
		input.k8sManager.SetSettingsRepository(repos.settings)
		input.k8sManager.SetSlackTokenClient(input.persistence, input.namespace)
		input.k8sManager.SetCredentialsRepository(repos.credentials)
		input.k8sManager.SetTeamConfigRepository(repos.teamConfig)
		input.k8sManager.SetPersonalAPIKeyRepository(repos.personalAPIKey)
		input.k8sManager.SetSandboxPolicyRepository(repos.sandboxPolicy)
		input.k8sManager.SetUserFileRepository(repos.userFile)
		input.k8sManager.SetSessionProfileRepository(repos.sessionProfile)
	}
	if input.remoteManager != nil {
		configureRemoteProvisionSettings(input, repos)
	}
}

func configureRemoteProvisionSettings(input serverRepositoryConfig, repos serverRepositories) {
	builder, err := services.NewKubernetesSessionManagerWithClient(input.config, false, input.logger, fake.NewSimpleClientset())
	if err != nil {
		log.Fatalf("[SERVER] Failed to initialize API-side provision settings builder: %v", err)
	}
	builder.SetSettingsRepository(repos.settings)
	builder.SetSettingsSecretClient(input.persistence, input.namespace)
	builder.SetSlackTokenClient(input.persistence, input.namespace)
	builder.SetCredentialsRepository(repos.credentials)
	builder.SetTeamConfigRepository(repos.teamConfig)
	builder.SetPersonalAPIKeyRepository(repos.personalAPIKey)
	builder.SetSandboxPolicyRepository(repos.sandboxPolicy)
	builder.SetUserFileRepository(repos.userFile)
	builder.SetSessionProfileRepository(repos.sessionProfile)
	input.remoteManager.SetProvisionSettingsBuilder(builder)
}
