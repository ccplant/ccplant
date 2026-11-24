package entities

import (
	"time"
)

// WebhookType defines the type of webhook source
type WebhookType string

const (
	// WebhookTypeGitHub indicates a GitHub/GHES webhook
	WebhookTypeGitHub WebhookType = "github"
	// WebhookTypeCustom indicates a custom webhook (reserved for future)
	WebhookTypeCustom WebhookType = "custom"
)

// WebhookStatus defines the current status of a webhook
type WebhookStatus string

const (
	// WebhookStatusActive indicates the webhook is active and will process events
	WebhookStatusActive WebhookStatus = "active"
	// WebhookStatusPaused indicates the webhook is paused
	WebhookStatusPaused WebhookStatus = "paused"
)

// WebhookSignatureType defines the signature verification type
type WebhookSignatureType string

const (
	// WebhookSignatureTypeHMAC indicates HMAC signature verification (default)
	WebhookSignatureTypeHMAC WebhookSignatureType = "hmac"
	// WebhookSignatureTypeStatic indicates static token comparison
	WebhookSignatureTypeStatic WebhookSignatureType = "static"
)

// DeliveryStatus defines the status of a webhook delivery
type DeliveryStatus string

const (
	// DeliveryStatusProcessed indicates the webhook was processed and a session was created
	DeliveryStatusProcessed DeliveryStatus = "processed"
	// DeliveryStatusSkipped indicates the webhook was skipped (no matching trigger)
	DeliveryStatusSkipped DeliveryStatus = "skipped"
	// DeliveryStatusFailed indicates the webhook processing failed
	DeliveryStatusFailed DeliveryStatus = "failed"
)

// Webhook represents a webhook configuration entity
type Webhook struct {
	id     string
	name   string
	userID string
	scope  ResourceScope
	teamID string
	// userTeams holds the GitHub team slugs of the webhook creator, captured at
	// create/update time.  Used to inject team-level settings (Bedrock, MCP, etc.)
	// when the webhook fires, because there is no live auth context at trigger time.
	// Mirrors the UserTeams field on Schedule.
	userTeams       []string
	status          WebhookStatus
	webhookType     WebhookType
	secret          string
	signatureHeader string
	signatureType   WebhookSignatureType
	signaturePrefix string
	github          *WebhookGitHubConfig
	triggers        []WebhookTrigger
	sessionConfig   *WebhookSessionConfig
	maxSessions     int
	createdAt       time.Time
	updatedAt       time.Time
	lastDelivery    *WebhookDeliveryRecord
	deliveryCount   int64
}

// NewWebhook creates a new Webhook entity
func NewWebhook(id, name, userID string, webhookType WebhookType) *Webhook {
	now := time.Now()
	return &Webhook{
		id:          id,
		name:        name,
		userID:      userID,
		webhookType: webhookType,
		status:      WebhookStatusActive,
		triggers:    []WebhookTrigger{},
		maxSessions: 10, // Default maximum concurrent sessions per webhook
		createdAt:   now,
		updatedAt:   now,
	}
}

// ID returns the webhook ID
func (w *Webhook) ID() string { return w.id }

// Name returns the webhook name
func (w *Webhook) Name() string { return w.name }

// SetName sets the webhook name
func (w *Webhook) SetName(name string) {
	w.name = name
	w.updatedAt = time.Now()
}

// UserID returns the user ID
func (w *Webhook) UserID() string { return w.userID }

// Scope returns the resource scope
func (w *Webhook) Scope() ResourceScope {
	if w.scope == "" {
		return ScopeUser
	}
	return w.scope
}

// SetScope sets the resource scope
func (w *Webhook) SetScope(scope ResourceScope) {
	w.scope = scope
	w.updatedAt = time.Now()
}

// TeamID returns the team ID
func (w *Webhook) TeamID() string { return w.teamID }

// SetTeamID sets the team ID
func (w *Webhook) SetTeamID(teamID string) {
	w.teamID = teamID
	w.updatedAt = time.Now()
}

// SetOwnership updates the resource ownership metadata.
func (w *Webhook) SetOwnership(scope ResourceScope, userID, teamID string) {
	w.scope = scope
	w.userID = userID
	if scope == ScopeTeam {
		w.teamID = teamID
	} else {
		w.teamID = ""
	}
	w.updatedAt = time.Now()
}

// UserTeams returns the GitHub team slugs captured at webhook creation/update time.
// These are used to inject team-level settings into sessions spawned by this webhook.
func (w *Webhook) UserTeams() []string {
	result := make([]string, len(w.userTeams))
	copy(result, w.userTeams)
	return result
}

// SetUserTeams stores the GitHub team slugs for settings merging.
// Should be called with the webhook owner's current team memberships on create/update.
func (w *Webhook) SetUserTeams(teams []string) {
	w.userTeams = teams
	w.updatedAt = time.Now()
}

// Status returns the webhook status
func (w *Webhook) Status() WebhookStatus { return w.status }

// SetStatus sets the webhook status
func (w *Webhook) SetStatus(status WebhookStatus) {
	w.status = status
	w.updatedAt = time.Now()
}

// WebhookType returns the webhook type
func (w *Webhook) WebhookType() WebhookType { return w.webhookType }

// Secret returns the webhook secret
func (w *Webhook) Secret() string { return w.secret }

// SetSecret sets the webhook secret
func (w *Webhook) SetSecret(secret string) {
	w.secret = secret
	w.updatedAt = time.Now()
}

// MaskSecret returns the secret with only the last 4 characters visible
func (w *Webhook) MaskSecret() string {
	if len(w.secret) <= 4 {
		return "****"
	}
	return "****" + w.secret[len(w.secret)-4:]
}

// SignatureHeader returns the signature header name (defaults to "X-Signature")
func (w *Webhook) SignatureHeader() string {
	if w.signatureHeader == "" {
		return "X-Signature"
	}
	return w.signatureHeader
}

// SetSignatureHeader sets the signature header name
func (w *Webhook) SetSignatureHeader(header string) {
	w.signatureHeader = header
	w.updatedAt = time.Now()
}

// SignatureType returns the signature verification type (defaults to "hmac")
func (w *Webhook) SignatureType() WebhookSignatureType {
	if w.signatureType == "" {
		return WebhookSignatureTypeHMAC
	}
	return w.signatureType
}

// SetSignatureType sets the signature verification type
func (w *Webhook) SetSignatureType(sigType WebhookSignatureType) {
	w.signatureType = sigType
	w.updatedAt = time.Now()
}

// SignaturePrefix returns the signature prefix to strip from the header value.
// When empty, auto-detection is used (e.g., "sha256=" prefix is detected automatically).
// When set, this exact prefix is stripped before comparing the HMAC digest.
// For example, set "sha256=" for GitHub-style, or "" for plain hex (Sentry-style).
func (w *Webhook) SignaturePrefix() string {
	return w.signaturePrefix
}

// SetSignaturePrefix sets the signature prefix to strip from the header value.
func (w *Webhook) SetSignaturePrefix(prefix string) {
	w.signaturePrefix = prefix
	w.updatedAt = time.Now()
}

// GitHub returns the GitHub configuration
func (w *Webhook) GitHub() *WebhookGitHubConfig { return w.github }

// SetGitHub sets the GitHub configuration
func (w *Webhook) SetGitHub(github *WebhookGitHubConfig) {
	w.github = github
	w.updatedAt = time.Now()
}

// Triggers returns the webhook triggers
func (w *Webhook) Triggers() []WebhookTrigger { return w.triggers }

// SetTriggers sets the webhook triggers
func (w *Webhook) SetTriggers(triggers []WebhookTrigger) {
	w.triggers = triggers
	w.updatedAt = time.Now()
}

// AddTrigger adds a trigger to the webhook
func (w *Webhook) AddTrigger(trigger WebhookTrigger) {
	w.triggers = append(w.triggers, trigger)
	w.updatedAt = time.Now()
}

// SessionConfig returns the session configuration
func (w *Webhook) SessionConfig() *WebhookSessionConfig { return w.sessionConfig }

// SetSessionConfig sets the session configuration
func (w *Webhook) SetSessionConfig(config *WebhookSessionConfig) {
	w.sessionConfig = config
	w.updatedAt = time.Now()
}

// MaxSessions returns the maximum concurrent sessions allowed for this webhook
// Returns 10 as default if not set or set to 0
func (w *Webhook) MaxSessions() int {
	if w.maxSessions <= 0 {
		return 10
	}
	return w.maxSessions
}

// SetMaxSessions sets the maximum concurrent sessions allowed for this webhook
func (w *Webhook) SetMaxSessions(max int) {
	w.maxSessions = max
	w.updatedAt = time.Now()
}

// CreatedAt returns the creation time
func (w *Webhook) CreatedAt() time.Time { return w.createdAt }

// SetCreatedAt sets the creation time when restoring persisted state.
func (w *Webhook) SetCreatedAt(t time.Time) { w.createdAt = t }

// UpdatedAt returns the last update time
func (w *Webhook) UpdatedAt() time.Time { return w.updatedAt }

// SetUpdatedAt sets the update time
func (w *Webhook) SetUpdatedAt(t time.Time) { w.updatedAt = t }

// LastDelivery returns the last delivery record
func (w *Webhook) LastDelivery() *WebhookDeliveryRecord { return w.lastDelivery }

// SetLastDelivery sets the last delivery record
func (w *Webhook) SetLastDelivery(record *WebhookDeliveryRecord) {
	w.lastDelivery = record
	w.updatedAt = time.Now()
}

// DeliveryCount returns the delivery count
func (w *Webhook) DeliveryCount() int64 { return w.deliveryCount }

// SetDeliveryCount sets the delivery count when restoring persisted state.
func (w *Webhook) SetDeliveryCount(count int64) { w.deliveryCount = count }

// IncrementDeliveryCount increments the delivery count
func (w *Webhook) IncrementDeliveryCount() {
	w.deliveryCount++
	w.updatedAt = time.Now()
}

// Validate validates the webhook configuration
func (w *Webhook) Validate() error {
	if w.id == "" {
		return ErrInvalidWebhook{Field: "id", Message: "id is required"}
	}
	if w.name == "" {
		return ErrInvalidWebhook{Field: "name", Message: "name is required"}
	}
	if w.userID == "" {
		return ErrInvalidWebhook{Field: "user_id", Message: "user_id is required"}
	}
	if w.webhookType == "" {
		return ErrInvalidWebhook{Field: "type", Message: "type is required"}
	}
	if w.webhookType != WebhookTypeGitHub && w.webhookType != WebhookTypeCustom {
		return ErrInvalidWebhook{Field: "type", Message: "type must be 'github' or 'custom'"}
	}
	if len(w.triggers) == 0 {
		return ErrInvalidWebhook{Field: "triggers", Message: "at least one trigger is required"}
	}
	return nil
}

// WebhookGitHubConfig contains GitHub-specific webhook configuration
type WebhookGitHubConfig struct {
	enterpriseURL       string
	allowedEvents       []string
	allowedRepositories []string
}

// NewWebhookGitHubConfig creates a new GitHub config
func NewWebhookGitHubConfig() *WebhookGitHubConfig {
	return &WebhookGitHubConfig{}
}

// EnterpriseURL returns the enterprise URL
func (c *WebhookGitHubConfig) EnterpriseURL() string { return c.enterpriseURL }

// SetEnterpriseURL sets the enterprise URL
