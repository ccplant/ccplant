package app

import (
	"context"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	serviceaccountuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/service_account"
)

func (s *Server) finishInitialization() {
	s.configureRuntimeServices()
	s.bootstrapAuthenticationData()
	s.configureSessionManagerIntegrations()
	s.setupRoutes()
}

func (s *Server) configureRuntimeServices() {
	if s.verbose {
		s.echo.Use(s.loggingMiddleware())
	}
	s.initializeAuthentication()
	s.initializeNotifications()
	go s.cleanupDefunctProcesses()

	if k8sManager, ok := s.sessionManager.(*services.KubernetesSessionManager); ok && s.sandboxDomainRepo != nil {
		collector := newSandboxDomainCollector(k8sManager, s.sandboxDomainRepo, 60*time.Second)
		go collector.start(context.Background())
		log.Printf("[SERVER] Sandbox domain collector started (interval: 60s)")
	}
	if s.shareRepo != nil {
		go s.cleanupExpiredShares()
	}
}

func (s *Server) bootstrapAuthenticationData() {
	if s.teamConfigRepo != nil {
		if simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService); ok {
			if err := services.BootstrapServiceAccounts(context.Background(), simpleAuth, s.teamConfigRepo); err != nil {
				log.Printf("[SERVER] Warning: failed to bootstrap service accounts: %v", err)
			}
		}
	}

	if k8sManager, ok := s.sessionManager.(*services.KubernetesSessionManager); ok {
		personalAPIKeyRepo := k8sManager.GetPersonalAPIKeyRepository()
		if simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService); ok && personalAPIKeyRepo != nil {
			if err := services.BootstrapPersonalAPIKeys(context.Background(), simpleAuth, personalAPIKeyRepo); err != nil {
				log.Printf("[SERVER] Warning: failed to bootstrap personal API keys: %v", err)
			}
			k8sManager.SetPersonalAPIKeyLoader(simpleAuth)
		}
	}
	s.initAPITokenWiring()
}

func (s *Server) configureSessionManagerIntegrations() {
	if simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService); ok && s.teamConfigRepo != nil {
		if k8sManager, ok := s.sessionManager.(*services.KubernetesSessionManager); ok {
			ensurer := serviceaccountuc.NewGetOrCreateServiceAccountUseCase(s.teamConfigRepo, simpleAuth)
			k8sManager.SetServiceAccountEnsurer(ensurer)
			log.Printf("[SERVER] ServiceAccountEnsurer configured for KubernetesSessionManager")
		}
	}

	if k8sManager, ok := s.sessionManager.(*services.KubernetesSessionManager); ok && s.sessionRouteRepo != nil {
		k8sManager.SetSessionRouteRepository(s.sessionRouteRepo)
		k8sManager.AddSessionDeletedHandler(func(ctx context.Context, sess entities.Session) {
			cleanupLocalSessionRoutes(ctx, s.sessionRouteRepo, sess.ID())
		})
		log.Printf("[SERVER] Local session route cleanup handler registered")
	}
}
