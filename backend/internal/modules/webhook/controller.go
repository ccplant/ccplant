package webhook

import (
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// WebhookController handles webhook management HTTP requests
type WebhookController struct {
	repo    repositories.WebhookRepository
	baseURL string
}

// NewWebhookController creates a new webhook controller
func NewWebhookController(repo repositories.WebhookRepository) *WebhookController {
	return &WebhookController{
		repo: repo,
	}
}

// SetBaseURL sets the base URL for webhook URLs
func (c *WebhookController) SetBaseURL(baseURL string) {
	c.baseURL = baseURL
}

// GetName returns the name of this controller for logging
func (c *WebhookController) GetName() string {
	return "WebhookController"
}

// CreateWebhookRequest represents the request body for creating a webhook
type CreateWebhookRequest struct {
	Name            string                        `json:"name"`
	Scope           entities.ResourceScope        `json:"scope,omitempty"`
	TeamID          string                        `json:"team_id,omitempty"`
	Status          entities.WebhookStatus        `json:"status,omitempty"`
	Type            entities.WebhookType          `json:"type"`
	Secret          string                        `json:"secret,omitempty"`
	SignatureHeader string                        `json:"signature_header,omitempty"`
	SignatureType   entities.WebhookSignatureType `json:"signature_type,omitempty"`
	SignaturePrefix string                        `json:"signature_prefix,omitempty"`
	GitHub          *GitHubConfigRequest          `json:"github,omitempty"`
	Triggers        []TriggerRequest              `json:"triggers"`
	SessionConfig   *SessionConfigRequest         `json:"session_config,omitempty"`
	MaxSessions     int                           `json:"max_sessions,omitempty"`
}

// GitHubConfigRequest represents GitHub-specific configuration in requests
type GitHubConfigRequest struct {
	EnterpriseURL       string   `json:"enterprise_url,omitempty"`
	AllowedEvents       []string `json:"allowed_events,omitempty"`
	AllowedRepositories []string `json:"allowed_repositories,omitempty"`
}

// TriggerRequest represents a trigger in requests
type TriggerRequest struct {
	ID            string                   `json:"id,omitempty"`
	Name          string                   `json:"name"`
	Priority      int                      `json:"priority"`
	Enabled       bool                     `json:"enabled"`
	Conditions    TriggerConditionsRequest `json:"conditions"`
	SessionConfig *SessionConfigRequest    `json:"session_config,omitempty"`
	StopOnMatch   bool                     `json:"stop_on_match"`
}

// TriggerConditionsRequest represents trigger conditions in requests
type TriggerConditionsRequest struct {
	GitHub     *GitHubConditionsRequest `json:"github,omitempty"`
	GoTemplate string                   `json:"go_template,omitempty"`
}

// GitHubConditionsRequest represents GitHub-specific conditions in requests
type GitHubConditionsRequest struct {
	Events       []string `json:"events,omitempty"`
	Actions      []string `json:"actions,omitempty"`
	Branches     []string `json:"branches,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	BaseBranches []string `json:"base_branches,omitempty"`
	Draft        *bool    `json:"draft,omitempty"`
	Sender       []string `json:"sender,omitempty"`
}

// SessionConfigRequest represents session configuration in requests
type SessionConfigRequest struct {
	Environment            map[string]string       `json:"environment,omitempty"`
	Tags                   map[string]string       `json:"tags,omitempty"`
	InitialMessageTemplate string                  `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                  `json:"reuse_message_template,omitempty"`
	Params                 *entities.SessionParams `json:"params,omitempty"`
	ReuseSession           bool                    `json:"reuse_session,omitempty"`
	MountPayload           bool                    `json:"mount_payload,omitempty"`
	SessionProfileID       string                  `json:"session_profile_id,omitempty"`
}

// UpdateWebhookRequest represents the request body for updating a webhook
type UpdateWebhookRequest struct {
	Name            *string                        `json:"name,omitempty"`
	Status          *entities.WebhookStatus        `json:"status,omitempty"`
	Secret          *string                        `json:"secret,omitempty"`
	SignatureHeader *string                        `json:"signature_header,omitempty"`
	SignatureType   *entities.WebhookSignatureType `json:"signature_type,omitempty"`
	SignaturePrefix *string                        `json:"signature_prefix,omitempty"`
	GitHub          *GitHubConfigRequest           `json:"github,omitempty"`
	Triggers        []TriggerRequest               `json:"triggers,omitempty"`
	SessionConfig   *SessionConfigRequest          `json:"session_config,omitempty"`
	MaxSessions     *int                           `json:"max_sessions,omitempty"`
}

// WebhookResponse represents the response for a webhook
type WebhookResponse struct {
	ID              string                        `json:"id"`
	Name            string                        `json:"name"`
	UserID          string                        `json:"user_id"`
	Scope           entities.ResourceScope        `json:"scope,omitempty"`
	TeamID          string                        `json:"team_id,omitempty"`
	Status          entities.WebhookStatus        `json:"status"`
	Type            entities.WebhookType          `json:"type"`
	Secret          string                        `json:"secret"`
	SignatureHeader string                        `json:"signature_header,omitempty"`
	SignatureType   entities.WebhookSignatureType `json:"signature_type,omitempty"`
	SignaturePrefix string                        `json:"signature_prefix,omitempty"`
	WebhookURL      string                        `json:"webhook_url"`
	GitHub          *GitHubConfigResponse         `json:"github,omitempty"`
	Triggers        []TriggerResponse             `json:"triggers"`
	SessionConfig   *SessionConfigResponse        `json:"session_config,omitempty"`
	MaxSessions     int                           `json:"max_sessions"`
	CreatedAt       string                        `json:"created_at"`
	UpdatedAt       string                        `json:"updated_at"`
	LastDelivery    *DeliveryRecordResponse       `json:"last_delivery,omitempty"`
	DeliveryCount   int64                         `json:"delivery_count"`
}

// GitHubConfigResponse represents GitHub-specific configuration in responses
type GitHubConfigResponse struct {
	EnterpriseURL       string   `json:"enterprise_url,omitempty"`
	AllowedEvents       []string `json:"allowed_events,omitempty"`
	AllowedRepositories []string `json:"allowed_repositories,omitempty"`
}

// TriggerResponse represents a trigger in responses
type TriggerResponse struct {
	ID            string                    `json:"id"`
	Name          string                    `json:"name"`
	Priority      int                       `json:"priority"`
	Enabled       bool                      `json:"enabled"`
	Conditions    TriggerConditionsResponse `json:"conditions"`
	SessionConfig *SessionConfigResponse    `json:"session_config,omitempty"`
	StopOnMatch   bool                      `json:"stop_on_match"`
}

// TriggerConditionsResponse represents trigger conditions in responses
type TriggerConditionsResponse struct {
	GitHub     *GitHubConditionsResponse `json:"github,omitempty"`
	GoTemplate string                    `json:"go_template,omitempty"`
}

// GitHubConditionsResponse represents GitHub-specific conditions in responses
type GitHubConditionsResponse struct {
	Events       []string `json:"events,omitempty"`
	Actions      []string `json:"actions,omitempty"`
	Branches     []string `json:"branches,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	BaseBranches []string `json:"base_branches,omitempty"`
	Draft        *bool    `json:"draft,omitempty"`
	Sender       []string `json:"sender,omitempty"`
}

// SessionConfigResponse represents session configuration in responses
type SessionConfigResponse struct {
	Environment            map[string]string      `json:"environment,omitempty"`
	Tags                   map[string]string      `json:"tags,omitempty"`
	InitialMessageTemplate string                 `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                 `json:"reuse_message_template,omitempty"`
	Params                 *SessionParamsResponse `json:"params,omitempty"`
	ReuseSession           bool                   `json:"reuse_session,omitempty"`
	MountPayload           bool                   `json:"mount_payload,omitempty"`
	SessionProfileID       string                 `json:"session_profile_id,omitempty"`
}

// SessionParamsResponse represents session params in responses
type SessionParamsResponse struct {
	GithubToken      string `json:"github_token,omitempty"`
	AgentType        string `json:"agent_type,omitempty"`
	Model            string `json:"model,omitempty"`
	Oneshot          bool   `json:"oneshot,omitempty"`
	SessionTTL       string `json:"session_ttl,omitempty"`
	AuthProxy        *bool  `json:"auth_proxy,omitempty"`
	CredentialSource string `json:"credential_source,omitempty"`
}

// DeliveryRecordResponse represents a delivery record in responses
type DeliveryRecordResponse struct {
	ID             string `json:"id"`
	ReceivedAt     string `json:"received_at"`
	Status         string `json:"status"`
	MatchedTrigger string `json:"matched_trigger,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

// TriggerWebhookRequest represents the request body for triggering a webhook with a test payload
type TriggerWebhookRequest struct {
	Payload map[string]interface{} `json:"payload"`         // The test payload JSON
	Event   string                 `json:"event,omitempty"` // For GitHub webhooks: the event type (e.g., "push", "pull_request")
	DryRun  bool                   `json:"dry_run"`         // If true, only evaluate triggers without creating a session
}

// TriggerWebhookResponse represents the response for a webhook trigger test
type TriggerWebhookResponse struct {
	Matched        bool                       `json:"matched"`                   // Whether any trigger matched
	MatchedTrigger *TriggerMatchedTriggerInfo `json:"matched_trigger,omitempty"` // Info about the matched trigger
	SessionID      string                     `json:"session_id,omitempty"`      // Set when dry_run=false and session was created
	SessionReused  bool                       `json:"session_reused,omitempty"`  // Whether an existing session was reused
	DryRun         bool                       `json:"dry_run"`                   // Echoes back the dry_run flag
	InitialMessage string                     `json:"initial_message,omitempty"` // The rendered initial message (in dry_run mode)
	Tags           map[string]string          `json:"tags,omitempty"`            // The computed tags (in dry_run mode)
	Environment    map[string]string          `json:"environment,omitempty"`     // The computed environment (in dry_run mode)
	Error          string                     `json:"error,omitempty"`           // Any error encountered during evaluation
}

// TriggerMatchedTriggerInfo contains information about a matched trigger
type TriggerMatchedTriggerInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateWebhook handles POST /webhooks
func (c *WebhookController) CreateWebhook(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	var req CreateWebhookRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate request
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.Type == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "type is required")
	}
	if req.Type != entities.WebhookTypeGitHub && req.Type != entities.WebhookTypeCustom {
		return echo.NewHTTPError(http.StatusBadRequest, "type must be 'github' or 'custom'")
	}
	if req.Status != "" && req.Status != entities.WebhookStatusActive && req.Status != entities.WebhookStatusPaused {
		return echo.NewHTTPError(http.StatusBadRequest, "status must be active or paused")
	}
	if len(req.Triggers) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "at least one trigger is required")
	}

	// Validate templates before creating webhook
	if err := c.validateWebhookTemplates(req.Type, req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Template validation error: %v", err))
	}

	// Reject enterprise URLs that point at internal infrastructure (SSRF)
	if req.GitHub != nil {
		if err := validateEnterpriseURL(req.GitHub.EnterpriseURL); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	// Get user from context
	user := auth.GetUserFromContext(ctx)
	var userID string
	if user != nil {
		userID = string(user.ID())
	} else {
		userID = "anonymous"
	}

	// Service accounts cannot use user scope; transparently route to team scope.
	{
		resolvedScope, resolvedTeamID := auth.ResolveUserScope(user, string(req.Scope), req.TeamID)
		req.Scope = entities.ResourceScope(resolvedScope)
		req.TeamID = resolvedTeamID
	}

	// Validate team scope
	if req.Scope == entities.ScopeTeam {
		if req.TeamID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
		}
		if user == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required for team-scoped webhooks")
		}
		// Use AuthorizationContext.CanCreateInTeam which handles service accounts correctly.
		// service accounts have their team populated in TeamScope.TeamPermissions via buildAuthorizationContext.
		if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx == nil || !authzCtx.CanCreateInTeam(req.TeamID) {
			log.Printf("User %s is not a member of team %s", userID, req.TeamID)
			return echo.NewHTTPError(http.StatusForbidden, "You are not a member of this team")
		}
	}

	// Create webhook entity
	webhook := entities.NewWebhook(uuid.New().String(), req.Name, userID, req.Type)
	webhook.SetScope(req.Scope)
	webhook.SetTeamID(req.TeamID)
	if req.Status != "" {
		webhook.SetStatus(req.Status)
	}
	if req.Secret != "" {
		webhook.SetSecret(req.Secret)
	}
	if req.SignatureHeader != "" {
		webhook.SetSignatureHeader(req.SignatureHeader)
	}
	if req.SignatureType != "" {
		webhook.SetSignatureType(req.SignatureType)
	}
	if req.SignaturePrefix != "" {
		webhook.SetSignaturePrefix(req.SignaturePrefix)
	}
	if req.MaxSessions > 0 {
		webhook.SetMaxSessions(req.MaxSessions)
	}

	// Capture creator's team memberships for user-scoped webhooks so that the
	// webhook handler can inject team-level settings without a live auth context.
	// Mirrors the UserTeams pattern used by Schedule.
	if req.Scope != entities.ScopeTeam {
		if azCtx := auth.GetAuthorizationContext(ctx); azCtx != nil {
			webhook.SetUserTeams(azCtx.TeamScope.Teams)
		}
	}

	// Set GitHub config
	if req.GitHub != nil {
		webhook.SetGitHub(c.requestToGitHubConfig(req.GitHub))
	}

	// Set triggers and session config
	webhook.SetTriggers(c.requestToTriggers(req.Triggers))
	if req.SessionConfig != nil {
		webhook.SetSessionConfig(c.requestToSessionConfig(req.SessionConfig))
	}

	if err := c.repo.Create(ctx.Request().Context(), webhook); err != nil {
		log.Printf("Failed to create webhook: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create webhook")
	}

	log.Printf("Created webhook %s (%s) for user %s", webhook.ID(), webhook.Name(), userID)

	return ctx.JSON(http.StatusCreated, c.toResponse(ctx, webhook))
}

// ListWebhooks handles GET /webhooks
