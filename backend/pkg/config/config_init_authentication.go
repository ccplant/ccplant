package config

import (
	"log"
	"os"

	"github.com/spf13/viper"
)

func warnLegacyStaticAuth(v *viper.Viper) {
	// Static API key authentication was removed. Warn loudly when its legacy
	// settings are still present so operators do not assume they take effect.
	if v.IsSet("auth.static") || v.GetBool("auth.static.enabled") || os.Getenv("AGENTAPI_AUTH_STATIC_ENABLED") != "" || os.Getenv("AGENTAPI_AUTH_STATIC_HEADER_NAME") != "" {
		log.Printf("[CONFIG] Warning: static API key authentication has been removed; ignoring legacy auth.static settings. Set AGENTAPI_AUTH_ADMIN_KEY to provide a single admin API key instead.")
	}
}

func initializeBootstrapAdminEnv(config *Config, v *viper.Viper) {
	// Initialize Auth.BootstrapAdmin if environment variables are set
	if config.Auth.BootstrapAdmin == nil && (v.GetBool("auth.bootstrap_admin.enabled") || v.GetString("auth.bootstrap_admin.token") != "") {
		config.Auth.BootstrapAdmin = &BootstrapAdminAuthConfig{
			Enabled:  v.GetBool("auth.bootstrap_admin.enabled"),
			UserID:   v.GetString("auth.bootstrap_admin.user_id"),
			Username: v.GetString("auth.bootstrap_admin.username"),
			Token:    v.GetString("auth.bootstrap_admin.token"),
		}
	}
}

func initializeGitHubEnv(config *Config, v *viper.Viper) {
	// Initialize Auth.GitHub if environment variables are set
	if config.Auth.GitHub == nil && (v.GetBool("auth.github.enabled") || v.GetString("auth.github.base_url") != "" || v.GetString("auth.github.token_header") != "") {
		config.Auth.GitHub = &GitHubAuthConfig{
			Enabled:      v.GetBool("auth.github.enabled"),
			ConnectionID: v.GetString("auth.github.connection_id"),
			BaseURL:      v.GetString("auth.github.base_url"),
			TokenHeader:  v.GetString("auth.github.token_header"),
			UserMapping: GitHubUserMapping{
				DefaultRole:           v.GetString("auth.github.user_mapping.default_role"),
				DefaultPermissions:    v.GetStringSlice("auth.github.user_mapping.default_permissions"),
				AllowUsersWithoutTeam: v.GetBool("auth.github.user_mapping.allow_users_without_team"),
			},
		}
		log.Printf("[CONFIG] Initialized GitHub auth config from environment variables")
	}
}

func initializeGitHubOAuthEnv(config *Config, v *viper.Viper) {
	// Initialize Auth.GitHub.OAuth if environment variables are set
	if config.Auth.GitHub != nil && config.Auth.GitHub.OAuth == nil {
		// Check if OAuth environment variables are set
		clientID := v.GetString("auth.github.oauth.client_id")
		clientSecret := v.GetString("auth.github.oauth.client_secret")

		if clientID != "" || clientSecret != "" {
			config.Auth.GitHub.OAuth = &GitHubOAuthConfig{
				ClientID:     clientID,
				ClientSecret: clientSecret,
				Scope:        v.GetString("auth.github.oauth.scope"),
				BaseURL:      v.GetString("auth.github.oauth.base_url"),
			}
			log.Printf("[CONFIG] Initialized GitHub OAuth config from environment variables")
			log.Printf("[CONFIG] OAuth ClientID from env: %v", clientID != "")
			log.Printf("[CONFIG] OAuth ClientSecret from env: %v", clientSecret != "")
		}
	}
}

func applyGitHubEnvOverrides(config *Config, v *viper.Viper) {
	// Override fields if environment variables are set (even if structures already exist)
	if config.Auth.GitHub != nil {
		if v.IsSet("auth.github.user_mapping.default_role") {
			config.Auth.GitHub.UserMapping.DefaultRole = v.GetString("auth.github.user_mapping.default_role")
		}
		if v.IsSet("auth.github.user_mapping.default_permissions") {
			config.Auth.GitHub.UserMapping.DefaultPermissions = v.GetStringSlice("auth.github.user_mapping.default_permissions")
		}
		if v.IsSet("auth.github.user_mapping.allow_users_without_team") {
			config.Auth.GitHub.UserMapping.AllowUsersWithoutTeam = v.GetBool("auth.github.user_mapping.allow_users_without_team")
		}

		// Override OAuth settings if already exists
		if config.Auth.GitHub.OAuth != nil {
			if clientID := v.GetString("auth.github.oauth.client_id"); clientID != "" {
				config.Auth.GitHub.OAuth.ClientID = clientID
			}
			if clientSecret := v.GetString("auth.github.oauth.client_secret"); clientSecret != "" {
				config.Auth.GitHub.OAuth.ClientSecret = clientSecret
			}
			if scope := v.GetString("auth.github.oauth.scope"); scope != "" {
				config.Auth.GitHub.OAuth.Scope = scope
			}
			if baseURL := v.GetString("auth.github.oauth.base_url"); baseURL != "" {
				config.Auth.GitHub.OAuth.BaseURL = baseURL
			}
		}
	}
}
