package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResolveConnectionsAndProfileModel(t *testing.T) {
	personal := entities.NewSettings("user")
	personal.SetCodexConnection(&modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://personal.example/v1", Model: "default", Authentication: "api_key", APIKey: "personal-key"})
	personal.SetClaudeConnection(&modelprovider.Connection{Mode: "anthropic_compatible", BaseURL: "https://personal.example/anthropic", Model: "claude-default", Authentication: "bearer_token", APIKey: "claude-key"})
	team := entities.NewSettings("org/team")
	team.SetCodexConnection(&modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://team.example/v1", Model: "team-model", Authentication: "api_key", APIKey: "team-key"})
	manager := &KubernetesSessionManager{client: fake.NewSimpleClientset(), settingsRepo: &fakeSettingsRepository{settings: map[string]*entities.Settings{"user": personal, "org/team": team}}}
	req := &entities.RunServerRequest{UserID: "user", AgentType: "auto", ProfileEnvironment: map[string]string{"CODEX_MODEL": "profile", "ANTHROPIC_MODEL": "claude-profile"}, Environment: map[string]string{"OPENAI_MODEL": "request"}}
	require.NoError(t, manager.prepareModelConnections(context.Background(), req))
	require.Equal(t, "codex-acp", manager.resolveAutoAgentType(context.Background(), req))
	require.Equal(t, "request", req.CodexConnection.Model)
	require.Equal(t, "personal-key", req.CodexConnection.APIKey)
	require.Equal(t, "default", personal.CodexConnection().Model)
	req.AgentType = "claude-acp"
	settings := &sessionsettings.SessionSettings{}
	applyModelConnections(settings, req)
	require.Nil(t, settings.CodexConnection)
	require.Equal(t, "claude-profile", settings.Env["ANTHROPIC_MODEL"])
	require.NotContains(t, settings.Env, "CCPLANT_CODEX_API_KEY")
	req = &entities.RunServerRequest{UserID: "user", Scope: entities.ScopeTeam, TeamID: "org/team", CredentialSource: "team", AgentType: "codex-acp"}
	require.NoError(t, manager.prepareModelConnections(context.Background(), req))
	require.Equal(t, "team-key", req.CodexConnection.APIKey)
	req = &entities.RunServerRequest{Scope: entities.ScopeTeam, TeamID: "org/team"}
	require.NoError(t, manager.prepareModelConnections(context.Background(), req))
	require.Equal(t, "team-key", req.CodexConnection.APIKey)
	req = &entities.RunServerRequest{AgentType: "", ClaudeConnection: personal.ClaudeConnection()}
	legacy := &sessionsettings.SessionSettings{}
	applyModelConnections(legacy, req)
	require.Equal(t, "claude-key", legacy.Env["ANTHROPIC_AUTH_TOKEN"])
	req = &entities.RunServerRequest{UserID: "user", CredentialSource: "none"}
	require.NoError(t, manager.prepareModelConnections(context.Background(), req))
	require.Nil(t, req.CodexConnection)
	require.Nil(t, req.ClaudeConnection)
	req = &entities.RunServerRequest{UserID: "user", ProfileEnvironment: map[string]string{"OPENAI_BASE_URL": "https://other.example"}}
	require.Error(t, manager.prepareModelConnections(context.Background(), req))
	require.False(t, req.ModelConnectionsResolved)
	require.Error(t, manager.prepareModelConnections(context.Background(), req))
}

func TestSessionAuthenticationOverrides(t *testing.T) {
	personal := entities.NewSettings("user")
	personal.SetCodexConnection(&modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://example.com/v1", Model: "default", Authentication: "api_key", APIKey: "secret"})
	personal.SetClaudeConnection(&modelprovider.Connection{Mode: "anthropic_compatible", BaseURL: "https://example.com", Model: "default", Authentication: "api_key", APIKey: "secret"})
	manager := &KubernetesSessionManager{client: fake.NewSimpleClientset(), settingsRepo: &fakeSettingsRepository{settings: map[string]*entities.Settings{"user": personal}}}
	for _, mode := range []string{"oauth", "bedrock"} {
		req := &entities.RunServerRequest{UserID: "user", AgentType: "claude-acp", CodexAuthMode: "auth_json", ClaudeAuthMode: mode}
		require.NoError(t, manager.prepareModelConnections(context.Background(), req))
		require.Equal(t, "auth_json", req.CodexConnection.Mode)
		require.Equal(t, mode, req.ClaudeConnection.Mode)
		require.Empty(t, req.ClaudeConnection.APIKey)
		settings := &sessionsettings.SessionSettings{Env: map[string]string{"ANTHROPIC_API_KEY": "stale", "ANTHROPIC_BASE_URL": "https://stale.example"}}
		applyModelConnections(settings, req)
		require.NotContains(t, settings.Env, "ANTHROPIC_API_KEY")
		require.NotContains(t, settings.Env, "ANTHROPIC_BASE_URL")
	}
	require.Equal(t, "openai_compatible", personal.CodexConnection().Mode)
	require.Equal(t, "anthropic_compatible", personal.ClaudeConnection().Mode)
	req := &entities.RunServerRequest{UserID: "user", CodexAuthMode: "openai_compatible", ProfileEnvironment: map[string]string{"CODEX_MODEL": "override"}}
	require.NoError(t, manager.prepareModelConnections(context.Background(), req))
	require.Equal(t, "override", req.CodexConnection.Model)
	for _, req := range []*entities.RunServerRequest{
		{CodexAuthMode: "invalid"}, {ClaudeAuthMode: "invalid"},
		{CodexAuthMode: "openai_compatible"}, {ClaudeAuthMode: "anthropic_compatible"},
		{UserID: "user", CredentialSource: "none", CodexAuthMode: "openai_compatible"},
	} {
		require.Error(t, manager.prepareModelConnections(context.Background(), req))
		require.False(t, req.ModelConnectionsResolved)
	}
}
