package controllers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

// SessionProfileController handles session profile CRUD endpoints
type SessionProfileController struct {
	repo         repositories.SessionProfileRepository
	settingsRepo repositories.SettingsRepository
}

// NewSessionProfileController creates a new SessionProfileController
func NewSessionProfileController(repo repositories.SessionProfileRepository, settingsRepo repositories.SettingsRepository) *SessionProfileController {
	return &SessionProfileController{repo: repo, settingsRepo: settingsRepo}
}

// GetName returns the name of this controller for logging
func (c *SessionProfileController) GetName() string {
	return "SessionProfileController"
}

// --- Request / Response types ---

// SessionProfileConfigRequest represents session profile config in requests
type SessionProfileConfigRequest struct {
	SettingsTeamID         string                       `json:"settings_team_id,omitempty"`
	CodexConnection        json.RawMessage              `json:"codex_connection,omitempty"`
	ClaudeConnection       json.RawMessage              `json:"claude_connection,omitempty"`
	PiConnection           json.RawMessage              `json:"pi_connection,omitempty"`
	Environment            map[string]string            `json:"environment,omitempty"`
	Tags                   map[string]string            `json:"tags,omitempty"`
	Pool                   string                       `json:"pool,omitempty"`
	InitialMessageTemplate string                       `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                       `json:"reuse_message_template,omitempty"`
	CommandWrapperTemplate string                       `json:"command_wrapper_template,omitempty"`
	SystemPrompt           string                       `json:"system_prompt,omitempty"`
	SystemPromptMode       string                       `json:"system_prompt_mode,omitempty"`
	Params                 *entities.SessionParams      `json:"params,omitempty"`
	ReuseSession           bool                         `json:"reuse_session,omitempty"`
	SandboxPolicyID        string                       `json:"sandbox_policy_id,omitempty"`
	SessionTTL             string                       `json:"session_ttl,omitempty"`
	UnsyncedFilePaths      []string                     `json:"unsynced_file_paths,omitempty"`
	SourceSessionProfileID string                       `json:"source_session_profile_id,omitempty"`
	SecretIDs              []string                     `json:"secret_ids,omitempty"`
	GitHubRepositories     []string                     `json:"github_repositories,omitempty"`
	Skills                 []string                     `json:"skills,omitempty"`
	PiPackages             []string                     `json:"pi_packages,omitempty"`
	Files                  []entities.ProfileFile       `json:"files,omitempty"`
	Repositories           []entities.ProfileRepository `json:"repositories,omitempty"`
	MCPServers             map[string]*MCPServerRequest `json:"mcp_servers,omitempty"`
	SessionState           *entities.SessionStateConfig `json:"session_state,omitempty"`
}

// CreateSessionProfileRequest is the request body for creating a session profile
type CreateSessionProfileRequest struct {
	Name                 string                      `json:"name"`
	Description          string                      `json:"description,omitempty"`
	Scope                entities.ResourceScope      `json:"scope,omitempty"`
	TeamID               string                      `json:"team_id,omitempty"`
	IsDefault            bool                        `json:"is_default,omitempty"`
	SelectorTags         map[string]string           `json:"selector_tags,omitempty"`
	SelectorRepositories []string                    `json:"selector_repositories,omitempty"`
	Config               SessionProfileConfigRequest `json:"config"`
}

// UpdateSessionProfileRequest is the request body for updating a session profile
type UpdateSessionProfileRequest struct {
	Name                 *string                      `json:"name,omitempty"`
	Description          *string                      `json:"description,omitempty"`
	IsDefault            *bool                        `json:"is_default,omitempty"`
	SelectorTags         map[string]string            `json:"selector_tags,omitempty"`
	SelectorRepositories *[]string                    `json:"selector_repositories,omitempty"`
	Config               *SessionProfileConfigRequest `json:"config,omitempty"`
}

// SessionProfileResponse is the response for a session profile
type SessionProfileResponse struct {
	ID                   string                       `json:"id"`
	Name                 string                       `json:"name"`
	Description          string                       `json:"description,omitempty"`
	UserID               string                       `json:"user_id"`
	Scope                entities.ResourceScope       `json:"scope,omitempty"`
	TeamID               string                       `json:"team_id,omitempty"`
	IsDefault            bool                         `json:"is_default,omitempty"`
	SelectorTags         map[string]string            `json:"selector_tags,omitempty"`
	SelectorRepositories []string                     `json:"selector_repositories,omitempty"`
	Config               SessionProfileConfigResponse `json:"config"`
	CreatedAt            string                       `json:"created_at"`
	UpdatedAt            string                       `json:"updated_at"`
	PersonalEnabled      bool                         `json:"personal_enabled,omitempty"`
}

// SessionProfileConfigResponse represents session profile config in responses
type SessionProfileConfigResponse struct {
	SettingsTeamID         string                       `json:"settings_team_id,omitempty"`
	CodexConnection        *modelprovider.Connection    `json:"codex_connection,omitempty"`
	ClaudeConnection       *modelprovider.Connection    `json:"claude_connection,omitempty"`
	PiConnection           *modelprovider.Connection    `json:"pi_connection,omitempty"`
	Environment            map[string]string            `json:"environment,omitempty"`
	Tags                   map[string]string            `json:"tags,omitempty"`
	Pool                   string                       `json:"pool,omitempty"`
	InitialMessageTemplate string                       `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                       `json:"reuse_message_template,omitempty"`
	CommandWrapperTemplate string                       `json:"command_wrapper_template,omitempty"`
	SystemPrompt           string                       `json:"system_prompt,omitempty"`
	SystemPromptMode       string                       `json:"system_prompt_mode,omitempty"`
	Params                 *entities.SessionParams      `json:"params,omitempty"`
	ReuseSession           bool                         `json:"reuse_session,omitempty"`
	SandboxPolicyID        string                       `json:"sandbox_policy_id,omitempty"`
	SessionTTL             string                       `json:"session_ttl,omitempty"`
	UnsyncedFilePaths      []string                     `json:"unsynced_file_paths,omitempty"`
	SourceSessionProfileID string                       `json:"source_session_profile_id,omitempty"`
	SecretIDs              []string                     `json:"secret_ids,omitempty"`
	GitHubRepositories     []string                     `json:"github_repositories,omitempty"`
	Skills                 []string                     `json:"skills,omitempty"`
	PiPackages             []string                     `json:"pi_packages,omitempty"`
	Files                  []entities.ProfileFile       `json:"files,omitempty"`
	Repositories           []entities.ProfileRepository `json:"repositories,omitempty"`
	MCPServers             map[string]*MCPServerRequest `json:"mcp_servers,omitempty"`
	SessionState           *entities.SessionStateConfig `json:"session_state,omitempty"`
}

// PreviewSessionProfileRequest is the request body for POST /session-profiles/preview
type PreviewSessionProfileRequest struct {
	Tags   map[string]string      `json:"tags,omitempty"`
	Scope  entities.ResourceScope `json:"scope,omitempty"`
	TeamID string                 `json:"team_id,omitempty"`
}

// --- Handlers ---

// CreateSessionProfile handles POST /session-profiles
func (c *SessionProfileController) CreateSessionProfile(ctx echo.Context) error {
	var req CreateSessionProfileRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}

	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	userID := string(user.ID())

	// Resolve scope
	resolvedScope, resolvedTeamID := auth.ResolveUserScope(user, string(req.Scope), req.TeamID)
	req.Scope = entities.ResourceScope(resolvedScope)
	req.TeamID = resolvedTeamID

	if req.Scope == entities.ScopeTeam && req.TeamID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
	}

	if req.Scope == entities.ScopeTeam {
		if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx == nil || !authzCtx.CanCreateInTeam(req.TeamID) {
			return echo.NewHTTPError(http.StatusForbidden, "you are not a member of this team")
		}
	}

	profile := entities.NewSessionProfile(uuid.New().String(), req.Name, userID)
	profile.SetDescription(req.Description)
	profile.SetScope(req.Scope)
	profile.SetTeamID(req.TeamID)
	profile.SetIsDefault(req.IsDefault)
	profile.SetSelectorTags(req.SelectorTags)
	profile.SetSelectorRepositories(req.SelectorRepositories)
	config := c.requestToConfig(req.Config)
	if err := c.validateSourceSessionProfile(ctx.Request().Context(), profile, config); err != nil {
		return err
	}
	if err := validateProfileSettingsTeam(user, profile, config); err != nil {
		return err
	}
	if err := c.validateProfileSecrets(ctx.Request().Context(), profile, config); err != nil {
		return err
	}
	if err := applyProfileConnections(&config, entities.NewSessionProfileConfig(), req.Config); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := validateSessionProfileConfig(config); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	profile.SetConfig(config)

	if err := c.repo.Create(ctx.Request().Context(), profile); err != nil {
		log.Printf("Failed to create session profile: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create session profile")
	}

	return ctx.JSON(http.StatusCreated, c.toResponse(profile))
}

// ListSessionProfiles handles GET /session-profiles
func (c *SessionProfileController) ListSessionProfiles(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	userID := string(user.ID())

	scopeFilter := ctx.QueryParam("scope")
	teamIDFilter := ctx.QueryParam("team_id")
	scopeFilter, teamIDFilter = auth.ResolveUserScope(user, scopeFilter, teamIDFilter)

	var userTeamIDs []string
	if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx != nil {
		userTeamIDs = authzCtx.TeamScope.Teams
	}

	filter := repositories.SessionProfileFilter{
		Scope:   entities.ResourceScope(scopeFilter),
		TeamID:  teamIDFilter,
		TeamIDs: userTeamIDs,
	}
	if scopeFilter != "team" && teamIDFilter == "" {
		filter.UserID = userID
		filter.Scope = entities.ScopeUser
	}

	profiles, err := c.repo.List(ctx.Request().Context(), filter)
	if err != nil {
		log.Printf("Failed to list session profiles: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list session profiles")
	}
	// Personal sessions may explicitly use profiles owned by any team the user
	// belongs to. Keep the personal query separate so repository filters remain
	// simple AND filters, then append the accessible team profiles.
	enabledTeamProfileIDs := map[string]struct{}{}
	includeAvailableTeamProfiles := ctx.QueryParam("include_available_team_profiles") == "true"
	if scopeFilter != "team" && teamIDFilter == "" && len(userTeamIDs) > 0 {
		if c.settingsRepo != nil {
			if settings, settingsErr := c.settingsRepo.FindByName(ctx.Request().Context(), userID); settingsErr == nil && settings != nil {
				for _, id := range settings.EnabledTeamSessionProfileIDs() {
					enabledTeamProfileIDs[id] = struct{}{}
				}
			}
		}
		teamProfiles, listErr := c.repo.List(ctx.Request().Context(), repositories.SessionProfileFilter{
			Scope:   entities.ScopeTeam,
			TeamIDs: userTeamIDs,
		})
		if listErr != nil {
			log.Printf("Failed to list team session profiles: %v", listErr)
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to list session profiles")
		}
		for _, profile := range teamProfiles {
			_, enabled := enabledTeamProfileIDs[profile.ID()]
			if includeAvailableTeamProfiles || enabled {
				profiles = append(profiles, profile)
			}
		}
	}

	responses := make([]SessionProfileResponse, 0, len(profiles))
	for _, p := range profiles {
		response := c.toResponse(p)
		if p.Scope() == entities.ScopeTeam {
			_, response.PersonalEnabled = enabledTeamProfileIDs[p.ID()]
		}
		responses = append(responses, response)
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"session_profiles": responses,
	})
}

func listEnabledTeamSessionProfiles(
	ctx context.Context,
	profileRepo repositories.SessionProfileRepository,
	settingsRepo repositories.SettingsRepository,
	userID string,
	teamIDs []string,
) ([]*entities.SessionProfile, error) {
	if profileRepo == nil || settingsRepo == nil || userID == "" || len(teamIDs) == 0 {
		return nil, nil
	}
	exists, err := settingsRepo.Exists(ctx, userID)
	if err != nil || !exists {
		return nil, err
	}
	settings, err := settingsRepo.FindByName(ctx, userID)
	if err != nil || settings == nil {
		return nil, err
	}
	enabled := make(map[string]struct{})
	for _, id := range settings.EnabledTeamSessionProfileIDs() {
		enabled[id] = struct{}{}
	}
	if len(enabled) == 0 {
		return nil, nil
	}
	profiles, err := profileRepo.List(ctx, repositories.SessionProfileFilter{Scope: entities.ScopeTeam, TeamIDs: teamIDs})
	if err != nil {
		return nil, err
	}
	result := make([]*entities.SessionProfile, 0, len(profiles))
	for _, profile := range profiles {
		if _, ok := enabled[profile.ID()]; ok {
			result = append(result, profile)
		}
	}
	return result, nil
}

// PreviewSessionProfile handles POST /session-profiles/preview.
// It resolves the profile that /start would apply for the given tags without
// creating a session: selector_tags match, then settings default, then the
// profile-level default flag.
func (c *SessionProfileController) PreviewSessionProfile(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	userID := string(user.ID())

	var req PreviewSessionProfileRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	resolvedScope, resolvedTeamID := auth.ResolveUserScope(user, string(req.Scope), req.TeamID)
	scope := entities.ResourceScope(resolvedScope)
	if scope == entities.ScopeTeam {
		authzCtx := auth.GetAuthorizationContext(ctx)
		if resolvedTeamID == "" || authzCtx == nil || !authzCtx.CanAccessTeam(resolvedTeamID) {
			return echo.NewHTTPError(http.StatusForbidden, "team access denied")
		}
	}

	var userTeamIDs []string
	if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx != nil {
		userTeamIDs = authzCtx.TeamScope.Teams
	}

	filter := repositories.SessionProfileFilter{
		Scope:   scope,
		TeamID:  resolvedTeamID,
		TeamIDs: userTeamIDs,
	}
	if scope != entities.ScopeTeam || resolvedTeamID == "" {
		filter.UserID = userID
	}

	profiles, err := c.repo.List(ctx.Request().Context(), filter)
	if err != nil {
		log.Printf("Failed to list session profiles for preview: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list session profiles")
	}
	if scope == entities.ScopeUser {
		teamProfiles, teamErr := listEnabledTeamSessionProfiles(ctx.Request().Context(), c.repo, c.settingsRepo, userID, userTeamIDs)
		if teamErr != nil {
			log.Printf("Failed to list enabled team session profiles for preview: %v", teamErr)
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to list session profiles")
		}
		profiles = append(profiles, teamProfiles...)
	}

	if profile := selectSessionProfileByTags(profiles, req.Tags); profile != nil {
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"source":  "selector_tags",
			"profile": c.toResponse(profile),
		})
	}

	if c.settingsRepo != nil {
		settingsName := userID
		if scope == entities.ScopeTeam && resolvedTeamID != "" {
			settingsName = resolvedTeamID
		}
		settings, err := c.settingsRepo.FindByName(ctx.Request().Context(), settingsName)
		if err == nil && settings != nil && settings.DefaultSessionProfileID() != "" {
			defaultID := settings.DefaultSessionProfileID()
			for _, p := range profiles {
				if p.ID() == defaultID {
					return ctx.JSON(http.StatusOK, map[string]interface{}{
						"source":  "settings_default",
						"profile": c.toResponse(p),
					})
				}
			}
		}
	}

	for _, p := range profiles {
		if p.IsDefault() {
			return ctx.JSON(http.StatusOK, map[string]interface{}{
				"source":  "profile_default",
				"profile": c.toResponse(p),
			})
		}
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"source":  "none",
		"profile": nil,
	})
}

// GetSessionProfile handles GET /session-profiles/:id
