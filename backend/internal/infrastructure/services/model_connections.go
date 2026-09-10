package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// Resolve once so auto selection, local allocation and remote provisioning agree.
func (m *KubernetesSessionManager) prepareModelConnections(ctx context.Context, req *entities.RunServerRequest) error {
	if req.ProvisionSettings != nil || req.ModelConnectionsResolved {
		return nil
	}
	if err := modelprovider.ValidateAuthModes(req.CodexAuthMode, req.ClaudeAuthMode); err != nil {
		return err
	}
	if strings.TrimSpace(req.Model) != "" {
		if err := modelprovider.ValidateModel(req.Model); err != nil {
			return err
		}
		req.Model = strings.TrimSpace(req.Model)
	}
	var profileConfig entities.SessionProfileConfig
	req.SettingsTeamID = ""
	if req.ResolvedSessionProfileID != "" {
		if m.sessionProfileRepo == nil {
			return fmt.Errorf("session profile repository unavailable")
		}
		profile, err := m.sessionProfileRepo.Get(ctx, req.ResolvedSessionProfileID)
		if err != nil {
			return fmt.Errorf("failed to load session profile connection")
		}
		profileConfig = profile.Config()
		team := profileConfig.SettingsTeamID()
		if team != "" {
			allowed := false
			if req.Scope == entities.ScopeTeam {
				allowed = req.TeamID == team
			} else {
				for _, memberTeam := range req.Teams {
					if memberTeam == team {
						allowed = true
						break
					}
				}
			}
			if !allowed {
				return fmt.Errorf("team membership is required to inherit profile settings")
			}
			if m.settingsRepo == nil {
				return fmt.Errorf("team settings unavailable")
			}
			exists, err := m.settingsRepo.Exists(ctx, team)
			if err != nil || !exists {
				return fmt.Errorf("selected team settings are not configured")
			}
			req.SettingsTeamID = team
		}
	}
	var codexConnection, claudeConnection *modelprovider.Connection
	owners := credentialOwnersForRequest(req)
	// Team-scoped API connections default to the team's settings. Legacy auth
	// file mounting keeps its existing credential_source behavior.
	if req.SettingsTeamID == "" && req.CredentialSource == "" && req.Scope == entities.ScopeTeam && req.TeamID != "" {
		owners = []string{req.TeamID}
	}
	for _, owner := range owners {
		if owner == "" || m.settingsRepo == nil {
			continue
		}
		exists, err := m.settingsRepo.Exists(ctx, owner)
		if err != nil {
			return fmt.Errorf("failed to read connection settings")
		}
		if exists {
			settings, err := m.settingsRepo.FindByName(ctx, owner)
			if err != nil {
				return fmt.Errorf("failed to read connection settings")
			}
			if codexConnection == nil {
				codexConnection = settings.CodexConnection()
			}
			if claudeConnection == nil {
				claudeConnection = settings.ClaudeConnection()
			}
		}
		files, _ := m.loadCredentialFiles(ctx, owner)
		if len(files) > 0 {
			break
		}
		if codexConnection != nil && claudeConnection != nil {
			break
		}
	}
	codexConnection = selectProfileConnection(codexConnection, profileConfig.CodexConnection(), req.CodexAuthMode)
	claudeConnection = selectProfileConnection(claudeConnection, profileConfig.ClaudeConnection(), req.ClaudeAuthMode)
	var err error
	codexConnection, err = modelprovider.SelectAuthMode(codexConnection, req.CodexAuthMode)
	if err != nil {
		return err
	}
	claudeConnection, err = modelprovider.SelectAuthMode(claudeConnection, req.ClaudeAuthMode)
	if err != nil {
		return err
	}
	for agent, c := range map[string]*modelprovider.Connection{"codex": codexConnection, "claude": claudeConnection} {
		if c == nil {
			continue
		}
		c.Model = modelprovider.ModelForLayers(agent, c.Model, req.ProfileEnvironment, req.Environment)
		if err := c.Validate(agent); err != nil {
			return fmt.Errorf("invalid %s connection: %w", agent, err)
		}
		// Built-in authentication modes may coexist with legacy environment
		// credentials. Only compatible API connections own these variables.
		if c.Compatible() {
			for _, layer := range []map[string]string{req.ProfileEnvironment, req.Environment} {
				for _, key := range modelprovider.ConnectionEnvKeys(agent) {
					if _, ok := layer[key]; ok {
						return fmt.Errorf("%s conflicts with managed %s connection; only model overrides are allowed", key, agent)
					}
				}
			}
		}
	}
	req.CodexConnection, req.ClaudeConnection = codexConnection, claudeConnection
	req.ModelConnectionsResolved = true
	return nil
}

// applySelectedAgentDefaultModel resolves the provider-specific default only
// after auto agent selection. An explicit session/profile model always wins.
func applySelectedAgentDefaultModel(req *entities.RunServerRequest) {
	if req == nil || req.Model != "" {
		return
	}
	agent := "claude"
	fallback := ""
	switch req.AgentType {
	case "codex-acp":
		agent = "codex"
		if req.CodexConnection != nil {
			fallback = req.CodexConnection.Model
		}
	case "", "claude-acp", "claude-legacy", "claude":
		if req.ClaudeConnection != nil {
			fallback = req.ClaudeConnection.Model
		}
	default:
		return
	}
	// Resolve the model for the selected agent again at the final selection
	// boundary. This keeps profile/request model overrides authoritative even
	// when the inherited team connection already carries its default model.
	req.Model = modelprovider.ModelForLayers(agent, fallback, req.ProfileEnvironment, req.Environment)
}

func applyModelConnections(settings *sessionsettings.SessionSettings, req *entities.RunServerRequest) {
	if req.SettingsTeamID != "" {
		// Remove process-level personal credentials before applying the selected
		// team's environment, including legacy OAuth/Bedrock configurations.
		settings.UnsetEnv = append(settings.UnsetEnv, modelprovider.ConnectionEnvKeys("codex")...)
		settings.UnsetEnv = append(settings.UnsetEnv, modelprovider.ConnectionEnvKeys("claude")...)
		settings.UnsetEnv = append(settings.UnsetEnv, "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_ROLE_ARN")
	}
	switch req.AgentType {
	case "codex-acp":
		settings.CodexConnection = req.CodexConnection.Clone()
		if req.Model != "" && settings.CodexConnection != nil && settings.CodexConnection.Compatible() {
			settings.CodexConnection.Model = req.Model
		}
	case "", "claude-acp", "claude-legacy", "claude":
		settings.ClaudeConnection = req.ClaudeConnection.Clone()
		if req.Model != "" && settings.ClaudeConnection != nil && settings.ClaudeConnection.Compatible() {
			settings.ClaudeConnection.Model = req.Model
		}
	}
	settings.ApplyModelConnections()
	if req.Model == "" {
		return
	}
	switch req.AgentType {
	case "codex-acp":
		// Compatible connections write their model while compiling the provider.
		if settings.CodexConnection == nil || !settings.CodexConnection.Compatible() {
			if merged, err := sessionsettings.MergeCodexModelConfig(settings.Codex.ConfigTOML, req.Model); err == nil {
				settings.Codex.ConfigTOML = merged
			}
		}
	case "", "claude-acp", "claude-legacy", "claude":
		settings.Env["ANTHROPIC_MODEL"] = req.Model
	case "pi-ollama":
		settings.Env["PI_OLLAMA_MODEL"] = req.Model
	}
}

func (m *KubernetesSessionManager) SetSessionProfileRepository(repo portrepos.SessionProfileRepository) {
	m.sessionProfileRepo = repo
}

// An explicit profile connection is a complete endpoint/credential pair. Never
// send an inherited settings API key to a profile-controlled endpoint.
func selectProfileConnection(base, override *modelprovider.Connection, mode string) *modelprovider.Connection {
	if override == nil || mode != override.Mode {
		return base
	}
	result := override.Clone()
	if result.Model == "" && base != nil {
		result.Model = base.Model
	}
	return result
}
