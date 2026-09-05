package sessionsettings

import "github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"

// ApplyModelConnections runs after all legacy environment layers have been merged.
// Its persisted unset list prevents inherited credentials from reappearing at launch.
func (s *SessionSettings) ApplyModelConnections() {
	if s.Env == nil {
		s.Env = map[string]string{}
	}
	c := s.CodexConnection
	agent := "codex"
	if c == nil {
		c = s.ClaudeConnection
		agent = "claude"
	}
	if c == nil {
		return
	}
	keys := modelprovider.ConnectionEnvKeys(agent)
	// OAuth/Bedrock use the existing materializer; only remove the gateway fields.
	if agent == "claude" && !c.Compatible() {
		keys = []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_CUSTOM_HEADERS", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL"}
		if c.Mode == "bedrock" {
			keys = append(keys, "CLAUDE_CODE_OAUTH_TOKEN")
			s.Env["CLAUDE_CODE_USE_BEDROCK"] = "1"
		} else {
			s.Env["CLAUDE_CODE_USE_BEDROCK"] = "0"
		}
	}
	for _, k := range keys {
		delete(s.Env, k)
		s.UnsetEnv = append(s.UnsetEnv, k)
	}
	if !c.Compatible() {
		return
	}
	if agent == "codex" {
		if c.Authentication != "none" {
			s.Env["CCPLANT_CODEX_API_KEY"] = c.APIKey
		}
	} else {
		s.Env["ANTHROPIC_BASE_URL"] = c.BaseURL
		s.Env["ANTHROPIC_MODEL"] = c.Model
		if c.Authentication == "api_key" {
			s.Env["ANTHROPIC_API_KEY"] = c.APIKey
		} else {
			s.Env["ANTHROPIC_AUTH_TOKEN"] = c.APIKey
		}
		for alias, key := range map[string]string{"sonnet": "ANTHROPIC_DEFAULT_SONNET_MODEL", "opus": "ANTHROPIC_DEFAULT_OPUS_MODEL", "haiku": "ANTHROPIC_DEFAULT_HAIKU_MODEL"} {
			model := c.Model
			if c.ModelAliases[alias] != "" {
				model = c.ModelAliases[alias]
			}
			s.Env[key] = model
		}
	}
	// Both account credential files are unused for an explicitly managed API session.
	s.RemoveFiles = append(s.RemoveFiles, ManagedFileTypes[FileTypeCodexAuth], ManagedFileTypes[FileTypeClaudeCredentials])
	s.UnsyncedFilePaths = append(s.UnsyncedFilePaths, s.RemoveFiles...)
	files := s.Files[:0]
	for _, f := range s.Files {
		blocked := false
		for _, p := range s.RemoveFiles {
			if f.Path == p {
				blocked = true
			}
		}
		if !blocked {
			files = append(files, f)
		}
	}
	s.Files = files
	s.Credentials = ""
	// Drop the other agent's inherited account variables as well.
	if agent == "codex" {
		for _, k := range modelprovider.ConnectionEnvKeys("claude") {
			delete(s.Env, k)
			s.UnsetEnv = append(s.UnsetEnv, k)
		}
	} else {
		delete(s.Env, "CCPLANT_CODEX_API_KEY")
		s.UnsetEnv = append(s.UnsetEnv, "CCPLANT_CODEX_API_KEY")
	}
}
