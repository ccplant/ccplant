package modelprovider

// ConnectionEnvKeys deliberately excludes model-only overrides.
func ConnectionEnvKeys(agent string) []string {
	if agent == "codex" {
		return []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "CCPLANT_CODEX_API_KEY"}
	}
	return []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_CUSTOM_HEADERS", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"}
}
