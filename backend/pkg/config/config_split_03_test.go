package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionCLIImageConfig(t *testing.T) {
	t.Setenv("AGENTAPI_K8S_SESSION_CLI_IMAGE", "example/cli:v2")
	loaded, err := LoadConfig("")
	assert.NoError(t, err)
	assert.Equal(t, "example/cli:v2", loaded.KubernetesSession.CLIImage)
}

func TestSessionManagerBuiltinRegistrationFromEnv(t *testing.T) {
	t.Setenv("AGENTAPI_SESSION_MANAGER_BUILTIN_ENABLED", "true")
	t.Setenv("AGENTAPI_SESSION_MANAGER_BUILTIN_MANAGER_ID", "builtin-dev")
	t.Setenv("AGENTAPI_SESSION_MANAGER_BUILTIN_CONNECTION_TOKEN", "connection-token")
	t.Setenv("AGENTAPI_SESSION_MANAGER_BUILTIN_POOL", "builtin")
	t.Setenv("AGENTAPI_SESSION_MANAGER_BUILTIN_POOL_LABELS", `{"environment":"dev"}`)

	loaded, err := LoadConfig("")
	assert.NoError(t, err)
	assert.Equal(t, "builtin-dev", loaded.SessionManager.Builtin.ManagerID)
	assert.Equal(t, "connection-token", loaded.SessionManager.Builtin.ConnectionToken)
	assert.Equal(t, "builtin", loaded.SessionManager.Builtin.Pool.Name)
	assert.Equal(t, "dev", loaded.SessionManager.Builtin.Pool.Labels["environment"])
}
