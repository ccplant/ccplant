package entities

import (
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

func (c *SessionProfileConfig) SetReuseSession(reuse bool) { c.reuseSession = reuse }

// SandboxPolicyID returns the sandbox policy ID
func (c *SessionProfileConfig) SandboxPolicyID() string { return c.sandboxPolicyID }

// SetSandboxPolicyID sets the sandbox policy ID
func (c *SessionProfileConfig) SetSandboxPolicyID(id string) { c.sandboxPolicyID = id }

// SessionTTL returns the session TTL override for this profile
func (c *SessionProfileConfig) SessionTTL() string { return c.sessionTTL }

// SetSessionTTL sets the session TTL override for this profile
func (c *SessionProfileConfig) SetSessionTTL(ttl string) { c.sessionTTL = ttl }

// UnsyncedFilePaths returns file paths excluded from managed file sync.
func (c *SessionProfileConfig) UnsyncedFilePaths() []string {
	return copyStringSlice(c.unsyncedFilePaths)
}

// SetUnsyncedFilePaths sets file paths excluded from managed file sync.
func (c *SessionProfileConfig) SetUnsyncedFilePaths(paths []string) {
	c.unsyncedFilePaths = copyStringSlice(paths)
}

// SourceSessionProfileID returns the profile referenced for environment and MCP settings.
func (c *SessionProfileConfig) SourceSessionProfileID() string { return c.sourceProfileID }

// SetSourceSessionProfileID sets the profile referenced for environment and MCP settings.
func (c *SessionProfileConfig) SetSourceSessionProfileID(id string) { c.sourceProfileID = id }

// SecretIDs returns the settings Secret references selected by this profile.
func (c *SessionProfileConfig) SecretIDs() []string { return copyStringSlice(c.secretIDs) }

// SetSecretIDs sets the settings Secret references selected by this profile.
func (c *SessionProfileConfig) SetSecretIDs(ids []string) { c.secretIDs = copyStringSlice(ids) }

// GitHubRepositories returns additional repositories made available by this profile.
func (c *SessionProfileConfig) GitHubRepositories() []string {
	return copyStringSlice(c.githubRepositories)
}

// SetGitHubRepositories sets additional repositories made available by this profile.
func (c *SessionProfileConfig) SetGitHubRepositories(repositories []string) {
	c.githubRepositories = copyStringSlice(repositories)
}

// Skills returns skills.sh package sources contributed by this profile.
func (c *SessionProfileConfig) Skills() []string { return copyStringSlice(c.skills) }

// SetSkills sets skills.sh package sources contributed by this profile.
func (c *SessionProfileConfig) SetSkills(skills []string) { c.skills = copyStringSlice(skills) }

// PiPackages returns Pi packages installed before a Pi session starts.
func (c *SessionProfileConfig) PiPackages() []string { return copyStringSlice(c.piPackages) }

// SetPiPackages sets Pi package sources (for example git:github.com/org/pi-harness@v1).
func (c *SessionProfileConfig) SetPiPackages(packages []string) {
	c.piPackages = copyStringSlice(packages)
}

// ProfileFiles returns files managed by this profile.
func (c *SessionProfileConfig) ProfileFiles() []ProfileFile {
	files := make([]ProfileFile, len(c.files))
	copy(files, c.files)
	return files
}

// SetProfileFiles sets files managed by this profile.
func (c *SessionProfileConfig) SetProfileFiles(files []ProfileFile) {
	c.files = append([]ProfileFile(nil), files...)
}

// ProfileRepositories returns repositories preloaded by this profile.
func (c *SessionProfileConfig) ProfileRepositories() []ProfileRepository {
	return append([]ProfileRepository(nil), c.repositories...)
}

// SetProfileRepositories sets repositories preloaded by this profile.
func (c *SessionProfileConfig) SetProfileRepositories(repositories []ProfileRepository) {
	c.repositories = append([]ProfileRepository(nil), repositories...)
}

// MCPServers returns the MCP servers contributed by this profile.
func (c *SessionProfileConfig) MCPServers() *MCPServersSettings { return c.mcpServers }

// SetMCPServers sets the MCP servers contributed by this profile.
func (c *SessionProfileConfig) SetMCPServers(servers *MCPServersSettings) { c.mcpServers = servers }

// SessionState returns the Storage-backed session state selection.
func (c *SessionProfileConfig) SessionState() *SessionStateConfig {
	if c.sessionState == nil {
		return nil
	}
	value := *c.sessionState
	value.Components = copyStringSlice(value.Components)
	value.Agents = copyStringSlice(value.Agents)
	return &value
}

// SetSessionState sets the Storage-backed session state selection.
func (c *SessionProfileConfig) SetSessionState(value *SessionStateConfig) {
	if value == nil {
		c.sessionState = nil
		return
	}
	copyValue := *value
	copyValue.Components = copyStringSlice(value.Components)
	copyValue.Agents = copyStringSlice(value.Agents)
	c.sessionState = &copyValue
}

// --- Error types ---

// ErrInvalidSessionProfile represents a validation error for session profiles
type ErrInvalidSessionProfile struct {
	Field   string
	Message string
}

func (e ErrInvalidSessionProfile) Error() string {
	return "invalid session profile: " + e.Field + ": " + e.Message
}

// ErrSessionProfileNotFound is returned when a session profile is not found
type ErrSessionProfileNotFound struct {
	ID string
}

func (e ErrSessionProfileNotFound) Error() string {
	return "session profile not found: " + e.ID
}

func copyStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (c *SessionProfileConfig) CodexConnection() *modelprovider.Connection {
	return c.codexConnection.Clone()
}
func (c *SessionProfileConfig) ClaudeConnection() *modelprovider.Connection {
	return c.claudeConnection.Clone()
}
func (c *SessionProfileConfig) SetCodexConnection(v *modelprovider.Connection) {
	c.codexConnection = v.Clone()
}
func (c *SessionProfileConfig) SetClaudeConnection(v *modelprovider.Connection) {
	c.claudeConnection = v.Clone()
}
func (c *SessionProfileConfig) PiConnection() *modelprovider.Connection {
	return c.piConnection.Clone()
}
func (c *SessionProfileConfig) SetPiConnection(v *modelprovider.Connection) {
	c.piConnection = v.Clone()
}

func (c *SessionProfileConfig) SettingsTeamID() string      { return c.settingsTeamID }
func (c *SessionProfileConfig) SetSettingsTeamID(id string) { c.settingsTeamID = id }
