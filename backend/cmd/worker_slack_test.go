package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestConfigureWorkerSlackCredential(t *testing.T) {
	cfg := &config.Config{Slack: config.SlackConfig{AppToken: "xapp-test", BotToken: "xoxb-test"}}

	require.NoError(t, configureWorkerSlackCredential(cfg))
	require.Equal(t, workerDefaultSlackSecretName, cfg.Slack.AppTokenSecretName)
	require.Equal(t, workerDefaultSlackSecretName, cfg.KubernetesSession.SlackBotTokenSecretName)
}

func TestConfigureWorkerSlackCredentialRequiresPair(t *testing.T) {
	cfg := &config.Config{Slack: config.SlackConfig{AppToken: "xapp-test"}}

	require.ErrorContains(t, configureWorkerSlackCredential(cfg), "requires both")
}
