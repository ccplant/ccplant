package controllers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type SandboxDomainsResponse struct {
	Allowed []string `json:"allowed"`
	Denied  []string `json:"denied"`
}

// GetSessionSandboxDomains handles GET /sessions/:sessionId/sandbox-domains.
// It forwards the request to the session's agent-provisioner /sandbox-domains endpoint,
// which in turn queries the network filter control server (127.0.0.1:3129/domains).
// Returns 404 when the session does not exist, 503 when the network filter is unavailable.
func (c *SessionController) GetSessionSandboxDomains(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")

	session := c.getSessionManager().GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
	}

	reader, ok := c.getSessionManager().(repositories.SessionSandboxDomainReader)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "Sandbox domains not available for this session type")
	}
	domains, err := reader.GetSessionSandboxDomains(ctx.Request().Context(), sessionID)
	if err != nil {
		log.Printf("[SESSION] Failed to fetch sandbox domains for %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Network filter not available")
	}
	return ctx.JSON(http.StatusOK, domains)
}

// resolveSessionProfile returns the session profile to apply for a session creation request.
// If profileID is set, it fetches that profile directly.
// Otherwise it searches for a selector_tags match before falling back to the settings default,
// then the legacy profile-level default flag.
func (c *SessionController) resolveSessionProfile(
	ctx context.Context,
	profileID,
	userID string,
	scope entities.ResourceScope,
	teamID string,
	tags map[string]string,
	authzCtx *auth.AuthorizationContext,
) (*entities.SessionProfile, string, error) {
	if profileID != "" {
		profile, err := c.sessionProfileRepo.Get(ctx, profileID)
		if err != nil {
			log.Printf("[SESSION] Warning: could not resolve session_profile_id %q: %v", profileID, err)
			if _, ok := err.(entities.ErrSessionProfileNotFound); ok {
				return nil, "", echo.NewHTTPError(http.StatusNotFound, err.Error())
			}
			return nil, "", echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve session profile")
		}
		if !c.canUseSessionProfile(authzCtx, profile, scope, teamID) {
			return nil, "", echo.NewHTTPError(
				http.StatusForbidden,
				entities.ErrSessionProfileAccessDenied{ID: profile.ID()}.Error(),
			)
		}
		if profile.Scope() == entities.ScopeTeam && scope == entities.ScopeUser {
			enabledProfiles, enabledErr := listEnabledTeamSessionProfiles(ctx, c.sessionProfileRepo, c.settingsRepo, userID, authzCtx.TeamScope.Teams)
			if enabledErr != nil {
				return nil, "", echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve enabled team profiles")
			}
			enabled := false
			for _, candidate := range enabledProfiles {
				enabled = enabled || candidate.ID() == profile.ID()
			}
			if !enabled {
				return nil, "", echo.NewHTTPError(http.StatusForbidden, entities.ErrSessionProfileAccessDenied{ID: profile.ID()}.Error())
			}
		}
		return profile, "explicit_profile_selected", nil
	}

	filter := repositories.SessionProfileFilter{
		UserID: userID,
		Scope:  scope,
	}
	if scope == entities.ScopeTeam {
		filter.TeamID = teamID
	}
	profiles, err := c.sessionProfileRepo.List(ctx, filter)
	if err != nil {
		log.Printf("[SESSION] Warning: could not list session profiles for default lookup: %v", err)
		return nil, "profile_list_unavailable", nil
	}
	if scope == entities.ScopeUser {
		var userTeamIDs []string
		if authzCtx != nil {
			userTeamIDs = authzCtx.TeamScope.Teams
		}
		teamProfiles, teamErr := listEnabledTeamSessionProfiles(ctx, c.sessionProfileRepo, c.settingsRepo, userID, userTeamIDs)
		if teamErr != nil {
			log.Printf("[SESSION] Warning: could not list enabled team profiles: %v", teamErr)
			return nil, "profile_list_unavailable", nil
		}
		profiles = append(profiles, teamProfiles...)
	}
	if profile := selectSessionProfileByTags(profiles, tags); profile != nil {
		log.Printf("[SESSION] Applying tag-selected session profile %q (%s) for user %s", profile.ID(), profile.Name(), userID)
		return profile, "most_specific_selector_tags_match", nil
	}
	if profile := c.resolveSettingsDefaultSessionProfile(ctx, userID, scope, teamID, profiles, authzCtx); profile != nil {
		log.Printf("[SESSION] Applying settings default session profile %q (%s) for user %s", profile.ID(), profile.Name(), userID)
		return profile, "settings_default_selected", nil
	}
	for _, p := range profiles {
		if p.IsDefault() {
			log.Printf("[SESSION] Applying default session profile %q (%s) for user %s", p.ID(), p.Name(), userID)
			return p, "legacy_default_selected", nil
		}
	}
	return nil, "no_matching_or_default_profile", nil
}

func (c *SessionController) canUseSessionProfile(
	authzCtx *auth.AuthorizationContext,
	profile *entities.SessionProfile,
	requestedScope entities.ResourceScope,
	requestedTeamID string,
) bool {
	if authzCtx == nil || profile == nil {
		return false
	}
	switch profile.Scope() {
	case entities.ScopeUser:
		return requestedScope == entities.ScopeUser && profile.UserID() == authzCtx.PersonalScope.UserID
	case entities.ScopeTeam:
		if requestedScope == entities.ScopeTeam {
			return profile.TeamID() == requestedTeamID && authzCtx.CanAccessTeam(profile.TeamID())
		}
		return requestedScope == entities.ScopeUser && authzCtx.CanAccessTeam(profile.TeamID())
	default:
		return false
	}
}

func sessionProfileResolutionHTTPError(err error) error {
	var notFound entities.ErrSessionProfileNotFound
	if errors.As(err, &notFound) {
		return echo.NewHTTPError(http.StatusNotFound, notFound.Error())
	}
	var invalid entities.ErrInvalidSessionProfile
	if errors.As(err, &invalid) {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, invalid.Error())
	}
	var accessDenied entities.ErrSessionProfileAccessDenied
	if errors.As(err, &accessDenied) {
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}
	return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve session profile")
}

func (c *SessionController) resolveSettingsDefaultSessionProfile(
	ctx context.Context,
	userID string,
	scope entities.ResourceScope,
	teamID string,
	profiles []*entities.SessionProfile,
	authzCtx *auth.AuthorizationContext,
) *entities.SessionProfile {
	if c.settingsRepo == nil {
		return nil
	}
	settingsName := userID
	if scope == entities.ScopeTeam && teamID != "" {
		settingsName = teamID
	}
	settings, err := c.settingsRepo.FindByName(ctx, settingsName)
	if err != nil || settings == nil || settings.DefaultSessionProfileID() == "" {
		return nil
	}
	defaultID := settings.DefaultSessionProfileID()
	for _, p := range profiles {
		if p.ID() == defaultID {
			return p
		}
	}
	profile, err := c.sessionProfileRepo.Get(ctx, defaultID)
	if err != nil {
		log.Printf("[SESSION] Warning: could not resolve default_session_profile_id %q from settings %q: %v", defaultID, settingsName, err)
		return nil
	}
	if !c.canUseSessionProfile(authzCtx, profile, scope, teamID) {
		log.Printf("[SESSION] Warning: default session profile %q is not accessible from settings %q", defaultID, settingsName)
		return nil
	}
	return profile
}

func selectSessionProfileByTags(profiles []*entities.SessionProfile, tags map[string]string) *entities.SessionProfile {
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

func applySessionProfile(
	startReq *entities.StartRequest,
	profile *entities.SessionProfile,
	cfg entities.SessionProfileConfig,
	explicitSandbox,
	explicitDocker bool,
) {
	// Resolve session profile: merge profile config into startReq fields.
	// When SessionProfileID is set, use that profile. Otherwise fall back to the
	// user/team's default profile. The profile is the base; explicit request fields override.
	if profile != nil {
		if startReq.Tags == nil {
			startReq.Tags = make(map[string]string)
		}
		startReq.Tags["session_profile_id"] = profile.ID()
		startReq.ResolvedSessionProfileID = profile.ID()
		startReq.ProfileMCPServers = cfg.MCPServers()
		startReq.ProfileSkills = cfg.Skills()
		startReq.ProfileGitHubRepositories = cfg.GitHubRepositories()
		for _, repository := range cfg.ProfileRepositories() {
			startReq.ProfileRepositories = append(startReq.ProfileRepositories, sessionsettings.RepositoryConfig{
				FullName: repository.Repository, CloneDir: repository.Path, Branch: repository.Branch,
			})
		}
		startReq.ProfileCommandWrapper = cfg.CommandWrapperTemplate()
		startReq.ProfileSystemPrompt = cfg.SystemPrompt()
		if profileFiles := cfg.ProfileFiles(); len(profileFiles) > 0 {
			startReq.ProfileFiles = make([]sessionsettings.ManagedFile, len(profileFiles))
			for i, file := range profileFiles {
				startReq.ProfileFiles[i] = sessionsettings.ManagedFile{
					Path:        file.Path,
					Content:     file.Content,
					Permissions: file.Permissions,
				}
			}
		}

		// Keep profile environment separate so it can override team/user
		// settings without overriding explicit request keys.
		if len(cfg.Environment()) > 0 {
			startReq.ProfileEnvironment = make(map[string]string, len(cfg.Environment()))
			for k, v := range cfg.Environment() {
				startReq.ProfileEnvironment[k] = v
			}
		}
		// Tags: profile is base, request keys override
		if len(cfg.Tags()) > 0 {
			merged := make(map[string]string, len(cfg.Tags()))
			for k, v := range cfg.Tags() {
				merged[k] = v
			}
			for k, v := range startReq.Tags {
				merged[k] = v
			}
			startReq.Tags = merged
		}

		// Params: profile is base, request fields override per-field
		if cfg.Params() != nil {
			if startReq.Params == nil {
				startReq.Params = cfg.Params()
			} else {
				startReq.Params = mergeSessionParams(cfg.Params(), startReq.Params)
			}
		}
		if cfg.Pool() != "" {
			if startReq.Params == nil {
				startReq.Params = &entities.SessionParams{}
			}
			if startReq.Params.Pool == "" {
				startReq.Params.Pool = cfg.Pool()
			}
		}
		if containsAllocatorSelector(startReq.Tags) || hasRequestedSessionPool(*startReq) {
			removeImplicitAllocatorCapabilities(startReq.Params, explicitSandbox, explicitDocker)
		}

		// SandboxPolicyID: apply profile's policy when request does not already specify one.
		if startReq.Params == nil {
			startReq.Params = &entities.SessionParams{}
		}
		// Native allocator sessions intentionally do not support sandboxing.
		// Do not let a profile's implicit sandbox default turn an otherwise valid
		// allocator.* request into an unsupported-capability request. An explicit
		// sandbox in the request remains intact and is rejected by the allocator
		// selection layer.
		if !containsAllocatorSelector(startReq.Tags) && !hasRequestedSessionPool(*startReq) {
			applyProfileSandboxDefaults(cfg, startReq.Params)
		}

		// SessionTTL: apply profile's TTL when request does not already specify one.
		if cfg.SessionTTL() != "" {
			if startReq.Params == nil {
				startReq.Params = &entities.SessionParams{}
			}
			if startReq.Params.SessionTTL == "" {
				startReq.Params.SessionTTL = cfg.SessionTTL()
			}
		}
		if len(cfg.UnsyncedFilePaths()) > 0 {
			if startReq.Params == nil {
				startReq.Params = &entities.SessionParams{}
			}
			if len(startReq.Params.UnsyncedFilePaths) == 0 {
				startReq.Params.UnsyncedFilePaths = cfg.UnsyncedFilePaths()
			}
		}
		applyProfileSessionState(startReq, cfg.SessionState())
	}
}

func applyProfileSessionState(startReq *entities.StartRequest, state *entities.SessionStateConfig) {
	if state == nil || !state.Enabled || state.StorageID == "" {
		return
	}
	if startReq.Params == nil {
		startReq.Params = &entities.SessionParams{}
	}
	componentEnabled := func(value string) bool {
		for _, component := range state.Components {
			if component == value {
				return true
			}
		}
		return false
	}
	agentEnabled := func(value string) bool {
		for _, agent := range state.Agents {
			if agent == value {
				return true
			}
		}
		return false
	}
	appendManaged := func(storagePath, mountPath string) {
		startReq.Params.Storages = append(startReq.Params.Storages, entities.StorageAttachment{
			StorageID: state.StorageID, StoragePath: storagePath, MountPath: mountPath, Managed: true,
		})
	}
	if componentEnabled("agent_state") {
		agent := sessionStateAgent(startReq.Params.AgentType)
		// Codex keeps its resume index in SQLite. File-by-file object storage
		// synchronization cannot preserve a consistent database/WAL generation,
		// so persisted legacy profiles must not create a managed Codex mount.
		if agent == "" || agent == "claude" {
			if agentEnabled("claude") {
				appendManaged("session-state/claude", "/home/agentapi/.claude")
			}
		}
	}
	if componentEnabled("workspace") {
		appendManaged("workspace", "/home/agentapi/workdir")
	}
}
