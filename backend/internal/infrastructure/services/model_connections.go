package services

import (
	"context"
	"fmt"

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
	var codexConnection, claudeConnection *modelprovider.Connection
	owners := credentialOwnersForRequest(req)
	// Team-scoped API connections default to the team's settings. Legacy auth
	// file mounting keeps its existing credential_source behavior.
	if req.CredentialSource == "" && req.Scope == entities.ScopeTeam && req.TeamID != "" {
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
	if req.ResolvedSessionProfileID != "" {
		if m.sessionProfileRepo == nil {
			return fmt.Errorf("session profile repository unavailable")
		}
		profile, err := m.sessionProfileRepo.Get(ctx, req.ResolvedSessionProfileID)
		if err != nil {
			return fmt.Errorf("failed to load session profile connection")
		}
		cfg := profile.Config()
		codexConnection = selectProfileConnection(codexConnection, cfg.CodexConnection(), req.CodexAuthMode)
		claudeConnection = selectProfileConnection(claudeConnection, cfg.ClaudeConnection(), req.ClaudeAuthMode)
	}
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
		if c.Compatible() {
			c.Model = modelprovider.ModelForLayers(agent, c.Model, req.ProfileEnvironment, req.Environment)
		}
		if err := c.Validate(agent); err != nil {
			return fmt.Errorf("invalid %s connection: %w", agent, err)
		}
		for _, layer := range []map[string]string{req.ProfileEnvironment, req.Environment} {
			for _, key := range modelprovider.ConnectionEnvKeys(agent) {
				if _, ok := layer[key]; ok {
					return fmt.Errorf("%s conflicts with managed %s connection; only model overrides are allowed", key, agent)
				}
			}
		}
	}
	req.CodexConnection, req.ClaudeConnection = codexConnection, claudeConnection
	req.ModelConnectionsResolved = true
	return nil
}

func applyModelConnections(settings *sessionsettings.SessionSettings, req *entities.RunServerRequest) {
	switch req.AgentType {
	case "codex-acp":
		settings.CodexConnection = req.CodexConnection.Clone()
	case "", "claude-acp", "claude-legacy", "claude":
		settings.ClaudeConnection = req.ClaudeConnection.Clone()
	}
	settings.ApplyModelConnections()
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
