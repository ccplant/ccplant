package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResolveBaseSettingsSource(t *testing.T) {
	for _, tc := range []struct {
		name       string
		baseName   string
		injected   bool
		wantServer string
	}{
		{"legacy workload store", "agentapi-settings-base", false, "workload"},
		{"application store takes precedence", "agentapi-settings-base", true, "application"},
		{"missing base is optional", "absent", true, ""},
		{"empty name disables base", "", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			secret := func(namespace, server string) *corev1.Secret {
				return &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "agentapi-settings-base", Namespace: namespace},
					Data:       map[string][]byte{"settings.json": []byte(`{"mcp_servers":{"` + server + `":{"type":"http","url":"https://example.com/mcp"}}}`)},
				}
			}
			manager := &KubernetesSessionManager{
				client:    fake.NewSimpleClientset(secret("workloads", "workload")),
				namespace: "workloads",
				k8sConfig: &config.KubernetesSessionConfig{SettingsBaseSecret: tc.baseName},
			}
			if tc.injected {
				manager.SetSettingsSecretClient(fake.NewSimpleClientset(secret("app", "application")), "app")
			}
			settings := manager.resolveSettings(context.Background(), nil, &entities.RunServerRequest{})
			if tc.wantServer == "" {
				require.Empty(t, settings.MCPServers)
			} else {
				require.Len(t, settings.MCPServers, 1)
				require.Contains(t, settings.MCPServers, tc.wantServer)
			}
		})
	}
}
