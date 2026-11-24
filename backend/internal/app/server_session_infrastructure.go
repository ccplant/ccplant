package app

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionmanagerapi"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
)

type sessionInfrastructure struct {
	namespace     string
	k8sManager    *services.KubernetesSessionManager
	remoteManager *sessionmanagerapi.Client
	manager       portrepos.SessionManager
	persistence   kubernetes.Interface
	kvStore       kvstore.Store
}

func buildSessionInfrastructure(cfg *config.Config, verbose bool, lgr *logger.Logger) sessionInfrastructure {
	if cfg.SessionManager.APIURL != "" {
		return buildRemoteSessionInfrastructure(cfg)
	}
	return buildLocalSessionInfrastructure(cfg, verbose, lgr)
}

func buildRemoteSessionInfrastructure(cfg *config.Config) sessionInfrastructure {
	if err := validateAPIKVStore(cfg.KVStore); err != nil {
		log.Fatalf("[SERVER] Invalid API KV persistence: %v", err)
	}
	remoteManager, err := sessionmanagerapi.NewClient(cfg.SessionManager.APIURL, cfg.SessionManager.APIToken)
	if err != nil {
		log.Fatalf("[SERVER] Failed to initialize session-manager client: %v", err)
	}
	var client kubernetes.Interface = fake.NewSimpleClientset()
	if configuredKVBackend(cfg.KVStore) == "kubernetes" {
		restConfig, configErr := ctrlconfig.GetConfig()
		if configErr != nil {
			log.Fatalf("[SERVER] Failed to get Kubernetes config for API KV store: %v", configErr)
		}
		client, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			log.Fatalf("[SERVER] Failed to create Kubernetes client for API KV store: %v", err)
		}
	}
	store, wrapPersistence, err := buildApplicationKVStore(cfg.KVStore, client)
	if err != nil {
		log.Fatalf("[SERVER] Failed to initialize API KV store: %v", err)
	}
	persistence := client
	if wrapPersistence {
		persistence = kvstore.NewKubernetesAdapter(client, store)
	}
	log.Printf("[SERVER] Public API connected to isolated session manager: %s", cfg.SessionManager.APIURL)
	return sessionInfrastructure{namespace: resolveApplicationNamespace(cfg.KVStore.Namespace), remoteManager: remoteManager, manager: remoteManager, persistence: persistence, kvStore: store}
}

func buildLocalSessionInfrastructure(cfg *config.Config, verbose bool, lgr *logger.Logger) sessionInfrastructure {
	log.Printf("[SERVER] Initializing legacy in-process Kubernetes session manager")
	manager, err := services.NewKubernetesSessionManager(cfg, verbose, lgr)
	if err != nil {
		log.Printf("[SERVER] Kubernetes config not available, using fake client: %v", err)
		manager, err = services.NewKubernetesSessionManagerWithClient(cfg, verbose, lgr, fake.NewSimpleClientset())
		if err != nil {
			log.Fatalf("[SERVER] Failed to initialize session manager with fake client: %v", err)
		}
	}
	namespace := resolveApplicationNamespace(manager.GetNamespace())
	client := manager.GetClient()
	store, wrapPersistence, err := buildApplicationKVStore(cfg.KVStore, client)
	if err != nil {
		log.Fatalf("Failed to initialize application KV store: %v", err)
	}
	persistence := client
	if wrapPersistence {
		persistence = kvstore.NewKubernetesAdapter(client, store)
	}
	return sessionInfrastructure{namespace: namespace, k8sManager: manager, manager: manager, persistence: persistence, kvStore: store}
}
