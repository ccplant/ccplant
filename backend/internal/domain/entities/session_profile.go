package entities

import (
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

// SessionProfile represents a named, reusable session configuration for a tenant
type SessionProfile struct {
	id                   string
	name                 string
	description          string
	userID               string
	scope                ResourceScope
	teamID               string
	isDefault            bool
	selectorTags         map[string]string
	selectorRepositories []string
	config               SessionProfileConfig
	createdAt            time.Time
	updatedAt            time.Time
}

// SessionProfileConfig contains the reusable session configuration fields
type SessionProfileConfig struct {
	settingsTeamID         string
	codexConnection        *modelprovider.Connection
	claudeConnection       *modelprovider.Connection
	piConnection           *modelprovider.Connection
	environment            map[string]string
	tags                   map[string]string
	pool                   string
	initialMessageTemplate string
	reuseMessageTemplate   string
	commandWrapperTemplate string
	systemPrompt           string
	systemPromptMode       string
	params                 *SessionParams
	reuseSession           bool
	// sandboxPolicyID is a reference to a SandboxPolicy resource.
	// When set, the sandbox sidecar is enabled automatically with this policy applied.
	sandboxPolicyID string
	// sessionTTL overrides the global cleanup worker TTL for sessions created with this profile.
	// Accepted format: Go duration string (e.g. "48h", "168h").
	// Empty string means the global cleanup worker TTL is used.
	sessionTTL        string
	unsyncedFilePaths []string
	// sourceProfileID references another profile whose environment and MCP
	// servers are inherited before this profile's local overrides.
	sourceProfileID string
	// secretIDs selects settings-scoped secrets whose projections are exposed to
	// sessions created with this profile. An empty list preserves the legacy
	// behavior of exposing every projected secret in the settings scope.
	secretIDs          []string
	githubRepositories []string
	skills             []string
	piPackages         []string
	files              []ProfileFile
	repositories       []ProfileRepository
	mcpServers         *MCPServersSettings
	sessionState       *SessionStateConfig
}

// SessionStateConfig selects profile-owned Storage-backed state.
type SessionStateConfig struct {
	Enabled    bool     `json:"enabled"`
	StorageID  string   `json:"storage_id,omitempty"`
	Components []string `json:"components,omitempty"`
	Agents     []string `json:"agents,omitempty"`
}

// ErrSessionProfileAccessDenied is returned when a session profile cannot be
// used as the source of session settings.
type ErrSessionProfileAccessDenied struct {
	ID string
}

func (e ErrSessionProfileAccessDenied) Error() string {
	return "session profile access denied: " + e.ID
}

// ProfileFile represents a file owned by a session profile and written into a
// container when a session using that profile is created.
type ProfileFile struct {
	Name        string `json:"name,omitempty"`
	Path        string `json:"path"`
	Content     string `json:"content,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}

// ProfileRepository is cloned before the agent starts. Unlike the session's
// primary repository, it does not change the agent working directory.
type ProfileRepository struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Branch     string `json:"branch,omitempty"`
}

// NewSessionProfile creates a new SessionProfile
func NewSessionProfile(id, name, userID string) *SessionProfile {
	now := time.Now()
	return &SessionProfile{
		id:        id,
		name:      name,
		userID:    userID,
		scope:     ScopeUser,
		createdAt: now,
		updatedAt: now,
	}
}

// ID returns the profile ID
func (p *SessionProfile) ID() string { return p.id }

// Name returns the profile name
func (p *SessionProfile) Name() string { return p.name }

// SetName sets the profile name
func (p *SessionProfile) SetName(name string) {
	p.name = name
	p.updatedAt = time.Now()
}

// Description returns the profile description
func (p *SessionProfile) Description() string { return p.description }

// SetDescription sets the profile description
func (p *SessionProfile) SetDescription(desc string) {
	p.description = desc
	p.updatedAt = time.Now()
}

// UserID returns the user ID
func (p *SessionProfile) UserID() string { return p.userID }

// Scope returns the resource scope
func (p *SessionProfile) Scope() ResourceScope {
	if p.scope == "" {
		return ScopeUser
	}
	return p.scope
}

// SetScope sets the resource scope
func (p *SessionProfile) SetScope(scope ResourceScope) {
	p.scope = scope
	p.updatedAt = time.Now()
}

// TeamID returns the team ID
func (p *SessionProfile) TeamID() string { return p.teamID }

// SetTeamID sets the team ID
func (p *SessionProfile) SetTeamID(teamID string) {
	p.teamID = teamID
	p.updatedAt = time.Now()
}

// SetOwnership updates the resource ownership metadata.
func (p *SessionProfile) SetOwnership(scope ResourceScope, userID, teamID string) {
	p.scope = scope
	p.userID = userID
	if scope == ScopeTeam {
		p.teamID = teamID
	} else {
		p.teamID = ""
	}
	p.updatedAt = time.Now()
}

// IsDefault returns whether this profile is the tenant's default
func (p *SessionProfile) IsDefault() bool { return p.isDefault }

// SetIsDefault sets whether this profile is the tenant's default
func (p *SessionProfile) SetIsDefault(isDefault bool) {
	p.isDefault = isDefault
	p.updatedAt = time.Now()
}

// SelectorTags returns the tag selector used to choose this profile at launch time.
func (p *SessionProfile) SelectorTags() map[string]string {
	return copyStringMap(p.selectorTags)
}

// SetSelectorTags sets the tag selector used to choose this profile at launch time.
func (p *SessionProfile) SetSelectorTags(tags map[string]string) {
	p.selectorTags = copyStringMap(tags)
	p.updatedAt = time.Now()
}

// SelectorRepositories returns the repositories that may select this profile.
func (p *SessionProfile) SelectorRepositories() []string {
	return copyStringSlice(p.selectorRepositories)
}

// SetSelectorRepositories sets repository alternatives used for automatic selection.
func (p *SessionProfile) SetSelectorRepositories(repositories []string) {
	p.selectorRepositories = copyStringSlice(repositories)
	p.updatedAt = time.Now()
}

// MatchesSelectorTags returns true when requestTags contains all selector tags.
func (p *SessionProfile) MatchesSelectorTags(requestTags map[string]string) bool {
	if len(p.selectorTags) == 0 && len(p.selectorRepositories) == 0 {
		return false
	}
	if len(p.selectorRepositories) > 0 {
		repository := requestTags["repository"]
		if repository == "" {
			repository = requestTags["repo"]
		}
		matched := false
		for _, candidate := range p.selectorRepositories {
			if candidate == repository {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for k, v := range p.selectorTags {
		if k == "repo" {
			if requestTags["repository"] != v && requestTags["repo"] != v {
				return false
			}
			continue
		}
		if requestTags[k] != v {
			return false
		}
	}
	return true
}

// SelectorSpecificity returns the number of selector tags.
func (p *SessionProfile) SelectorSpecificity() int {
	specificity := len(p.selectorTags)
	if len(p.selectorRepositories) > 0 {
		specificity++
	}
	return specificity
}

// Config returns the session profile configuration
func (p *SessionProfile) Config() SessionProfileConfig { return p.config }

// SetConfig sets the session profile configuration
func (p *SessionProfile) SetConfig(cfg SessionProfileConfig) {
	p.config = cfg
	p.updatedAt = time.Now()
}

// CreatedAt returns the creation time
func (p *SessionProfile) CreatedAt() time.Time { return p.createdAt }

// UpdatedAt returns the last update time
func (p *SessionProfile) UpdatedAt() time.Time { return p.updatedAt }

// SetCreatedAt sets the creation time
func (p *SessionProfile) SetCreatedAt(t time.Time) { p.createdAt = t }

// SetUpdatedAt sets the update time
func (p *SessionProfile) SetUpdatedAt(t time.Time) { p.updatedAt = t }

// Validate validates the session profile
func (p *SessionProfile) Validate() error {
	if p.id == "" {
		return ErrInvalidSessionProfile{Field: "id", Message: "id is required"}
	}
	if p.name == "" {
		return ErrInvalidSessionProfile{Field: "name", Message: "name is required"}
	}
	if p.userID == "" {
		return ErrInvalidSessionProfile{Field: "user_id", Message: "user_id is required"}
	}
	return nil
}

// --- SessionProfileConfig accessors ---

// NewSessionProfileConfig creates a new SessionProfileConfig
func NewSessionProfileConfig() SessionProfileConfig {
	return SessionProfileConfig{
		environment: make(map[string]string),
		tags:        make(map[string]string),
	}
}

// Environment returns the environment variables
func (c *SessionProfileConfig) Environment() map[string]string { return c.environment }

// SetEnvironment sets the environment variables
func (c *SessionProfileConfig) SetEnvironment(env map[string]string) { c.environment = env }

// Tags returns the tags
func (c *SessionProfileConfig) Tags() map[string]string { return c.tags }

// SetTags sets the tags
func (c *SessionProfileConfig) SetTags(tags map[string]string) { c.tags = tags }

// Pool returns the logical session runner pool selected by this profile.
func (c *SessionProfileConfig) Pool() string { return c.pool }

// SetPool sets the logical session runner pool selected by this profile.
func (c *SessionProfileConfig) SetPool(pool string) { c.pool = pool }

// InitialMessageTemplate returns the initial message template
func (c *SessionProfileConfig) InitialMessageTemplate() string { return c.initialMessageTemplate }

// SetInitialMessageTemplate sets the initial message template
func (c *SessionProfileConfig) SetInitialMessageTemplate(t string) { c.initialMessageTemplate = t }

// ReuseMessageTemplate returns the reuse message template
func (c *SessionProfileConfig) ReuseMessageTemplate() string { return c.reuseMessageTemplate }

// SetReuseMessageTemplate sets the reuse message template
func (c *SessionProfileConfig) SetReuseMessageTemplate(t string) { c.reuseMessageTemplate = t }

// CommandWrapperTemplate returns the Go template used to wrap the final agent command.
func (c *SessionProfileConfig) CommandWrapperTemplate() string { return c.commandWrapperTemplate }

// SetCommandWrapperTemplate sets the Go template used to wrap the final agent command.
func (c *SessionProfileConfig) SetCommandWrapperTemplate(value string) {
	c.commandWrapperTemplate = value
}

// SystemPrompt returns the agent instructions injected for sessions using this profile.
func (c *SessionProfileConfig) SystemPrompt() string { return c.systemPrompt }

// SetSystemPrompt sets the agent instructions injected for sessions using this profile.
func (c *SessionProfileConfig) SetSystemPrompt(value string) { c.systemPrompt = value }

// SystemPromptMode returns how this profile combines its prompt with an inherited prompt.
func (c *SessionProfileConfig) SystemPromptMode() string { return c.systemPromptMode }

// SetSystemPromptMode sets how this profile combines its prompt with an inherited prompt.
func (c *SessionProfileConfig) SetSystemPromptMode(value string) { c.systemPromptMode = value }

// Params returns the session params
func (c *SessionProfileConfig) Params() *SessionParams { return c.params }

// SetParams sets the session params
func (c *SessionProfileConfig) SetParams(p *SessionParams) { c.params = p }

// ReuseSession returns whether to reuse an existing session
func (c *SessionProfileConfig) ReuseSession() bool { return c.reuseSession }

// SetReuseSession sets whether to reuse an existing session
