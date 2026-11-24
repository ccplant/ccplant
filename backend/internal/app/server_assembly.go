package app

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/di"
	infraesmcontrol "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	storageattachmentuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/storageattachment"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

type serverSessionServices struct {
	stateStore     services.SessionStateStore
	objectStore    services.StorageObjectStore
	sessionControl sessioncontrol.Store
	esmControl     esmcontrol.Store
	esmTunnel      *infraesmcontrol.Tunnel
	codexLauncher  codexauth.WorkloadLauncher
	localFallback  bool
	schedule       schedule.Manager
}

func buildServerSessionServices(cfg *config.Config, infrastructure sessionInfrastructure, repos serverRepositories) serverSessionServices {
	stateStore, err := services.NewSessionStateStore(context.Background(), cfg.SessionPersistence)
	if err != nil {
		if cfg.SessionPersistence.Backend != "s3" {
			log.Fatalf("[SERVER] Failed to initialize session state store: %v", err)
		}
		log.Printf("[SERVER] Session persistence disabled because S3 store initialization failed: %v", err)
		stateStore = nil
	}
	objectStore, err := services.NewStorageObjectStore(context.Background(), cfg.SessionPersistence.S3)
	if err != nil {
		log.Printf("[SERVER] Object-backed storage disabled because S3 store initialization failed: %v", err)
		objectStore = nil
	}
	sessionControlStore := buildSessionControlStore(cfg)
	if sessionControlStore != nil && infrastructure.k8sManager != nil {
		infrastructure.k8sManager.SetSessionControlStore(sessionControlStore)
	}
	esmControlStore := buildESMControlStore(cfg)
	var tunnel *infraesmcontrol.Tunnel
	if esmControlStore != nil {
		tunnel = infraesmcontrol.NewTunnel(esmControlStore)
	}
	localFallback := !strings.EqualFold(os.Getenv("AGENTAPI_LOCAL_SESSION_FALLBACK_ENABLED"), "false")
	var localLauncher codexauth.WorkloadLauncher
	if localFallback && infrastructure.k8sManager != nil {
		localLauncher = infrastructure.k8sManager
	}
	var codexLauncher codexauth.WorkloadLauncher
	if repos.sessionRunner != nil && (tunnel != nil || localLauncher != nil) {
		codexLauncher = infraesmcontrol.NewCodexDeviceAuthLauncher(tunnel, repos.sessionRunner, localLauncher)
		log.Printf("[SERVER] Codex device auth workloads use authorized session routes")
	}
	return serverSessionServices{stateStore: stateStore, objectStore: objectStore, sessionControl: sessionControlStore, esmControl: esmControlStore, esmTunnel: tunnel, codexLauncher: codexLauncher, localFallback: localFallback, schedule: schedule.NewKubernetesManager(infrastructure.persistence, infrastructure.namespace)}
}

type serverAssembly struct {
	config         *config.Config
	verbose        bool
	echo           *echo.Echo
	logger         *logger.Logger
	container      *di.Container
	infrastructure sessionInfrastructure
	runtime        serverRuntime
	repositories   serverRepositories
	services       serverSessionServices
}

func assembleServer(input serverAssembly) *Server {
	repos, runtime, infrastructure, sessionServices := input.repositories, input.runtime, input.infrastructure, input.services
	return &Server{
		config: input.config, configProvider: runtime.provider, runtimeConfigCancel: runtime.cancel, echo: input.echo, verbose: input.verbose,
		logger: input.logger, container: input.container, sessionManager: infrastructure.manager, codexDeviceAuthLauncher: sessionServices.codexLauncher,
		persistenceClient: infrastructure.persistence, kvStore: infrastructure.kvStore, usageRepo: runtime.usage, sessionCountRepo: runtime.sessionCount,
		settingsRepo: repos.settings, credentialsRepo: repos.credentials, shareRepo: repos.share, teamConfigRepo: repos.teamConfig,
		sandboxPolicyRepo: repos.sandboxPolicy, storageRepo: repos.storage, storageAttachmentResolver: storageattachmentuc.NewResolver(repos.storage),
		storageObjectStore: sessionServices.objectStore, sandboxDomainRepo: repos.sandboxDomain, sessionRouteRepo: repos.sessionRoute,
		sessionRunnerStore: repos.sessionRunner, sessionAllocationNotifier: runtime.allocationNotifier, sessionAllocationRedis: runtime.allocationRedis,
		routeStatusBus: runtime.routeStatusBus, userFileRepo: repos.userFile, sessionProfileRepo: repos.sessionProfile, contextTemplateRepo: repos.contextTemplate,
		scheduleManager: sessionServices.schedule, apiTokenRepo: repos.apiToken, localUserRepo: repos.localUser, namespace: infrastructure.namespace,
		personalAPIKeyRepo: repos.personalAPIKey, sessionStateStore: sessionServices.stateStore, sessionControlStore: sessionServices.sessionControl,
		esmControlStore: sessionServices.esmControl, esmControlTunnel: sessionServices.esmTunnel, directSessionRuntimeEnabled: true,
		localSessionFallbackEnabled: sessionServices.localFallback,
	}
}
