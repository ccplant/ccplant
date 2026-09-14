package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuildRemoteProvisionSettings_ResolvesSlackTokenFromApplicationStore(t *testing.T) {
	ctx := context.Background()
	runtimeClient := fake.NewSimpleClientset()
	store := kvstore.NewKubernetesStore(fake.NewSimpleClientset())
	persistence := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	_, err := persistence.CoreV1().Secrets("application").Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-slack"},
		Data:       map[string][]byte{"bot-token": []byte("test-slack-token")},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "runtime", Image: "test-image", BasePort: 9000, PVCEnabled: boolPtrForTest(false),
	}}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), runtimeClient)
	require.NoError(t, err)
	manager.SetSlackTokenClient(persistence, "application")
	settings, err := manager.BuildRemoteProvisionSettings(ctx, "session", &entities.RunServerRequest{
		UserID: "user", Scope: entities.ScopeUser, AgentType: "claude-acp",
		SlackParams: &entities.SlackParams{Channel: "channel", ThreadTS: "123.456", BotTokenSecretName: "worker-slack"},
	})
	require.NoError(t, err)
	// Exercise the JSON payload consumed by the provisioner, with no Slack Secret
	// in the runtime cluster and no server-default token reference.
	payload, err := json.Marshal(settings)
	require.NoError(t, err)
	var received sessionsettings.SessionSettings
	require.NoError(t, json.Unmarshal(payload, &received))
	require.Equal(t, &sessionsettings.SlackParams{Channel: "channel", ThreadTS: "123.456", BotToken: "test-slack-token"}, received.SlackParams)
}
