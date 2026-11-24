package app

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func (s *Server) initializeAuthentication() {
	githubAuthProvider := s.initializeGitHubAuthentication()
	s.initializeAdminAuthentication()
	s.echo.Use(auth.AuthMiddleware(s.configProvider, s.container.AuthService))
	s.initializeOAuthProvider(githubAuthProvider)
	s.configProvider.Subscribe(func(updated *config.Config) {
		if githubAuthProvider != nil && updated.Auth.GitHub != nil {
			githubAuthProvider.UpdateConfig(updated.Auth.GitHub)
		}
	})

}

func (s *Server) initializeGitHubAuthentication() *auth.GitHubAuthProvider {
	// Initialize GitHub auth provider if configured.
	// A single *GitHubAuthProvider instance is shared across all subsystems
	// (SimpleAuthService and GitHubOAuthProvider) so they use the same
	// in-memory teamCache and ConfigMap-backed teamMappingRepo.
	var githubAuthProvider *auth.GitHubAuthProvider
	if s.config.Auth.GitHub != nil && s.config.Auth.GitHub.Enabled {
		log.Printf("[AUTH_INIT] Initializing GitHub auth provider...")
		for _, rule := range s.config.TeamDiscovery {
			s.config.Auth.GitHub.TeamDiscoveryPatterns = append(s.config.Auth.GitHub.TeamDiscoveryPatterns, rule.TeamPattern)
		}
		githubAuthProvider = auth.NewGitHubAuthProvider(s.config.Auth.GitHub)

		// Inject ConfigMap-backed team mapping cache (1 user = 1 key in the ConfigMap)
		teamMappingRepo := repositories.NewKubernetesUserTeamMappingRepository(
			s.persistenceClient,
			s.namespace,
		)
		githubAuthProvider.SetTeamMappingRepo(teamMappingRepo)
		log.Printf("[AUTH_INIT] GitHub auth provider initialized with ConfigMap team mapping cache")

		// Inject the shared provider into SimpleAuthService.
		if simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService); ok {
			simpleAuth.SetGitHubProvider(githubAuthProvider)
			simpleAuth.SetGitHubAuthConfig(s.config.Auth.GitHub)
			log.Printf("[AUTH_INIT] GitHub auth provider injected into internal auth service")
		}
	}

	return githubAuthProvider
}

func (s *Server) initializeAdminAuthentication() {
	// Add authentication middleware using internal auth service
	if bootstrap := s.config.Auth.BootstrapAdmin; bootstrap != nil && bootstrap.Enabled {
		if simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService); ok {
			if err := simpleAuth.LoadBootstrapAdmin(bootstrap.UserID, bootstrap.Username, bootstrap.Token); err != nil {
				log.Fatalf("[AUTH_INIT] Invalid bootstrap admin configuration: %v", err)
			}
			log.Printf("[AUTH_INIT] Bootstrap admin authentication enabled for user %q", bootstrap.UserID)
		}
	}
	// Register the single admin API key from AGENTAPI_AUTH_ADMIN_KEY as a
	// non-expiring admin credential for the X-API-Key header. This replaces the
	// removed static API key authentication (ccplant-deploy#66 F1).
	if s.config.Auth.AdminKey != "" {
		if simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService); ok {
			if err := simpleAuth.LoadBootstrapAdmin("admin-key", "admin-key", s.config.Auth.AdminKey); err != nil {
				log.Fatalf("[AUTH_INIT] Invalid AGENTAPI_AUTH_ADMIN_KEY configuration: %v", err)
			}
			log.Printf("[AUTH_INIT] Admin key authentication enabled (user \"admin-key\", header X-API-Key)")
		} else {
			log.Printf("[AUTH_INIT] Warning: AGENTAPI_AUTH_ADMIN_KEY is set but the auth service does not support API keys; it will be ignored")
		}
	}
}

func (s *Server) initializeOAuthProvider(githubAuthProvider *auth.GitHubAuthProvider) {
	// Initialize OAuth provider if configured.
	// Reuses the shared githubAuthProvider so OAuth-authenticated users benefit from
	// the same teamCache and teamMappingRepo as token-based auth users.
	if s.config.Auth.GitHub != nil && s.config.Auth.GitHub.OAuth != nil &&
		s.config.Auth.GitHub.OAuth.ClientID != "" && s.config.Auth.GitHub.OAuth.ClientSecret != "" {
		log.Printf("[OAUTH_INIT] Initializing GitHub OAuth provider...")
		s.oauthProvider = auth.NewGitHubOAuthProvider(s.config.Auth.GitHub.OAuth, githubAuthProvider)
		log.Printf("[OAUTH_INIT] OAuth provider initialized successfully")
		// Start cleanup goroutine for expired OAuth sessions
		go s.cleanupExpiredOAuthSessions()
	} else {
		log.Printf("[OAUTH_INIT] OAuth provider not initialized - configuration missing or incomplete")
	}
}
