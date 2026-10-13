package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/buildinfo"
)

func TestVersionCommandJSON(t *testing.T) {
	originalVersion := buildinfo.Version
	originalDistribution := buildinfo.Distribution
	originalJSON := updateJSON
	t.Cleanup(func() {
		buildinfo.Version = originalVersion
		buildinfo.Distribution = originalDistribution
		updateJSON = originalJSON
	})
	buildinfo.Version = "v1.2.3"
	buildinfo.Distribution = "github-release"
	updateJSON = true
	var output bytes.Buffer
	VersionCmd.SetOut(&output)
	if err := VersionCmd.RunE(VersionCmd, nil); err != nil {
		t.Fatal(err)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["version"] != "v1.2.3" || response["distribution"] != "github-release" {
		t.Fatalf("unexpected version output: %v", response)
	}
}

func TestAutomaticUpdateCommandAllowlist(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"ccplant doctor", true},
		{"ccplant client user list", true},
		{"ccplant client consume-secret", false},
		{"ccplant client cycle", false},
		{"ccplant server", false},
		{"ccplant acp-server", false},
		{"ccplant update apply", false},
	}
	for _, test := range tests {
		root := &cobra.Command{Use: "ccplant"}
		parent := root
		parts := splitCommandPath(test.path)[1:]
		for _, part := range parts {
			child := &cobra.Command{Use: part}
			parent.AddCommand(child)
			parent = child
		}
		if got := automaticUpdateAllowed(parent); got != test.want {
			t.Errorf("automaticUpdateAllowed(%q) = %t, want %t", test.path, got, test.want)
		}
	}
}

func splitCommandPath(path string) []string {
	var parts []string
	for _, part := range bytes.Fields([]byte(path)) {
		parts = append(parts, string(part))
	}
	return parts
}
