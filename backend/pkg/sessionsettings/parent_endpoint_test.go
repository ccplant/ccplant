package sessionsettings

import "testing"

func TestRouteEnvToParent(t *testing.T) {
	const parent = "https://cc-api.example.com/api/v1"
	const clusterBroker = "http://agentapi-proxy.ccplant.svc.cluster.local:8080/internal/sessions/public%2Fid/github-credentials"
	tests := []struct {
		name           string
		parentURL      string
		env            map[string]string
		wantEndpoint   string
		wantBrokerURL  string
		wantNoEndpoint bool
	}{
		{
			name:          "cluster-local broker and missing proxy endpoint",
			parentURL:     parent,
			env:           map[string]string{GitHubBrokerURLEnv: clusterBroker},
			wantEndpoint:  parent,
			wantBrokerURL: parent + "/internal/sessions/public%2Fid/github-credentials",
		},
		{
			name:          "parent URL with trailing slash",
			parentURL:     parent + "/",
			env:           map[string]string{GitHubBrokerURLEnv: clusterBroker},
			wantEndpoint:  parent,
			wantBrokerURL: parent + "/internal/sessions/public%2Fid/github-credentials",
		},
		{
			name:          "explicit proxy endpoint is kept",
			parentURL:     parent,
			env:           map[string]string{ProxyEndpointEnv: "https://custom.example.com"},
			wantEndpoint:  "https://custom.example.com",
			wantBrokerURL: "",
		},
		{
			name:          "broker URL without broker path is kept",
			parentURL:     parent,
			env:           map[string]string{GitHubBrokerURLEnv: "https://broker.example.com/credentials"},
			wantEndpoint:  parent,
			wantBrokerURL: "https://broker.example.com/credentials",
		},
		{
			name:           "no parent URL keeps env",
			env:            map[string]string{GitHubBrokerURLEnv: clusterBroker},
			wantBrokerURL:  clusterBroker,
			wantNoEndpoint: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RouteEnvToParent(tt.env, tt.parentURL)
			endpoint, ok := tt.env[ProxyEndpointEnv]
			if tt.wantNoEndpoint {
				if ok {
					t.Fatalf("%s = %q, want unset", ProxyEndpointEnv, endpoint)
				}
			} else if endpoint != tt.wantEndpoint {
				t.Fatalf("%s = %q, want %q", ProxyEndpointEnv, endpoint, tt.wantEndpoint)
			}
			if got := tt.env[GitHubBrokerURLEnv]; got != tt.wantBrokerURL {
				t.Fatalf("%s = %q, want %q", GitHubBrokerURLEnv, got, tt.wantBrokerURL)
			}
		})
	}
}

func TestRouteEnvToParentNilEnv(t *testing.T) {
	RouteEnvToParent(nil, "https://cc-api.example.com/api/v1")
}
