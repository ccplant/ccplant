package services

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
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

func TestResolveSettingsReadsLegacyMCPBaseFromApplicationStore(t *testing.T) {
	const namespace = "app"
	legacy := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mcp-servers-base", Namespace: namespace},
		Data:       map[string][]byte{"mcp-servers.json": []byte(`{"mcpServers":{"legacy":{"type":"http","url":"https://legacy.example/mcp"},"shared":{"type":"http","url":"https://legacy.example/shared"}}}`)},
	}
	unified := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-settings-base", Namespace: namespace},
		Data:       map[string][]byte{"settings.json": []byte(`{"mcp_servers":{"shared":{"type":"http","url":"https://settings.example/shared"}}}`)},
	}
	manager := &KubernetesSessionManager{
		client:    fake.NewSimpleClientset(),
		namespace: "workloads",
		k8sConfig: &config.KubernetesSessionConfig{
			MCPServersBaseSecret: "mcp-servers-base",
			SettingsBaseSecret:   "agentapi-settings-base",
		},
	}
	manager.SetSettingsSecretClient(fake.NewSimpleClientset(legacy, unified), namespace)

	settings := manager.resolveSettings(context.Background(), nil, &entities.RunServerRequest{})
	require.Len(t, settings.MCPServers, 2)
	legacyServer, ok := settings.MCPServers["legacy"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "https://legacy.example/mcp", legacyServer["url"])
	sharedServer, ok := settings.MCPServers["shared"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "https://settings.example/shared", sharedServer["url"])
}

func TestResolveSettingsReadsLegacyMCPBaseFromLibSQL(t *testing.T) {
	ctx := context.Background()
	const namespace = "app"
	store, err := kvstore.NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "settings.db"), "")
	require.NoError(t, err)
	persistence := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	_, err = persistence.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mcp-servers-base", Namespace: namespace},
		Data:       map[string][]byte{"mcp-servers.json": []byte(`{"mcpServers":{"global":{"type":"http","url":"https://global.example/mcp"}}}`)},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	manager := &KubernetesSessionManager{
		client:    fake.NewSimpleClientset(),
		namespace: "workloads",
		k8sConfig: &config.KubernetesSessionConfig{MCPServersBaseSecret: "mcp-servers-base"},
	}
	manager.SetSettingsSecretClient(persistence, namespace)

	settings := manager.resolveSettings(ctx, nil, &entities.RunServerRequest{})
	require.Contains(t, settings.MCPServers, "global")
}
