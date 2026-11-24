package controllers

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func (c *SessionProfileController) GetSessionProfile(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	profile, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSessionProfileNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "session profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get session profile")
	}

	if !c.canAccess(ctx, user, profile) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(profile))
}

// UpdateSessionProfile handles PUT /session-profiles/:id
func (c *SessionProfileController) UpdateSessionProfile(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	profile, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSessionProfileNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "session profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get session profile")
	}

	if !c.canModify(ctx, user, profile) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	var req UpdateSessionProfileRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if req.Name != nil {
		profile.SetName(*req.Name)
	}
	if req.Description != nil {
		profile.SetDescription(*req.Description)
	}
	if req.IsDefault != nil {
		profile.SetIsDefault(*req.IsDefault)
	}
	if req.SelectorTags != nil {
		profile.SetSelectorTags(req.SelectorTags)
	}
	if req.SelectorRepositories != nil {
		profile.SetSelectorRepositories(*req.SelectorRepositories)
	}
	if req.Config != nil {
		config := c.requestToConfig(*req.Config)
		if err := c.validateSourceSessionProfile(ctx.Request().Context(), profile, config); err != nil {
			return err
		}
		if err := validateProfileSettingsTeam(user, profile, config); err != nil {
			return err
		}
		if err := c.validateProfileSecrets(ctx.Request().Context(), profile, config); err != nil {
			return err
		}
		if err := applyProfileConnections(&config, profile.Config(), *req.Config); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if err := validateSessionProfileConfig(config); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		profile.SetConfig(config)
	}

	if err := c.repo.Update(ctx.Request().Context(), profile); err != nil {
		log.Printf("Failed to update session profile %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update session profile")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(profile))
}

func validateSessionProfileConfig(config entities.SessionProfileConfig) error {
	if err := sessionsettings.ValidateCommandWrapperTemplate(config.CommandWrapperTemplate()); err != nil {
		return err
	}
	if mode := config.SystemPromptMode(); mode != "" && mode != "override" && mode != "append" {
		return fmt.Errorf("system_prompt_mode must be override or append")
	}
	if params := config.Params(); params != nil {
		if err := modelprovider.ValidateAuthModes(params.CodexAuthMode, params.ClaudeAuthMode); err != nil {
			return err
		}
		if err := modelprovider.ValidateDefaultModels("codex", params.CodexDefaultModels); err != nil {
			return err
		}
		if err := modelprovider.ValidateDefaultModels("claude", params.ClaudeDefaultModels); err != nil {
			return err
		}
		if params.Model != "" {
			if err := modelprovider.ValidateModel(params.Model); err != nil {
				return err
			}
		}
		for _, model := range params.ModelOptions {
			if err := modelprovider.ValidateModel(model); err != nil {
				return err
			}
		}
	}
	if config.MCPServers() != nil {
		if err := config.MCPServers().Validate(); err != nil {
			return err
		}
	}
	if _, err := normalizeRepositoryPatterns(config.GitHubRepositories()); err != nil {
		return err
	}
	if err := validateSessionStateConfig(config.SessionState(), config.Params()); err != nil {
		return err
	}
	seenPaths := make(map[string]bool)
	for _, file := range config.ProfileFiles() {
		if file.Path == "" {
			return fmt.Errorf("profile file path is required")
		}
		if !filepath.IsAbs(file.Path) {
			return fmt.Errorf("profile file path must be absolute: %s", file.Path)
		}
		if seenPaths[file.Path] {
			return fmt.Errorf("duplicate profile file path: %s", file.Path)
		}
		seenPaths[file.Path] = true
	}
	seenRepositoryPaths := make(map[string]bool)
	for _, repository := range config.ProfileRepositories() {
		if repository.Repository == "" {
			return fmt.Errorf("profile repository is required")
		}
		parts := strings.Split(repository.Repository, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("profile repository must use owner/repository format: %s", repository.Repository)
		}
		if repository.Path == "" || !filepath.IsAbs(repository.Path) {
			return fmt.Errorf("profile repository path must be absolute: %s", repository.Path)
		}
		cleanPath := filepath.Clean(repository.Path)
		if cleanPath == "/" || cleanPath == "/home/agentapi/workdir/repo" {
			return fmt.Errorf("profile repository path is reserved: %s", repository.Path)
		}
		if seenRepositoryPaths[cleanPath] {
			return fmt.Errorf("duplicate profile repository path: %s", repository.Path)
		}
		seenRepositoryPaths[cleanPath] = true
	}
	return nil
}

func validateSessionStateConfig(value *entities.SessionStateConfig, params *entities.SessionParams) error {
	if value == nil || !value.Enabled {
		return nil
	}
	if value.StorageID == "" {
		return fmt.Errorf("session state storage_id is required")
	}
	components := make(map[string]struct{}, len(value.Components))
	for _, component := range value.Components {
		if component != "agent_state" && component != "workspace" {
			return fmt.Errorf("unsupported session state component: %s", component)
		}
		if _, exists := components[component]; exists {
			return fmt.Errorf("duplicate session state component: %s", component)
		}
		components[component] = struct{}{}
	}
	if len(components) == 0 {
		return fmt.Errorf("at least one session state component is required")
	}
	agents := make(map[string]struct{}, len(value.Agents))
	for _, agent := range value.Agents {
		if agent != "codex" && agent != "claude" {
			return fmt.Errorf("unsupported session state agent: %s", agent)
		}
		if _, exists := agents[agent]; exists {
			return fmt.Errorf("duplicate session state agent: %s", agent)
		}
		agents[agent] = struct{}{}
	}
	if _, enabled := components["agent_state"]; enabled && len(agents) == 0 {
		return fmt.Errorf("at least one session state agent is required")
	}
	if _, enabled := components["agent_state"]; enabled {
		if _, selected := agents["codex"]; selected {
			return fmt.Errorf("storage-backed session state is not supported for codex")
		}
	}
	if params != nil {
		fixedAgent := sessionStateAgent(params.AgentType)
		if _, enabled := components["agent_state"]; enabled && fixedAgent != "" {
			if _, selected := agents[fixedAgent]; !selected {
				return fmt.Errorf("session state agent %s must be selected for agent_type %s", fixedAgent, params.AgentType)
			}
		}
	}
	return nil
}

func sessionStateAgent(agentType string) string {
	switch agentType {
	case "codex", "codex-acp":
		return "codex"
	case "claude", "claude-acp", "claude-legacy":
		return "claude"
	default:
		return ""
	}
}

// DeleteSessionProfile handles DELETE /session-profiles/:id
func (c *SessionProfileController) DeleteSessionProfile(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	profile, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSessionProfileNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "session profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get session profile")
	}

	if !c.canModify(ctx, user, profile) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	if err := c.repo.Delete(ctx.Request().Context(), id); err != nil {
		log.Printf("Failed to delete session profile %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete session profile")
	}

	return ctx.NoContent(http.StatusNoContent)
}

// --- Helpers ---

func (c *SessionProfileController) canAccess(ctx echo.Context, user *entities.User, profile *entities.SessionProfile) bool {
	if user.IsAdmin() {
		return true
	}
	if profile.Scope() == entities.ScopeTeam {
		return user.IsMemberOfTeam(profile.TeamID())
	}
	return profile.UserID() == string(user.ID())
}

func (c *SessionProfileController) canModify(ctx echo.Context, user *entities.User, profile *entities.SessionProfile) bool {
	return c.canAccess(ctx, user, profile)
}

func (c *SessionProfileController) requestToConfig(req SessionProfileConfigRequest) entities.SessionProfileConfig {
	cfg := entities.NewSessionProfileConfig()
	if req.Environment != nil {
		cfg.SetEnvironment(req.Environment)
	}
	if req.Tags != nil {
		cfg.SetTags(req.Tags)
	}
	cfg.SetPool(req.Pool)
	cfg.SetSettingsTeamID(req.SettingsTeamID)
	cfg.SetInitialMessageTemplate(req.InitialMessageTemplate)
	cfg.SetReuseMessageTemplate(req.ReuseMessageTemplate)
	cfg.SetCommandWrapperTemplate(req.CommandWrapperTemplate)
	cfg.SetSystemPrompt(req.SystemPrompt)
	cfg.SetSystemPromptMode(req.SystemPromptMode)
	cfg.SetReuseSession(req.ReuseSession)
	if req.Params != nil {
		cfg.SetParams(req.Params)
	}
	cfg.SetSandboxPolicyID(req.SandboxPolicyID)
	cfg.SetSessionTTL(req.SessionTTL)
	cfg.SetUnsyncedFilePaths(req.UnsyncedFilePaths)
	cfg.SetSourceSessionProfileID(req.SourceSessionProfileID)
	cfg.SetSecretIDs(req.SecretIDs)
	if repositories, err := normalizeRepositoryPatterns(req.GitHubRepositories); err == nil {
		cfg.SetGitHubRepositories(repositories)
	} else {
		cfg.SetGitHubRepositories(req.GitHubRepositories)
	}
	cfg.SetSkills(req.Skills)
	cfg.SetPiPackages(req.PiPackages)
	cfg.SetSessionState(req.SessionState)
	if req.Files != nil {
		cfg.SetProfileFiles(req.Files)
	}
	if req.Repositories != nil {
		cfg.SetProfileRepositories(req.Repositories)
	}
	if req.MCPServers != nil {
		servers := entities.NewMCPServersSettings()
		for name, item := range req.MCPServers {
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
	return cfg
}

func (c *SessionProfileController) toResponse(p *entities.SessionProfile) SessionProfileResponse {
	cfg := p.Config()
	mcpServers := make(map[string]*MCPServerRequest)
	if cfg.MCPServers() != nil {
		for name, server := range cfg.MCPServers().Servers() {
			mcpServers[name] = &MCPServerRequest{Type: server.Type(), URL: server.URL(), Command: server.Command(), Args: server.Args(), Env: server.Env(), Headers: server.Headers()}
		}
	}
	return SessionProfileResponse{
		ID:                   p.ID(),
		Name:                 p.Name(),
		Description:          p.Description(),
		UserID:               p.UserID(),
		Scope:                p.Scope(),
		TeamID:               p.TeamID(),
		IsDefault:            p.IsDefault(),
		SelectorTags:         p.SelectorTags(),
		SelectorRepositories: p.SelectorRepositories(),
		Config: SessionProfileConfigResponse{
			CodexConnection:        cfg.CodexConnection(),
			ClaudeConnection:       cfg.ClaudeConnection(),
			PiConnection:           cfg.PiConnection(),
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
		CreatedAt: p.CreatedAt().Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt().Format(time.RFC3339),
	}
}
