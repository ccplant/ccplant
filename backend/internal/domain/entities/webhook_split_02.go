package entities

import (
	"time"
)

func (c *WebhookGitHubConfig) SetEnterpriseURL(url string) { c.enterpriseURL = url }

// AllowedEvents returns the allowed events
func (c *WebhookGitHubConfig) AllowedEvents() []string { return c.allowedEvents }

// SetAllowedEvents sets the allowed events
func (c *WebhookGitHubConfig) SetAllowedEvents(events []string) { c.allowedEvents = events }

// AllowedRepositories returns the allowed repositories
func (c *WebhookGitHubConfig) AllowedRepositories() []string { return c.allowedRepositories }

// SetAllowedRepositories sets the allowed repositories
func (c *WebhookGitHubConfig) SetAllowedRepositories(repos []string) { c.allowedRepositories = repos }

// WebhookTrigger defines a rule for starting a session based on webhook payload
type WebhookTrigger struct {
	id            string
	name          string
	priority      int
	enabled       bool
	conditions    WebhookTriggerConditions
	sessionConfig *WebhookSessionConfig
	stopOnMatch   bool
}

// NewWebhookTrigger creates a new trigger
func NewWebhookTrigger(id, name string) WebhookTrigger {
	return WebhookTrigger{
		id:      id,
		name:    name,
		enabled: true,
	}
}

// ID returns the trigger ID
func (t *WebhookTrigger) ID() string { return t.id }

// Name returns the trigger name
func (t *WebhookTrigger) Name() string { return t.name }

// SetName sets the trigger name
func (t *WebhookTrigger) SetName(name string) { t.name = name }

// Priority returns the priority
func (t *WebhookTrigger) Priority() int { return t.priority }

// SetPriority sets the priority
func (t *WebhookTrigger) SetPriority(priority int) { t.priority = priority }

// Enabled returns whether the trigger is enabled
func (t *WebhookTrigger) Enabled() bool { return t.enabled }

// SetEnabled sets whether the trigger is enabled
func (t *WebhookTrigger) SetEnabled(enabled bool) { t.enabled = enabled }

// Conditions returns the trigger conditions
func (t *WebhookTrigger) Conditions() WebhookTriggerConditions { return t.conditions }

// SetConditions sets the trigger conditions
func (t *WebhookTrigger) SetConditions(conditions WebhookTriggerConditions) {
	t.conditions = conditions
}

// SessionConfig returns the session configuration
func (t *WebhookTrigger) SessionConfig() *WebhookSessionConfig { return t.sessionConfig }

// SetSessionConfig sets the session configuration
func (t *WebhookTrigger) SetSessionConfig(config *WebhookSessionConfig) { t.sessionConfig = config }

// StopOnMatch returns whether to stop on match
func (t *WebhookTrigger) StopOnMatch() bool { return t.stopOnMatch }

// SetStopOnMatch sets whether to stop on match
func (t *WebhookTrigger) SetStopOnMatch(stop bool) { t.stopOnMatch = stop }

// WebhookTriggerConditions defines the conditions for a trigger to match
type WebhookTriggerConditions struct {
	github     *WebhookGitHubConditions
	goTemplate string
}

// GitHub returns the GitHub conditions
func (c WebhookTriggerConditions) GitHub() *WebhookGitHubConditions { return c.github }

// SetGitHub sets the GitHub conditions
func (c *WebhookTriggerConditions) SetGitHub(github *WebhookGitHubConditions) { c.github = github }

// GoTemplate returns the Go template condition
func (c WebhookTriggerConditions) GoTemplate() string { return c.goTemplate }

// SetGoTemplate sets the Go template condition
func (c *WebhookTriggerConditions) SetGoTemplate(goTemplate string) {
	c.goTemplate = goTemplate
}

// WebhookGitHubConditions defines GitHub-specific trigger conditions
type WebhookGitHubConditions struct {
	events       []string
	actions      []string
	branches     []string
	repositories []string
	labels       []string
	paths        []string
	baseBranches []string
	draft        *bool
	sender       []string
}

// NewWebhookGitHubConditions creates new GitHub conditions
func NewWebhookGitHubConditions() *WebhookGitHubConditions {
	return &WebhookGitHubConditions{}
}

// Events returns the events
func (c *WebhookGitHubConditions) Events() []string { return c.events }

// SetEvents sets the events
func (c *WebhookGitHubConditions) SetEvents(events []string) { c.events = events }

// Actions returns the actions
func (c *WebhookGitHubConditions) Actions() []string { return c.actions }

// SetActions sets the actions
func (c *WebhookGitHubConditions) SetActions(actions []string) { c.actions = actions }

// Branches returns the branches
func (c *WebhookGitHubConditions) Branches() []string { return c.branches }

// SetBranches sets the branches
func (c *WebhookGitHubConditions) SetBranches(branches []string) { c.branches = branches }

// Repositories returns the repositories
func (c *WebhookGitHubConditions) Repositories() []string { return c.repositories }

// SetRepositories sets the repositories
func (c *WebhookGitHubConditions) SetRepositories(repositories []string) {
	c.repositories = repositories
}

// Labels returns the labels
func (c *WebhookGitHubConditions) Labels() []string { return c.labels }

// SetLabels sets the labels
func (c *WebhookGitHubConditions) SetLabels(labels []string) { c.labels = labels }

// Paths returns the paths
func (c *WebhookGitHubConditions) Paths() []string { return c.paths }

// SetPaths sets the paths
func (c *WebhookGitHubConditions) SetPaths(paths []string) { c.paths = paths }

// BaseBranches returns the base branches
func (c *WebhookGitHubConditions) BaseBranches() []string { return c.baseBranches }

// SetBaseBranches sets the base branches
func (c *WebhookGitHubConditions) SetBaseBranches(baseBranches []string) {
	c.baseBranches = baseBranches
}

// Draft returns the draft filter
func (c *WebhookGitHubConditions) Draft() *bool { return c.draft }

// SetDraft sets the draft filter
func (c *WebhookGitHubConditions) SetDraft(draft *bool) { c.draft = draft }

// Sender returns the sender filter
func (c *WebhookGitHubConditions) Sender() []string { return c.sender }

// SetSender sets the sender filter
func (c *WebhookGitHubConditions) SetSender(sender []string) { c.sender = sender }

// WebhookSessionConfig contains session creation parameters
type WebhookSessionConfig struct {
	environment            map[string]string
	tags                   map[string]string
	initialMessageTemplate string
	reuseMessageTemplate   string
	params                 *SessionParams
	reuseSession           bool
	mountPayload           bool
	// sessionProfileID is an optional reference to a SessionProfile.
	// When set, the profile's config is used as a base; explicit fields override it.
	sessionProfileID string
}

// NewWebhookSessionConfig creates a new session config
func NewWebhookSessionConfig() *WebhookSessionConfig {
	return &WebhookSessionConfig{
		environment: make(map[string]string),
		tags:        make(map[string]string),
	}
}

// Environment returns the environment variables
func (c *WebhookSessionConfig) Environment() map[string]string { return c.environment }

// SetEnvironment sets the environment variables
func (c *WebhookSessionConfig) SetEnvironment(env map[string]string) { c.environment = env }

// Tags returns the tags
func (c *WebhookSessionConfig) Tags() map[string]string { return c.tags }

// SetTags sets the tags
func (c *WebhookSessionConfig) SetTags(tags map[string]string) { c.tags = tags }

// InitialMessageTemplate returns the initial message template
func (c *WebhookSessionConfig) InitialMessageTemplate() string { return c.initialMessageTemplate }

// SetInitialMessageTemplate sets the initial message template
func (c *WebhookSessionConfig) SetInitialMessageTemplate(template string) {
	c.initialMessageTemplate = template
}

// ReuseMessageTemplate returns the reuse message template
func (c *WebhookSessionConfig) ReuseMessageTemplate() string { return c.reuseMessageTemplate }

// SetReuseMessageTemplate sets the reuse message template
func (c *WebhookSessionConfig) SetReuseMessageTemplate(template string) {
	c.reuseMessageTemplate = template
}

// Params returns the session params
func (c *WebhookSessionConfig) Params() *SessionParams { return c.params }

// SetParams sets the session params
func (c *WebhookSessionConfig) SetParams(params *SessionParams) { c.params = params }

// ReuseSession returns whether to reuse existing sessions
func (c *WebhookSessionConfig) ReuseSession() bool { return c.reuseSession }

// SetReuseSession sets whether to reuse existing sessions
func (c *WebhookSessionConfig) SetReuseSession(reuse bool) { c.reuseSession = reuse }

// MountPayload returns whether to mount the webhook payload
func (c *WebhookSessionConfig) MountPayload() bool { return c.mountPayload }

// SetMountPayload sets whether to mount the webhook payload
func (c *WebhookSessionConfig) SetMountPayload(mount bool) { c.mountPayload = mount }

// SessionProfileID returns the optional session profile ID reference
func (c *WebhookSessionConfig) SessionProfileID() string { return c.sessionProfileID }

// SetSessionProfileID sets the optional session profile ID reference
func (c *WebhookSessionConfig) SetSessionProfileID(id string) { c.sessionProfileID = id }

// WebhookDeliveryRecord represents a single webhook delivery
type WebhookDeliveryRecord struct {
	id             string
	receivedAt     time.Time
	status         DeliveryStatus
	matchedTrigger string
	sessionID      string
	errorMessage   string
	sessionReused  bool
}

// NewWebhookDeliveryRecord creates a new delivery record
func NewWebhookDeliveryRecord(id string, status DeliveryStatus) *WebhookDeliveryRecord {
	return &WebhookDeliveryRecord{
		id:         id,
		receivedAt: time.Now(),
		status:     status,
	}
}

// ID returns the delivery ID
func (r *WebhookDeliveryRecord) ID() string { return r.id }

// ReceivedAt returns the received time
func (r *WebhookDeliveryRecord) ReceivedAt() time.Time { return r.receivedAt }

// SetReceivedAt sets the received time when restoring persisted state.
func (r *WebhookDeliveryRecord) SetReceivedAt(t time.Time) { r.receivedAt = t }

// Status returns the delivery status
func (r *WebhookDeliveryRecord) Status() DeliveryStatus { return r.status }

// MatchedTrigger returns the matched trigger ID
func (r *WebhookDeliveryRecord) MatchedTrigger() string { return r.matchedTrigger }

// SetMatchedTrigger sets the matched trigger ID
func (r *WebhookDeliveryRecord) SetMatchedTrigger(triggerID string) { r.matchedTrigger = triggerID }

// SessionID returns the session ID
func (r *WebhookDeliveryRecord) SessionID() string { return r.sessionID }

// SetSessionID sets the session ID
func (r *WebhookDeliveryRecord) SetSessionID(sessionID string) { r.sessionID = sessionID }

// Error returns the error message
func (r *WebhookDeliveryRecord) Error() string { return r.errorMessage }

// SetError sets the error message
func (r *WebhookDeliveryRecord) SetError(errorMessage string) { r.errorMessage = errorMessage }

// SessionReused returns whether the session was reused
func (r *WebhookDeliveryRecord) SessionReused() bool { return r.sessionReused }

// SetSessionReused sets whether the session was reused
func (r *WebhookDeliveryRecord) SetSessionReused(reused bool) { r.sessionReused = reused }

// ErrInvalidWebhook represents a validation error
type ErrInvalidWebhook struct {
	Field   string
	Message string
}

func (e ErrInvalidWebhook) Error() string {
	return "invalid webhook: " + e.Field + ": " + e.Message
}

// ErrWebhookNotFound is returned when a webhook is not found
type ErrWebhookNotFound struct {
	ID string
}

func (e ErrWebhookNotFound) Error() string {
	return "webhook not found: " + e.ID
}
