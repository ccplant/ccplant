package acp

import "testing"

func TestPickEnvVarAuthMethodGeminiAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEX_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "gemini-test-key")

	methods := []AuthMethod{
		{ID: "google-login"},
		{ID: "gemini-api-key", Type: "env_var"},
	}
	if got := pickEnvVarAuthMethod(methods); got != "gemini-api-key" {
		t.Fatalf("pickEnvVarAuthMethod() = %q, want %q", got, "gemini-api-key")
	}
}

func TestPickEnvVarAuthMethodAcceptsAntigravityTerminalMethod(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEX_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "gemini-test-key")

	methods := []AuthMethod{{ID: "gemini-api-key", Type: "terminal"}}
	if got := pickEnvVarAuthMethod(methods); got != "gemini-api-key" {
		t.Fatalf("pickEnvVarAuthMethod() = %q, want %q", got, "gemini-api-key")
	}
}
