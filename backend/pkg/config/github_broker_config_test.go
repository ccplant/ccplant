package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigGitHubBrokerBaseURL(t *testing.T) {
	for _, tt := range []struct {
		name string
		file string
		env  string
		want string
	}{
		{name: "unset preserves request-derived URLs", file: "{}\n"},
		{name: "YAML setting", file: "github_broker_base_url: https://broker.example.test/api/proxy\n", want: "https://broker.example.test/api/proxy"},
		{name: "environment only", file: "{}\n", env: "http://backend.internal:8080", want: "http://backend.internal:8080"},
		{name: "environment overrides YAML", file: "github_broker_base_url: https://broker.example.test/api/proxy\n", env: "http://backend.internal:8080", want: "http://backend.internal:8080"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearAGENTAPIEnvVars(t)
			t.Setenv("AGENTAPI_GITHUB_BROKER_BASE_URL", tt.env)
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0600))
			cfg, err := LoadConfig(path)
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.GitHubBrokerBaseURL)
		})
	}
}
