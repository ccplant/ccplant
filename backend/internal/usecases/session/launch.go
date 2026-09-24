package session

import (
	"context"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	"log"
	"sort"
)

// LaunchRequest contains all parameters needed to create a session from any external
// trigger source (schedule, webhook, slackbot, etc.).
// Callers should use ResolveTeams() to populate the Teams field correctly.
type LaunchRequest struct {
	ResumeFrom string
	// Identity
	UserID string
	// TriggeredUserID is the external event actor (for example a GitHub login).
	TriggeredUserID string
	Scope           entities.ResourceScope
	TeamID          string
	// Teams is the list of GitHub team slugs for settings injection.
	// MUST be populated via ResolveTeams() — never leave empty for team-scoped sessions.
	Teams []string

	// Session configuration
	Environment              map[string]string
	ProfileEnvironment       map[string]string
	Tags                     map[string]string
	InitialMessage           string
	GithubToken              string
	AgentType                string
	Model                    string
	ModelOptions             []string
	Pool                     string
	ManagerID                string
	SlackParams              *entities.SlackParams
	RepoInfo                 *entities.RepositoryInfo
	InitialMessageWaitSecond *int
	Sandbox                  *entities.SandboxParams
	Docker                   *entities.DockerParams
	AuthProxy                *bool
	CycleMessage             string
	CycleMaxCount            int
	SessionTTL               string
	UnsyncedFilePaths        []string
	CodexAuthMode            string
	ClaudeAuthMode           string
	CredentialSource         string
	ProfileFiles             []sessionsettings.ManagedFile
	ProfileMCPServers        *entities.MCPServersSettings
	ResolvedSessionProfileID string

	// Webhook payload to mount in the session filesystem (optional)
	WebhookPayload []byte

	// Session reuse: when ReuseSession is true and ReuseMatchTags is non-empty,
	// an existing active session matching those tags is sent ReuseMessage instead
	// of creating a new session.
	ReuseSession   bool
	ReuseMatchTags map[string]string
	// ReuseMessage is sent to the reused session. Falls back to InitialMessage when empty.
	ReuseMessage string
	// StopBeforeReuse stops the running agent before sending ReuseMessage.
	// This keeps the session resources in place while making a busy agent receive follow-up input.
	StopBeforeReuse bool
	// DeferReuseToStart lets the authoritative /start API perform the tag lookup
	// and enqueue, which is required for direct runtimes not owned by this worker.
	DeferReuseToStart bool

	// Session limit: when MaxSessions > 0, launch fails if the number of sessions
	// matching LimitMatchTags already equals or exceeds MaxSessions.
	MaxSessions    int
	LimitMatchTags map[string]string

	// SessionProfileID is an optional reference to a SessionProfile.
	// When set, the profile's config is merged as a base; explicit request fields override it.
	SessionProfileID string
}

// LaunchResult is returned by LaunchUseCase.Launch.
type LaunchResult struct {
	SessionID     string
	SessionReused bool
	Session       entities.Session
}

// ResolveSessionTTL keeps the public oneshot parameter as backwards-compatible
// shorthand for a one-minute session TTL. Internal session creation only carries
// the resolved TTL.
func ResolveSessionTTL(params *entities.SessionParams) string {
	if params == nil {
		return ""
	}
	if params.SessionTTL == "" && params.Oneshot {
		return "1m"
	}
	return params.SessionTTL
}

// ResolveTeams returns the GitHub team slugs to inject into a session's settings.
//
// The rule mirrors what session_controller.go does for direct API requests:
//   - team-scoped: exactly the designated team's settings apply → [teamID]
//   - user-scoped: the user's full team membership list applies → userTeams
//
// Every trigger source (schedule, webhook, slackbot) must call this function when
// building a LaunchRequest so that team-level settings (Bedrock, MCP servers, etc.)
// are always injected correctly.
func ResolveTeams(scope entities.ResourceScope, teamID string, userTeams []string) []string {
	if scope == entities.ScopeTeam && teamID != "" {
		return []string{teamID}
	}
	return userTeams
}

// LaunchUseCase creates sessions from external triggers.
// It centralises the concerns shared by all trigger sources:
//   - session profile resolution
//   - session-reuse check
//   - session-limit enforcement
//   - RunServerRequest construction (Teams is always set via the caller's ResolveTeams call)
type LaunchUseCase struct {
	sessionManager     repositories.SessionManager
	sessionProfileRepo repositories.SessionProfileRepository // optional; if set, resolves profile configs
}

// NewLaunchUseCase creates a new LaunchUseCase.
func NewLaunchUseCase(sessionManager repositories.SessionManager) *LaunchUseCase {
	return &LaunchUseCase{sessionManager: sessionManager}
}

// WithSessionProfileRepository configures the session profile repository used to resolve
// profile configs when a SessionProfileID is present in the LaunchRequest.
// Calling this is optional; if not called, profile resolution is disabled.
func (uc *LaunchUseCase) WithSessionProfileRepository(repo repositories.SessionProfileRepository) *LaunchUseCase {
	uc.sessionProfileRepo = repo
	return uc
}

// Launch creates or reuses a session according to the LaunchRequest.
//
// Execution order:
//  0. Resolve session profile config (explicit ID, or default profile when ID is empty).
//  1. Try to reuse an existing active session (when ReuseSession is true).
//  2. Check the session limit (when MaxSessions > 0).
//  3. Create a new session.
func (uc *LaunchUseCase) Launch(ctx context.Context, sessionID string, req LaunchRequest) (LaunchResult, error) {
	return telemetry.Operation(ctx, "session.LaunchUseCase.Launch", func(ctx context.Context) (LaunchResult, error) {
		return uc.launch(ctx, sessionID, req)
	},
		telemetry.String("session.scope", string(req.Scope)),
		telemetry.Bool("session.reuse_requested", req.ReuseSession),
	)
}

func (uc *LaunchUseCase) launch(ctx context.Context, sessionID string, req LaunchRequest) (LaunchResult, error) {
	// 0. Resolve session profile: merge profile config as base; explicit request fields override.
	// When SessionProfileID is empty, fall back to the default profile for the user/team.
	if uc.sessionProfileRepo != nil {
		profile, err := uc.resolveSessionProfile(ctx, req)
		if err != nil {
			return LaunchResult{}, err
		}
		if profile != nil {
			cfg, err := ResolveEffectiveSessionProfileConfig(
				ctx, uc.sessionProfileRepo, profile, req.Scope, req.UserID, req.TeamID,
			)
			if err != nil {
				return LaunchResult{}, err
			}
			applyProfileToLaunchRequest(cfg, &req)
			req.ResolvedSessionProfileID = profile.ID()
			if req.Tags == nil {
				req.Tags = make(map[string]string)
			}
			req.Tags["session_profile_id"] = profile.ID()
		}
	}

	// 1. Try session reuse
	if req.ReuseSession && !req.DeferReuseToStart && len(req.ReuseMatchTags) > 0 {
		filter := entities.SessionFilter{
			Tags:   req.ReuseMatchTags,
			Status: "active",
		}
		if existing := uc.sessionManager.ListSessions(filter); len(existing) > 0 {
			reuseMessage := req.ReuseMessage
			if reuseMessage == "" {
				reuseMessage = req.InitialMessage
			}
			if req.StopBeforeReuse {
				if err := uc.sessionManager.StopAgent(ctx, existing[0].ID()); err != nil {
					return LaunchResult{}, fmt.Errorf("failed to stop existing session before reuse: %w", err)
				}
			}
			if err := uc.sessionManager.SendMessage(ctx, existing[0].ID(), reuseMessage); err != nil {
				return LaunchResult{}, fmt.Errorf("failed to route message to existing session: %w", err)
			}
			return LaunchResult{SessionID: existing[0].ID(), SessionReused: true, Session: existing[0]}, nil
		}
	}

	// 2. Check session limit
	if req.MaxSessions > 0 && !req.DeferReuseToStart {
		filter := entities.SessionFilter{Tags: req.LimitMatchTags}
		if existing := uc.sessionManager.ListSessions(filter); len(existing) >= req.MaxSessions {
			return LaunchResult{}, fmt.Errorf("session limit reached: maximum %d sessions", req.MaxSessions)
		}
	}

	// 3. Build RunServerRequest and create the session.
	// Teams is provided by the caller via ResolveTeams() so it is always set correctly.
	runReq := &entities.RunServerRequest{
		ResumeFrom:               req.ResumeFrom,
		UserID:                   req.UserID,
		TriggeredUserID:          req.TriggeredUserID,
		Environment:              req.Environment,
		ProfileEnvironment:       req.ProfileEnvironment,
		ProfileFiles:             req.ProfileFiles,
		Tags:                     req.Tags,
		Scope:                    req.Scope,
		TeamID:                   req.TeamID,
		Teams:                    req.Teams,
		InitialMessage:           req.InitialMessage,
		ReuseMatchTags:           req.ReuseMatchTags,
		ReuseMessage:             req.ReuseMessage,
		StopBeforeReuse:          req.StopBeforeReuse,
		LimitMatchTags:           req.LimitMatchTags,
		MaxSessions:              req.MaxSessions,
		GithubToken:              req.GithubToken,
		AgentType:                req.AgentType,
		Model:                    req.Model,
		ModelOptions:             req.ModelOptions,
		Pool:                     req.Pool,
		ManagerID:                req.ManagerID,
		SlackParams:              req.SlackParams,
		RepoInfo:                 req.RepoInfo,
		InitialMessageWaitSecond: req.InitialMessageWaitSecond,
		CycleMessage:             req.CycleMessage,
		CycleMaxCount:            req.CycleMaxCount,
		Sandbox:                  req.Sandbox,
		Docker:                   req.Docker,
		AuthProxy:                req.AuthProxy,
		SessionTTL:               req.SessionTTL,
		UnsyncedFilePaths:        req.UnsyncedFilePaths,
		CredentialSource:         req.CredentialSource,
		CodexAuthMode:            req.CodexAuthMode,
		ClaudeAuthMode:           req.ClaudeAuthMode,
		ProfileMCPServers:        req.ProfileMCPServers,
		ResolvedSessionProfileID: req.ResolvedSessionProfileID,
	}

	session, err := uc.sessionManager.CreateSession(ctx, sessionID, runReq, req.WebhookPayload)
	if err != nil {
		return LaunchResult{}, err
	}
	reused := false
	if aware, ok := session.(interface{ SessionReused() bool }); ok {
		reused = aware.SessionReused()
	}
	return LaunchResult{SessionID: session.ID(), SessionReused: reused, Session: session}, nil
}

// resolveSessionProfile returns the session profile to apply for the given request.
// If SessionProfileID is set, it fetches that profile directly.
// Otherwise it searches for a selector_tags match before falling back to the default profile.
func (uc *LaunchUseCase) resolveSessionProfile(ctx context.Context, req LaunchRequest) (*entities.SessionProfile, error) {
	if req.SessionProfileID != "" {
		profile, err := uc.sessionProfileRepo.Get(ctx, req.SessionProfileID)
		if err != nil {
			log.Printf("[LAUNCH] Warning: could not resolve session_profile_id %q: %v", req.SessionProfileID, err)
			return nil, err
		}
		if !profileMatchesLaunchTenant(profile, req) {
			return nil, entities.ErrSessionProfileAccessDenied{ID: profile.ID()}
		}
		return profile, nil
	}

	scope := req.Scope
	if scope == "" {
		scope = entities.ScopeUser
	}
	filter := repositories.SessionProfileFilter{
		UserID: req.UserID,
		Scope:  scope,
	}
	if scope == entities.ScopeTeam {
		filter.TeamID = req.TeamID
	}
	profiles, err := uc.sessionProfileRepo.List(ctx, filter)
	if err != nil {
		log.Printf("[LAUNCH] Warning: could not list session profiles for default lookup: %v", err)
		return nil, nil
	}
	if profile := selectProfileByTags(profiles, profileSelectionTags(req)); profile != nil {
		log.Printf("[LAUNCH] Applying tag-selected session profile %q (%s) for user %s", profile.ID(), profile.Name(), req.UserID)
		return profile, nil
	}
	for _, p := range profiles {
		if p.IsDefault() {
			log.Printf("[LAUNCH] Applying default session profile %q (%s) for user %s", p.ID(), p.Name(), req.UserID)
			return p, nil
		}
	}
	return nil, nil
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
	if !profileMatchesTenant(source, scope, userID, teamID) {
		return entities.SessionProfileConfig{}, entities.ErrSessionProfileAccessDenied{ID: source.ID()}
	}
	visited[source.ID()] = struct{}{}
	sourceCfg, err := resolveEffectiveSessionProfileConfig(ctx, repo, source, scope, userID, teamID, visited)
	if err != nil {
		return entities.SessionProfileConfig{}, err
	}
	return mergeProfileConfigSources(sourceCfg, cfg), nil
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
	return local
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
	if cfg.MCPServers() != nil {
		req.ProfileMCPServers = cfg.MCPServers()
	}
	// Keep profile environment separate so settings resolution can apply it above
	// team/user settings while still allowing explicit request values to win.
	if len(cfg.Environment()) > 0 {
		req.ProfileEnvironment = make(map[string]string, len(cfg.Environment()))
		for k, v := range cfg.Environment() {
			req.ProfileEnvironment[k] = v
		}
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
