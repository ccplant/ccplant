package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeCLIConfig(t *testing.T, contents string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	directory := filepath.Join(home, ".ccplant")
	require.NoError(t, os.MkdirAll(directory, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "config.yaml"), []byte(contents), 0o600))
}

func clearCLIEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("AGENTAPI_PROXY_ENDPOINT", "")
	t.Setenv("AGENTAPI_PROXY_SERVICE_HOST", "")
	t.Setenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP", "")
	t.Setenv("AGENTAPI_KEY", "")
}

func TestResolveCLIConnectionFromConfig(t *testing.T) {
	clearCLIEnvironment(t)
	writeCLIConfig(t, "api_endpoint: https://ccplant.example.com\napi_token: config-token\n")

	endpoint, token, err := resolveCLIConnection(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, "https://ccplant.example.com", endpoint)
	require.Equal(t, "config-token", token)
}

func TestResolveCLIConnectionPrecedence(t *testing.T) {
	clearCLIEnvironment(t)
	writeCLIConfig(t, "api_endpoint: https://config.example.com\napi_token: config-token\n")
	t.Setenv("AGENTAPI_PROXY_ENDPOINT", "https://env.example.com")
	t.Setenv("AGENTAPI_KEY", "env-token")

	endpoint, token, err := resolveCLIConnection(context.Background(), "https://flag.example.com", "flag-token")
	require.NoError(t, err)
	require.Equal(t, "https://flag.example.com", endpoint)
	require.Equal(t, "flag-token", token)

	endpoint, token, err = resolveCLIConnection(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, "https://env.example.com", endpoint)
	require.Equal(t, "env-token", token)
}

func TestResolveCLITokenFromHelper(t *testing.T) {
	clearCLIEnvironment(t)
	writeCLIConfig(t, `api_endpoint: https://ccplant.example.com
api_token_helper:
  command: sh
  args: ["-c", "printf 'helper-token\\n'"]
`)

	_, token, err := resolveCLIConnection(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, "helper-token", token)
}

func TestResolveCLITokenHelperFailureDoesNotExposeOutput(t *testing.T) {
	clearCLIEnvironment(t)
	writeCLIConfig(t, `api_endpoint: https://ccplant.example.com
api_token_helper:
  command: sh
  args: ["-c", "echo secret-output >&2; exit 1"]
`)

	_, _, err := resolveCLIConnection(context.Background(), "", "")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-output")
}

func TestResolveCLIConnectionWithoutConfig(t *testing.T) {
	clearCLIEnvironment(t)
	t.Setenv("HOME", t.TempDir())

	_, _, err := resolveCLIConnection(context.Background(), "", "")
	require.ErrorContains(t, err, "AGENTAPI_PROXY_ENDPOINT")
}
