package services

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/settingspatch"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestProfileCanDisableTeamSettingsWithoutChangingStoredSettings(t *testing.T) {
	for _, inherit := range []bool{true, false} {
		for _, preferred := range []string{"", "org/team"} {
			for _, selected := range []string{"", "org/team"} {
				t.Run(fmt.Sprintf("inherit=%t/preferred=%s/selected=%s", inherit, preferred, selected), func(t *testing.T) {
					ctx := context.Background()
					personal := entities.NewSettings("user")
					personal.SetPreferredTeamID(preferred)
					personal.SetAuthMode(entities.AuthModeOAuth)
					personal.SetClaudeCodeOAuthToken("personal-oauth")
					personal.SetEnvVars(map[string]string{"PERSONAL": "yes"})
					team := entities.NewSettings("org/team")
					team.SetEnvVars(map[string]string{"TEAM": "yes"})
					team.SetEnabledPlugins([]string{"team@market"})
					team.SetCodexConnection(&modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://team.example/v1", Model: "test-model", Authentication: "none"})
					teamMCP := entities.NewMCPServersSettings()
					server := entities.NewMCPServer("team", "http")
					server.SetURL("https://team.example/mcp")
					teamMCP.SetServer("team", server)
					team.SetMCPServers(teamMCP)
					common, err := json.Marshal(settingspatch.SettingsPatch{
						EnvVars: map[string]string{"COMMON": "yes"}, EnabledPlugins: []string{"common@market"},
						MCPServers: map[string]*settingspatch.MCPServerPatch{"common": {Type: "http", URL: "https://common.example/mcp"}},
					})
					require.NoError(t, err)
					manager := &KubernetesSessionManager{
						client:    fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-settings-base", Namespace: "test"}, Data: map[string][]byte{"settings.json": common}}),
						namespace: "test", k8sConfig: &config.KubernetesSessionConfig{SettingsBaseSecret: "agentapi-settings-base"},
						settingsRepo: &fakeSettingsRepository{settings: map[string]*entities.Settings{"user": personal, "org/team": team}},
					}
					profile := entities.NewSessionProfile("profile", "Profile", "user")
					cfg := entities.NewSessionProfileConfig()
					cfg.SetInheritTeamSettings(inherit)
					// Explicit team selection must also be ignored when disabled.
					cfg.SetSettingsTeamID(selected)
					profile.SetConfig(cfg)
					manager.SetSessionProfileRepository(profileConnectionRepository{profile: profile})
					req := &entities.RunServerRequest{UserID: "user", Teams: []string{"org/team"}, ResolvedSessionProfileID: "profile"}
					require.NoError(t, manager.prepareModelConnections(ctx, req))
					resolved := manager.resolveSettings(ctx, nil, req)
					require.Equal(t, "yes", resolved.EnvVars["COMMON"])
					require.Contains(t, resolved.MCPServers, "common")
					require.Contains(t, resolved.ActivePlugins, "common@market")
					if inherit {
						require.Equal(t, "yes", resolved.EnvVars["TEAM"])
						require.Contains(t, resolved.MCPServers, "team")
						require.Contains(t, resolved.ActivePlugins, "team@market")
						if selected != "" {
							require.NotNil(t, req.CodexConnection)
							require.Equal(t, []string{"org/team"}, credentialOwnersForRequest(req))
						} else {
							require.Nil(t, req.CodexConnection)
							require.Equal(t, []string{"user"}, credentialOwnersForRequest(req))
						}
					} else {
						require.Equal(t, "yes", resolved.EnvVars["PERSONAL"])
						require.Equal(t, "personal-oauth", resolved.EnvVars["CLAUDE_CODE_OAUTH_TOKEN"])
						require.NotContains(t, resolved.EnvVars, "TEAM")
						require.NotContains(t, resolved.MCPServers, "team")
						require.NotContains(t, resolved.ActivePlugins, "team@market")
						require.Nil(t, req.CodexConnection)
						require.Equal(t, []string{"user"}, credentialOwnersForRequest(req))
					}
					require.Equal(t, preferred, personal.PreferredTeamID())
					require.Equal(t, "yes", team.EnvVars()["TEAM"])
				})
			}
		}
	}
}
