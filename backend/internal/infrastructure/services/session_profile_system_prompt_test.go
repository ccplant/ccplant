package services

import "testing"

func TestProfileSystemPromptFile(t *testing.T) {
	tests := []struct {
		agentType string
		wantPath  string
	}{
		{agentType: "claude-acp", wantPath: "/home/agentapi/.claude/CLAUDE.md"},
		{agentType: "codex-acp", wantPath: "/home/agentapi/.codex/AGENTS.md"},
		{agentType: "pi", wantPath: "/home/agentapi/.pi/agent/AGENTS.md"},
		{agentType: "cursor", wantPath: "/home/agentapi/workdir/repo/AGENTS.md"},
	}
	for _, tt := range tests {
		t.Run(tt.agentType, func(t *testing.T) {
			file := profileSystemPromptFile(tt.agentType, "follow profile instructions")
			if file == nil {
				t.Fatal("profileSystemPromptFile() = nil")
			}
			if file.Path != tt.wantPath {
				t.Fatalf("Path = %q, want %q", file.Path, tt.wantPath)
			}
			if file.Content != "follow profile instructions" || file.Permissions != "0644" {
				t.Fatalf("file = %#v", file)
			}
		})
	}
}

func TestProfileSystemPromptFileSkipsEmptyAndUnsupported(t *testing.T) {
	if file := profileSystemPromptFile("codex-acp", "  "); file != nil {
		t.Fatalf("empty prompt file = %#v", file)
	}
	if file := profileSystemPromptFile("unknown", "instructions"); file != nil {
		t.Fatalf("unsupported agent prompt file = %#v", file)
	}
}
