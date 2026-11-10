package services

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestNormalizeProvisionSettingsRoutesSessionEnvToRunnerParent(t *testing.T) {
	const upstream = "https://cc-api.example.com/api/v1"
	const clusterBroker = "http://agentapi-proxy.ccplant.svc.cluster.local:8080/internal/sessions/public/github-credentials"
	tests := []struct {
		name            string
		runnerParentURL string
		wantEndpoint    string
		wantBroker      string
	}{
		{name: "execution plane with runner parent", runnerParentURL: upstream, wantEndpoint: upstream, wantBroker: upstream + "/internal/sessions/public/github-credentials"},
		{name: "manager without runner parent", wantBroker: clusterBroker},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newResumeEndpointTestManager(t)
			if tt.runnerParentURL != "" {
				m.ConfigureSessionRunnerPool(tt.runnerParentURL, "manager", "manager-token", "pool")
			}
			incoming := &sessionsettings.SessionSettings{Env: map[string]string{sessionsettings.GitHubBrokerURLEnv: clusterBroker}}

			got := m.normalizeProvisionSettings(incoming)

			if endpoint := got.Env[sessionsettings.ProxyEndpointEnv]; endpoint != tt.wantEndpoint {
				t.Fatalf("%s = %q, want %q", sessionsettings.ProxyEndpointEnv, endpoint, tt.wantEndpoint)
			}
			if broker := got.Env[sessionsettings.GitHubBrokerURLEnv]; broker != tt.wantBroker {
				t.Fatalf("%s = %q, want %q", sessionsettings.GitHubBrokerURLEnv, broker, tt.wantBroker)
			}
			if incoming.Env[sessionsettings.GitHubBrokerURLEnv] != clusterBroker {
				t.Fatal("incoming settings were mutated")
			}
			if _, ok := incoming.Env[sessionsettings.ProxyEndpointEnv]; ok {
				t.Fatal("incoming settings were mutated")
			}
		})
	}
}
