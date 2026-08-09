package runtimeconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestProviderAppliesRuntimeSectionsAndPreservesBase(t *testing.T) {
	pvc := false
	base := &config.Config{
		Auth:              config.AuthConfig{GitHub: &config.GitHubAuthConfig{Enabled: true, UserMapping: config.GitHubUserMapping{DefaultRole: "user"}}},
		KubernetesSession: config.KubernetesSessionConfig{Image: "base:image", CPURequest: "100m", PVCEnabled: &pvc},
	}
	provider := New(base, nil, "default")
	sections := map[string]interface{}{
		"authentication": map[string]interface{}{
			"default_role":      "admin",
			"team_role_mapping": map[string]interface{}{"org/team": map[string]interface{}{"role": "admin", "permissions": []interface{}{"session:access"}}},
		},
		"sessions": map[string]interface{}{"image": "runtime:image", "pvc_enabled": true},
		"agents":   map[string]interface{}{"auth_mode": "bedrock", "env_vars": map[string]interface{}{"SYSTEM_DEFAULT": "enabled"}},
		"storage":  map[string]interface{}{"redis_enabled": true, "redis_address": "runtime-redis:6379", "session_persistence_backend": "s3", "session_persistence_bucket": "runtime-bucket"},
	}

	var notified *config.Config
	provider.Subscribe(func(cfg *config.Config) { notified = cfg })
	require.NoError(t, provider.Apply(7, sections))

	current := provider.Current()
	require.Equal(t, int64(7), provider.Version())
	require.Equal(t, "runtime:image", current.KubernetesSession.Image)
	require.Equal(t, "100m", current.KubernetesSession.CPURequest)
	require.True(t, *current.KubernetesSession.PVCEnabled)
	require.Equal(t, "admin", current.Auth.GitHub.UserMapping.DefaultRole)
	require.Equal(t, []string{"session:access"}, current.Auth.GitHub.UserMapping.TeamRoleMapping["org/team"].Permissions)
	require.Equal(t, "runtime-redis:6379", current.Redis.Addr)
	require.Equal(t, "s3", current.SessionPersistence.Backend)
	require.Equal(t, "runtime-bucket", current.SessionPersistence.S3.Bucket)
	require.NotNil(t, notified)
	require.Equal(t, "runtime:image", notified.KubernetesSession.Image)

	defaults := provider.AgentDefaults()
	require.Equal(t, "bedrock", defaults.AuthMode)
	require.Equal(t, "enabled", defaults.EnvVars["SYSTEM_DEFAULT"])

	current.KubernetesSession.Image = "mutated"
	require.Equal(t, "runtime:image", provider.Current().KubernetesSession.Image)
}
