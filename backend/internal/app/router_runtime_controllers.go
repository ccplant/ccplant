package app

import (
	"log"

	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/slackbot"
)

type runtimeControllers struct {
	provisioner, externalAllocation *controllers.ProvisionerController
	worker                          *controllers.WorkerControlController
	sessionControl                  *controllers.SessionControlController
	sessionControlReader            *controllers.SessionControlReaderController
	esm                             *controllers.ESMControlController
	sessionRuntime                  *controllers.SessionRuntimeController
	sessionSecret                   *controllers.SessionSecretController
}

func buildRuntimeControllers(server *Server, sessionController *controllers.SessionController) runtimeControllers {
	var result runtimeControllers
	if server.persistenceClient != nil {
		result.sessionSecret = controllers.NewSessionSecretController(server.persistenceClient, server.namespace, server.sessionManager, server.sessionRouteRepo)
	}
	if k8sManager, ok := server.sessionManager.(*services.KubernetesSessionManager); ok {
		result.provisioner = controllers.NewProvisionerController(k8sManager, k8sManager, server.settingsRepo, server.sessionRouteRepo, server.sessionStateStore)
		if server.sessionControlStore != nil {
			result.sessionControl = controllers.NewSessionControlController(server.sessionControlStore, k8sManager)
		}
		if server.esmControlStore != nil {
			result.esm = controllers.NewESMControlController(server.esmControlStore, result.provisioner, server.sessionRunnerStore)
		}
		log.Printf("[ROUTER] Provisioner controller initialized")
	}
	if queue, ok := server.sessionManager.(sessionallocation.Queue); ok {
		result.externalAllocation = result.provisioner
		if result.externalAllocation == nil {
			result.externalAllocation = controllers.NewProvisionerController(nil, queue, server.settingsRepo, server.sessionRouteRepo)
		}
		if server.esmControlStore != nil {
			result.esm = controllers.NewESMControlController(server.esmControlStore, result.externalAllocation, server.sessionRunnerStore)
		}
	}
	if result.esm == nil && server.esmControlStore != nil && server.sessionRunnerStore != nil {
		result.esm = controllers.NewESMControlController(server.esmControlStore, nil, server.sessionRunnerStore)
	}
	if server.esmControlStore != nil && server.sessionRouteRepo != nil {
		result.sessionRuntime = controllers.NewSessionRuntimeController(server.esmControlStore, server.sessionRouteRepo, sessionController).WithRunnerStore(server.sessionRunnerStore)
	}
	result.worker = buildWorkerControlController(server, sessionController)
	if server.sessionControlStore != nil {
		result.sessionControlReader = controllers.NewSessionControlReaderController(server.sessionControlStore, server.sessionManager)
	}
	return result
}

func buildWorkerControlController(server *Server, sessionController *controllers.SessionController) *controllers.WorkerControlController {
	cfg := server.GetConfig()
	if cfg == nil || cfg.Worker.ControlAPIToken == "" {
		return nil
	}
	namespace := cfg.KVStore.Namespace
	if namespace == "" {
		namespace = "default"
	}
	slackRepo := repositories.NewKubernetesSlackBotRepository(server.GetPersistenceClient(), namespace)
	slackResolver := slackbot.NewSlackChannelResolver(server.GetPersistenceClient(), namespace).WithSettingsRepository(server.GetSettingsRepository())
	controller := controllers.NewWorkerControlController(server.sessionManager, cfg.Worker.ControlAPIToken, cfg.Worker.ExecutionTokenSigningKey, server, server.sessionRouteRepo).
		WithSlackBots(slackRepo, slackResolver).
		WithLeases(buildWorkerLeaseClient(cfg)).
		WithSessionDeleter(sessionController.DeleteSessionFromWorker).
		WithSessionPoolAllocationCleaner(server.DeleteSessionPoolAllocation)
	if server.scheduleManager != nil {
		controller.WithScheduleManager(server.scheduleManager)
	}
	log.Printf("[ROUTER] Worker control controller initialized")
	return controller
}
