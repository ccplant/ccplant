package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func (r *KubernetesSessionProfileRepository) jsonToEntity(pj *sessionProfileJSON) *entities.SessionProfile {
	profile := entities.NewSessionProfile(pj.ID, pj.Name, pj.UserID)
	profile.SetDescription(pj.Description)
	profile.SetScope(pj.Scope)
	profile.SetTeamID(pj.TeamID)
	profile.SetIsDefault(pj.IsDefault)
	profile.SetSelectorTags(pj.SelectorTags)
	profile.SetSelectorRepositories(pj.SelectorRepositories)
	profile.SetCreatedAt(pj.CreatedAt)
	profile.SetUpdatedAt(pj.UpdatedAt)

	cfg := entities.NewSessionProfileConfig()
	cfg.SetEnvironment(pj.Config.Environment)
	cfg.SetTags(pj.Config.Tags)
	cfg.SetPool(pj.Config.Pool)
	cfg.SetSettingsTeamID(pj.Config.SettingsTeamID)
	cfg.SetInitialMessageTemplate(pj.Config.InitialMessageTemplate)
	cfg.SetReuseMessageTemplate(pj.Config.ReuseMessageTemplate)
	cfg.SetCommandWrapperTemplate(pj.Config.CommandWrapperTemplate)
	cfg.SetSystemPrompt(pj.Config.SystemPrompt)
	cfg.SetSystemPromptMode(pj.Config.SystemPromptMode)
	cfg.SetParams(pj.Config.Params)
	cfg.SetReuseSession(pj.Config.ReuseSession)
	cfg.SetSandboxPolicyID(pj.Config.SandboxPolicyID)
	cfg.SetSessionTTL(pj.Config.SessionTTL)
	cfg.SetUnsyncedFilePaths(pj.Config.UnsyncedFilePaths)
	cfg.SetSourceSessionProfileID(pj.Config.SourceSessionProfileID)
	cfg.SetSecretIDs(pj.Config.SecretIDs)
	cfg.SetGitHubRepositories(pj.Config.GitHubRepositories)
	cfg.SetSkills(pj.Config.Skills)
	cfg.SetPiPackages(pj.Config.PiPackages)
	cfg.SetSessionState(pj.Config.SessionState)
	cfg.SetProfileFiles(pj.Config.Files)
	cfg.SetProfileRepositories(pj.Config.Repositories)
	if pj.Config.MCPServers != nil {
		servers := entities.NewMCPServersSettings()
		for name, item := range pj.Config.MCPServers {
			if item == nil {
				continue
			}
			server := entities.NewMCPServer(name, item.Type)
			server.SetURL(item.URL)
			server.SetCommand(item.Command)
			server.SetArgs(item.Args)
			server.SetEnv(item.Env)
			server.SetHeaders(item.Headers)
			servers.SetServer(name, server)
		}
		cfg.SetMCPServers(servers)
	}
	profile.SetConfig(cfg)

	return profile
}

func (r *KubernetesSessionProfileRepository) entityToJSON(profile *entities.SessionProfile) *sessionProfileJSON {
	cfg := profile.Config()
	mcpServers := make(map[string]*mcpServerJSON)
	if cfg.MCPServers() != nil {
		for name, server := range cfg.MCPServers().Servers() {
			mcpServers[name] = &mcpServerJSON{Type: server.Type(), URL: server.URL(), Command: server.Command(), Args: server.Args(), Env: server.Env(), Headers: server.Headers()}
		}
	}
	return &sessionProfileJSON{
		ID:                   profile.ID(),
		Name:                 profile.Name(),
		Description:          profile.Description(),
		UserID:               profile.UserID(),
		Scope:                profile.Scope(),
		TeamID:               profile.TeamID(),
		IsDefault:            profile.IsDefault(),
		SelectorTags:         profile.SelectorTags(),
		SelectorRepositories: profile.SelectorRepositories(),
		Config: sessionProfileConfigJSON{
			Environment:            cfg.Environment(),
			Tags:                   cfg.Tags(),
			Pool:                   cfg.Pool(),
			SettingsTeamID:         cfg.SettingsTeamID(),
			InitialMessageTemplate: cfg.InitialMessageTemplate(),
			ReuseMessageTemplate:   cfg.ReuseMessageTemplate(),
			CommandWrapperTemplate: cfg.CommandWrapperTemplate(),
			SystemPrompt:           cfg.SystemPrompt(),
			SystemPromptMode:       cfg.SystemPromptMode(),
			Params:                 cfg.Params(),
			ReuseSession:           cfg.ReuseSession(),
			SandboxPolicyID:        cfg.SandboxPolicyID(),
			SessionTTL:             cfg.SessionTTL(),
			UnsyncedFilePaths:      cfg.UnsyncedFilePaths(),
			SourceSessionProfileID: cfg.SourceSessionProfileID(),
			SecretIDs:              cfg.SecretIDs(),
			GitHubRepositories:     cfg.GitHubRepositories(),
			Skills:                 cfg.Skills(),
			PiPackages:             cfg.PiPackages(),
			Files:                  cfg.ProfileFiles(),
			Repositories:           cfg.ProfileRepositories(),
			MCPServers:             mcpServers,
			SessionState:           cfg.SessionState(),
		},
		CreatedAt: profile.CreatedAt(),
		UpdatedAt: profile.UpdatedAt(),
	}
}

func (r *KubernetesSessionProfileRepository) decodeProfile(ctx context.Context, pj *sessionProfileJSON) (*entities.SessionProfile, error) {
	codex, err := r.connections.decodeConnection(ctx, pj.Config.CodexConnection)
	if err != nil {
		return nil, err
	}
	claude, err := r.connections.decodeConnection(ctx, pj.Config.ClaudeConnection)
	if err != nil {
		return nil, err
	}
	pi, err := r.connections.decodeConnection(ctx, pj.Config.PiConnection)
	if err != nil {
		return nil, err
	}
	p := r.jsonToEntity(pj)
	cfg := p.Config()
	cfg.SetCodexConnection(codex)
	cfg.SetClaudeConnection(claude)
	cfg.SetPiConnection(pi)
	p.SetConfig(cfg)
	p.SetUpdatedAt(pj.UpdatedAt)
	return p, nil
}
