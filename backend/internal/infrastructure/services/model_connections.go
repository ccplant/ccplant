package services

import (
	"context"
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// Resolve once so auto selection, local allocation and remote provisioning agree.
func (m *KubernetesSessionManager) prepareModelConnections(ctx context.Context, req *entities.RunServerRequest) error {
	if req.ProvisionSettings != nil || req.ModelConnectionsResolved {
		return nil
	}
	if m.settingsRepo == nil {
		req.ModelConnectionsResolved = true
		return nil
	}
	var codexConnection, claudeConnection *modelprovider.Connection
	owners := credentialOwnersForRequest(req)
	// Team-scoped API connections default to the team's settings. Legacy auth
	// file mounting keeps its existing credential_source behavior.
	if req.CredentialSource == "" && req.Scope == entities.ScopeTeam && req.TeamID != "" {
		owners = []string{req.TeamID}
	}
	for _, owner := range owners {
		if owner == "" {
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
	for agent, c := range map[string]*modelprovider.Connection{"codex": codexConnection, "claude": claudeConnection} {
		if c == nil {
			continue
		}
		if err := c.Validate(agent); err != nil {
			return fmt.Errorf("invalid %s connection: %w", agent, err)
		}
		if c.Compatible() {
			c.Model = modelprovider.ModelForLayers(agent, c.Model, req.ProfileEnvironment, req.Environment)
			if err := modelprovider.ValidateModel(c.Model); err != nil {
				return err
			}
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
