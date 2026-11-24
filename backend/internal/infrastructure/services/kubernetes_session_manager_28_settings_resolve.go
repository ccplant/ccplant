package services

import (
	"context"
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/settingspatch"
)

func (m *KubernetesSessionManager) resolveSettings(
	ctx context.Context,
	session *KubernetesSession,
	req *entities.RunServerRequest,
) settingspatch.MaterializedSettings {
	m.refreshConfig()
	var layers []settingspatch.SettingsPatch
	if m.configProvider != nil {
		layers = append(layers, m.configProvider.AgentDefaults())
	}

	appendSettingsIfExists := func(settingsName string) {
		if p := m.readSettingsPatchByName(ctx, settingsName); p != nil {
			layers = append(layers, *p)
		}
	}

	// 1. legacy MCP base, then unified base. The unified base wins on conflicts.
	if p := m.readLegacyBaseMCPPatch(ctx, m.k8sConfig.MCPServersBaseSecret); p != nil {
		layers = append(layers, *p)
	}
	if m.k8sConfig.SettingsBaseSecret != "" {
		var base *settingspatch.SettingsPatch
		if m.k8sConfig.SettingsBaseSecret == "agentapi-settings-base" {
			base = m.readSettingsPatchByName(ctx, "base")
		} else {
			base = m.readSettingsPatch(ctx, m.k8sConfig.SettingsBaseSecret)
		}
		if base != nil {
			layers = append(layers, *base)
		} else {
			log.Printf("[K8S_SESSION] Warning: base settings secret %s could not be read", m.k8sConfig.SettingsBaseSecret)
		}
	}

	// 2. teams (in order)
	if req.SettingsTeamID != "" {
		appendSettingsIfExists(req.SettingsTeamID)
	} else if req.Scope == entities.ScopeTeam && req.TeamID != "" {
		// Team-scoped session: always use the specified team only (preferred_team_id is ignored)
		appendSettingsIfExists(req.TeamID)
		if req.CredentialSource == "github_sender" && req.TriggeredUserID != "" {
			if senderSettings := m.readSettingsPatchByName(ctx, req.TriggeredUserID); senderSettings != nil {
				layers = append(layers, settingspatch.SettingsPatch{
					AuthMode:   senderSettings.AuthMode,
					OAuthToken: senderSettings.OAuthToken,
					EnvVars:    cloneStringMap(senderSettings.EnvVars),
				})
			}
		}
	} else {
		// User-scoped session: check if the user has a preferred team set
		preferredTeamID := m.resolvePreferredTeamID(ctx, req)
		if preferredTeamID != "" {
			// Use only the preferred team's settings
			log.Printf("[K8S_SESSION] Using preferred team settings: %s", preferredTeamID)
			appendSettingsIfExists(preferredTeamID)
		} else {
			// Default: apply all teams in order
			for _, team := range req.Teams {
				appendSettingsIfExists(team)
			}
		}
		// 3. user
		if req.UserID != "" {
			appendSettingsIfExists(req.UserID)
		}
	}

	// 4. session profile
	if req.ProfileMCPServers != nil && !req.ProfileMCPServers.IsEmpty() {
		layers = append(layers, settingsToMCPProfilePatch(req.ProfileMCPServers))
	}
	if len(req.ProfileSkills) > 0 {
		layers = append(layers, settingspatch.SettingsPatch{Skills: append([]string(nil), req.ProfileSkills...)})
	}

	resolved := settingspatch.Resolve(layers...)
	// Materialize credentials for the selected session method, including when
	// a legacy oneshot patch contains a different default authentication mode.
	if req.ClaudeAuthMode != "" {
		resolved.AuthMode = req.ClaudeAuthMode
	}
	materialized, err := settingspatch.Materialize(resolved)
	if err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to materialize settings: %v", err)
	}
	return materialized
}

func settingsToMCPProfilePatch(servers *entities.MCPServersSettings) settingspatch.SettingsPatch {
	patch := settingspatch.SettingsPatch{MCPServers: make(map[string]*settingspatch.MCPServerPatch)}
	for name, server := range servers.Servers() {
		patch.MCPServers[name] = &settingspatch.MCPServerPatch{
			Type: server.Type(), URL: server.URL(), Command: server.Command(),
			Args: append([]string(nil), server.Args()...), Env: cloneMCPStringMap(server.Env()),
			Headers: cloneMCPStringMap(server.Headers()),
		}
	}
	return patch
}

func cloneMCPStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

// resolvePreferredTeamID reads the user's personal settings to determine if a preferred team
// is configured. Returns the preferred team ID if it is set and the user belongs to that team,
// otherwise returns "".
func (m *KubernetesSessionManager) resolvePreferredTeamID(ctx context.Context, req *entities.RunServerRequest) string {
	if req.UserID == "" {
		return ""
	}
	userPatch := m.readSettingsPatchByName(ctx, req.UserID)
	if userPatch == nil {
		userSecretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(req.UserID))
		userPatch = m.readSettingsPatch(ctx, userSecretName)
	}
	if userPatch == nil || userPatch.PreferredTeamID == "" {
		return ""
	}
	// Security check: ensure the preferred team is one the user actually belongs to
	for _, team := range req.Teams {
		if team == userPatch.PreferredTeamID {
			return userPatch.PreferredTeamID
		}
	}
	log.Printf("[K8S_SESSION] Warning: preferred_team_id %q is not in user's team list, falling back to all teams", userPatch.PreferredTeamID)
	return ""
}

// BuildRemoteProvisionSettings implements portrepos.RemoteProvisionSettingsBuilder.
// It builds a fully-resolved SessionSettings for forwarding to an external session manager (External Session Manager).
// All settings layers (base → team → user) are resolved so that External Session Manager can create the session
// without needing to re-resolve secrets from its own cluster.
func (m *KubernetesSessionManager) BuildRemoteProvisionSettings(
	ctx context.Context,
	sessionID string,
	req *entities.RunServerRequest,
) (*sessionsettings.SessionSettings, error) {
	if err := m.prepareModelConnections(ctx, req); err != nil {
		return nil, err
	}
	req.AgentType = m.resolveAutoAgentType(ctx, req)
	req.AgentType = supportedAgentTypeOrDefault(req.AgentType)
	applySelectedAgentDefaultModel(req)
	// Create a temporary session with the provided ID to satisfy buildSessionSettings
	tempSession := &KubernetesSession{
		id:          sessionID,
		serviceName: fmt.Sprintf("agentapi-session-%s-svc", sessionID),
		request:     req,
	}
	settings := m.buildSessionSettings(ctx, tempSession, req, nil)
	return settings, nil
}
