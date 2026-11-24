package session

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func profileInList(id string, profiles []*entities.SessionProfile) bool {
	for _, profile := range profiles {
		if profile.ID() == id {
			return true
		}
	}
	return false
}

func profileSelectionTags(req LaunchRequest) map[string]string {
	tags := make(map[string]string, len(req.Tags)+1)
	for key, value := range req.Tags {
		tags[key] = value
	}
	if req.RepoInfo != nil && req.RepoInfo.FullName != "" {
		tags["repository"] = req.RepoInfo.FullName
	}
	return tags
}

func profileMatchesLaunchTenant(profile *entities.SessionProfile, req LaunchRequest) bool {
	if profile != nil && profile.Scope() == entities.ScopeTeam && req.Scope != entities.ScopeTeam {
		for _, teamID := range req.Teams {
			if profile.TeamID() == teamID {
				return true
			}
		}
		return false
	}
	return profileMatchesTenant(profile, req.Scope, req.UserID, req.TeamID)
}

// ResolveEffectiveSessionProfileConfig resolves referenced profile sources and
// returns a config whose environment and MCP servers include those sources.
func ResolveEffectiveSessionProfileConfig(
	ctx context.Context,
	repo repositories.SessionProfileRepository,
	profile *entities.SessionProfile,
	scope entities.ResourceScope,
	userID string,
	teamID string,
) (entities.SessionProfileConfig, error) {
	if profile == nil {
		return entities.NewSessionProfileConfig(), nil
	}
	return resolveEffectiveSessionProfileConfig(ctx, repo, profile, scope, userID, teamID, map[string]struct{}{profile.ID(): {}})
}

func resolveEffectiveSessionProfileConfig(
	ctx context.Context,
	repo repositories.SessionProfileRepository,
	profile *entities.SessionProfile,
	scope entities.ResourceScope,
	userID string,
	teamID string,
	visited map[string]struct{},
) (entities.SessionProfileConfig, error) {
	cfg := profile.Config()
	sourceID := cfg.SourceSessionProfileID()
	if sourceID == "" {
		return cfg, nil
	}
	if _, cyclic := visited[sourceID]; cyclic {
		return entities.SessionProfileConfig{}, entities.ErrInvalidSessionProfile{
			Field:   "source_session_profile_id",
			Message: "cyclic reference",
		}
	}
	source, err := repo.Get(ctx, sourceID)
	if err != nil {
		return entities.SessionProfileConfig{}, err
	}
	// The selected profile has already been authorized by the caller. Inherited
	// profiles must stay within that profile's ownership tenant; this also lets a
	// team profile retain its team-local inheritance when used by a personal
	// session.
	if !profilesShareTenant(source, profile) {
		return entities.SessionProfileConfig{}, entities.ErrSessionProfileAccessDenied{ID: source.ID()}
	}
	visited[source.ID()] = struct{}{}
	sourceCfg, err := resolveEffectiveSessionProfileConfig(ctx, repo, source, scope, userID, teamID, visited)
	if err != nil {
		return entities.SessionProfileConfig{}, err
	}
	return mergeProfileConfigSources(sourceCfg, cfg), nil
}

func profilesShareTenant(left, right *entities.SessionProfile) bool {
	if left == nil || right == nil || left.Scope() != right.Scope() {
		return false
	}
	if left.Scope() == entities.ScopeTeam {
		return left.TeamID() != "" && left.TeamID() == right.TeamID()
	}
	return left.UserID() != "" && left.UserID() == right.UserID()
}

func profileMatchesTenant(profile *entities.SessionProfile, scope entities.ResourceScope, userID, teamID string) bool {
	if scope == "" {
		scope = entities.ScopeUser
	}
	switch profile.Scope() {
	case entities.ScopeUser:
		return scope == entities.ScopeUser && profile.UserID() == userID
	case entities.ScopeTeam:
		return scope == entities.ScopeTeam && profile.TeamID() == teamID
	default:
		return false
	}
}

func mergeProfileConfigSources(base, override entities.SessionProfileConfig) entities.SessionProfileConfig {
	local := override
	if local.CommandWrapperTemplate() == "" {
		local.SetCommandWrapperTemplate(base.CommandWrapperTemplate())
	}
	if local.SystemPrompt() == "" {
		local.SetSystemPrompt(base.SystemPrompt())
	} else if local.SystemPromptMode() == "append" && base.SystemPrompt() != "" {
		local.SetSystemPrompt(strings.TrimRight(base.SystemPrompt(), "\n") + "\n\n" + strings.TrimLeft(local.SystemPrompt(), "\n"))
	}
	if len(local.Environment()) == 0 {
		local.SetEnvironment(base.Environment())
	} else if len(base.Environment()) > 0 {
		freshConfig := entities.NewSessionProfileConfig()
		environment := freshConfig.Environment()
		for key, value := range base.Environment() {
			environment[key] = value
		}
		for key, value := range local.Environment() {
			environment[key] = value
		}
		local.SetEnvironment(environment)
	}
	if local.MCPServers() == nil || local.MCPServers().IsEmpty() {
		local.SetMCPServers(base.MCPServers().Clone())
	} else if base.MCPServers() != nil && !base.MCPServers().IsEmpty() {
		servers := base.MCPServers().Clone()
		for name, server := range local.MCPServers().Servers() {
			servers.SetServer(name, server)
		}
		local.SetMCPServers(servers)
	}
	if local.SessionState() == nil {
		local.SetSessionState(base.SessionState())
	}
	local.SetSkills(unionProfileStrings(base.Skills(), local.Skills()))
	local.SetPiPackages(unionProfileStrings(base.PiPackages(), local.PiPackages()))
	return local
}

func unionProfileStrings(base, override []string) []string {
	seen := make(map[string]struct{}, len(base)+len(override))
	result := make([]string, 0, len(base)+len(override))
	for _, values := range [][]string{base, override} {
		for _, value := range values {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func selectProfileByTags(profiles []*entities.SessionProfile, tags map[string]string) *entities.SessionProfile {
	var matches []*entities.SessionProfile
	for _, p := range profiles {
		if p.MatchesSelectorTags(tags) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].SelectorSpecificity() != matches[j].SelectorSpecificity() {
			return matches[i].SelectorSpecificity() > matches[j].SelectorSpecificity()
		}
		if matches[i].IsDefault() != matches[j].IsDefault() {
			return matches[i].IsDefault()
		}
		if matches[i].Name() != matches[j].Name() {
			return matches[i].Name() < matches[j].Name()
		}
		return matches[i].ID() < matches[j].ID()
	})
	return matches[0]
}

// applyProfileToLaunchRequest merges a SessionProfileConfig into a LaunchRequest.
// The profile provides the base; explicit request fields override.
func applyProfileToLaunchRequest(cfg entities.SessionProfileConfig, req *LaunchRequest) {
	req.ProfileCommandWrapper = cfg.CommandWrapperTemplate()
	req.ProfileSystemPrompt = cfg.SystemPrompt()
	if cfg.SettingsTeamID() != "" {
		req.SettingsTeamID = cfg.SettingsTeamID()
	}
	if cfg.MCPServers() != nil {
		req.ProfileMCPServers = cfg.MCPServers()
	}
	req.ProfileSkills = cfg.Skills()
	req.ProfileGitHubRepositories = cfg.GitHubRepositories()
	for _, repository := range cfg.ProfileRepositories() {
		req.ProfileRepositories = append(req.ProfileRepositories, sessionsettings.RepositoryConfig{
			FullName: repository.Repository, CloneDir: repository.Path, Branch: repository.Branch,
		})
		req.ProfileGitHubRepositories = append(req.ProfileGitHubRepositories, "^"+regexp.QuoteMeta(repository.Repository)+"$")
	}
	// Keep profile environment separate so settings resolution can apply it above
	// team/user settings while still allowing explicit request values to win.
	if len(cfg.Environment()) > 0 {
		req.ProfileEnvironment = make(map[string]string, len(cfg.Environment()))
		for k, v := range cfg.Environment() {
			req.ProfileEnvironment[k] = v
		}
	}
	if packages := cfg.PiPackages(); len(packages) > 0 {
		if req.ProfileEnvironment == nil {
			req.ProfileEnvironment = make(map[string]string)
		}
		req.ProfileEnvironment["CCPLANT_PI_PACKAGES"] = strings.Join(packages, "\n")
	}
	// Tags: profile is base, request overrides key-by-key
	if len(cfg.Tags()) > 0 {
		merged := make(map[string]string, len(cfg.Tags()))
		for k, v := range cfg.Tags() {
			merged[k] = v
		}
		for k, v := range req.Tags {
			merged[k] = v
		}
		req.Tags = merged
	}
	if req.Pool == "" {
		req.Pool = cfg.Pool()
	}
	// Params: profile fills in empty request fields
	if cfg.Params() != nil {
		if req.AgentType == "" {
			req.AgentType = cfg.Params().AgentType
		}
		if req.Model == "" {
			req.Model = cfg.Params().Model
		}
		if len(req.ModelOptions) == 0 && len(cfg.Params().ModelOptions) > 0 {
			req.ModelOptions = append([]string(nil), cfg.Params().ModelOptions...)
		}
		if req.GithubToken == "" {
			req.GithubToken = cfg.Params().GithubToken
		}
		if req.InitialMessage == "" && cfg.Params().Message != "" {
			req.InitialMessage = cfg.Params().Message
		}
		if req.Sandbox == nil && cfg.Params().Sandbox != nil {
			req.Sandbox = cfg.Params().Sandbox
		}
		if req.Sandbox != nil && req.Sandbox.PolicyID == "" && cfg.SandboxPolicyID() != "" {
			req.Sandbox.Enabled = true
			req.Sandbox.PolicyID = cfg.SandboxPolicyID()
		}
		if req.Docker == nil && cfg.Params().Docker != nil {
			req.Docker = cfg.Params().Docker
		}
		if req.AuthProxy == nil && cfg.Params().AuthProxy != nil {
			req.AuthProxy = cfg.Params().AuthProxy
		}
		if req.InitialMessageWaitSecond == nil && cfg.Params().InitialMessageWaitSecond != nil {
			req.InitialMessageWaitSecond = cfg.Params().InitialMessageWaitSecond
		}
		if req.CycleMessage == "" {
			req.CycleMessage = cfg.Params().CycleMessage
		}
		if req.CycleMaxCount == 0 {
			req.CycleMaxCount = cfg.Params().CycleMaxCount
		}
		if req.SessionTTL == "" {
			req.SessionTTL = ResolveSessionTTL(cfg.Params())
		}
		if len(req.UnsyncedFilePaths) == 0 && len(cfg.Params().UnsyncedFilePaths) > 0 {
			req.UnsyncedFilePaths = append([]string(nil), cfg.Params().UnsyncedFilePaths...)
		}
		if req.CodexAuthMode == "" {
			req.CodexAuthMode = cfg.Params().CodexAuthMode
		}
		if req.ClaudeAuthMode == "" {
			req.ClaudeAuthMode = cfg.Params().ClaudeAuthMode
		}
		if req.CredentialSource == "" {
			req.CredentialSource = cfg.Params().CredentialSource
		}
	}
	if cfg.SessionTTL() != "" && req.SessionTTL == "" {
		req.SessionTTL = cfg.SessionTTL()
	}
	if len(cfg.UnsyncedFilePaths()) > 0 && len(req.UnsyncedFilePaths) == 0 {
		req.UnsyncedFilePaths = cfg.UnsyncedFilePaths()
	}
	profileFiles := cfg.ProfileFiles()
	if len(profileFiles) > 0 {
		req.ProfileFiles = make([]sessionsettings.ManagedFile, len(profileFiles))
		for i, file := range profileFiles {
			req.ProfileFiles[i] = sessionsettings.ManagedFile{
				Path:        file.Path,
				Content:     file.Content,
				Permissions: file.Permissions,
			}
		}
	}
	if len(cfg.SecretIDs()) > 0 {
		req.ProfileSecretIDs = cfg.SecretIDs()
	}
	applyProfileSandboxDefaults(cfg, req)
}

func applyProfileSandboxDefaults(cfg entities.SessionProfileConfig, req *LaunchRequest) {
	if cfg.SandboxPolicyID() != "" {
		if req.Sandbox == nil {
			req.Sandbox = &entities.SandboxParams{Enabled: true, PolicyID: cfg.SandboxPolicyID()}
		} else if req.Sandbox.PolicyID == "" {
			req.Sandbox.Enabled = true
			req.Sandbox.PolicyID = cfg.SandboxPolicyID()
		}
		return
	}
	if req.Sandbox == nil {
		req.Sandbox = &entities.SandboxParams{Enabled: true, CountMode: true}
	} else if req.Sandbox.PolicyID == "" {
		req.Sandbox.Enabled = true
		req.Sandbox.CountMode = true
	}
}
