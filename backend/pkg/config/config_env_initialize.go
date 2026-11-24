package config

import (
	"github.com/spf13/viper"
)

func initializeConfigStructsFromEnv(config *Config, v *viper.Viper) {
	initializeBuiltinManagerEnv(config)
	initializePersistenceEnv(config, v)
	warnLegacyStaticAuth(v)
	initializeBootstrapAdminEnv(config, v)
	initializeGitHubEnv(config, v)
	initializeGitHubOAuthEnv(config, v)
	applyWorkerEnv(config)
	applySciaEnv(config)
	applyGitHubEnvOverrides(config, v)
}
