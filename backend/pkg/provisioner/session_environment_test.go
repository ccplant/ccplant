package provisioner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionProcessEnvironmentExcludesProvisionerAPIKey(t *testing.T) {
	env := sessionProcessEnvironment(
		[]string{"AGENTAPI_KEY=parent-secret", "REMOVE_ME=parent", "PATH=/bin"},
		[]string{"REMOVE_ME"},
		map[string]string{
			"AGENTAPI_KEY":            "overlay-secret",
			"AGENTAPI_PROXY_ENDPOINT": "http+unix://%2Ftmp%2Fccplant%2Fprovisioner.sock",
		},
	)

	require.Contains(t, env, "PATH=/bin")
	require.Contains(t, env, "AGENTAPI_PROXY_ENDPOINT=http+unix://%2Ftmp%2Fccplant%2Fprovisioner.sock")
	require.NotContains(t, env, "AGENTAPI_KEY=parent-secret")
	require.NotContains(t, env, "AGENTAPI_KEY=overlay-secret")
	require.NotContains(t, env, "REMOVE_ME=parent")
}
