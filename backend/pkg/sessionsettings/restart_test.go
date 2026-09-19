package sessionsettings

import (
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"testing"
)

func TestRestartAllowsFullConfigurationAndCredentialReplacement(t *testing.T) {
	old := &SessionSettings{Session: SessionMeta{AgentType: "codex-acp"}, CodexConnection: &modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://example.test", Model: "model", APIKey: "old"}, Env: map[string]string{"OLD": "old"}}
	next := &SessionSettings{Session: old.Session, CodexConnection: old.CodexConnection.Clone(), Env: map[string]string{"NEW": "new"}, Docker: &DockerConfig{Enabled: true}}
	next.CodexConnection.APIKey = "new"
	if err := ValidateRestart(old, next); err != nil {
		t.Fatal(err)
	}
	next.CodexConnection.BaseURL = "https://different.test"
	if err := ValidateRestart(old, next); err == nil {
		t.Fatal("different provider accepted")
	}
	next.CodexConnection = old.CodexConnection.Clone()
	next.Session.AgentType = "claude-acp"
	if err := ValidateRestart(old, next); err == nil {
		t.Fatal("different agent accepted")
	}
}
func TestRestartRejectsUnsupportedAgentsAndRepositoryChanges(t *testing.T) {
	for _, agent := range []string{"cursor", "pi-ollama", ""} {
		s := &SessionSettings{Session: SessionMeta{AgentType: agent}}
		if ValidateRestart(s, s) == nil {
			t.Fatalf("accepted %q", agent)
		}
	}
	s := &SessionSettings{Session: SessionMeta{AgentType: "claude-acp"}}
	n := *s
	n.Repository = &RepositoryConfig{FullName: "org/other"}
	if ValidateRestart(s, &n) == nil {
		t.Fatal("repository change accepted")
	}
}

func TestRestartRejectsUnstructuredModelRoutingChanges(t *testing.T) {
	old := &SessionSettings{Session: SessionMeta{AgentType: "codex-acp"}, Codex: CodexConfig{ConfigTOML: `model = "original"`}}
	next := *old
	next.Codex.ConfigTOML = `model = "different"`
	if ValidateRestart(old, &next) == nil {
		t.Fatal("unstructured model change accepted")
	}
	next.Codex.ConfigTOML = "model = \"original\"\n[mcp_servers.new]\ncommand = \"tool\"\n"
	if err := ValidateRestart(old, &next); err != nil {
		t.Fatalf("MCP change rejected: %v", err)
	}
}
