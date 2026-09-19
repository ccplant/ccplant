package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigCodexDeviceAuthCallbackBaseURL(t *testing.T) {
	for _, tt := range []struct {
		name string
		file string
		env  string
		want string
	}{
		{name: "unset keeps the request-derived callback URL", file: "{}\n"},
		{name: "YAML setting", file: "codex_device_auth_callback_base_url: https://api.example.test\n", want: "https://api.example.test"},
		{name: "environment only", file: "{}\n", env: "https://ccplant-api-dev.fly.dev", want: "https://ccplant-api-dev.fly.dev"},
		{name: "environment overrides YAML", file: "codex_device_auth_callback_base_url: https://api.example.test\n", env: "https://ccplant-api-dev.fly.dev", want: "https://ccplant-api-dev.fly.dev"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearAGENTAPIEnvVars(t)
			t.Setenv("AGENTAPI_CODEX_DEVICE_AUTH_CALLBACK_BASE_URL", tt.env)
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0600))
			cfg, err := LoadConfig(path)
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.CodexDeviceAuthCallbackBaseURL)
		})
	}
}
