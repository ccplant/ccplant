package sessionsettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryCheckoutTarget(t *testing.T) {
	tests := []struct {
		name      string
		repo      *RepositoryConfig
		wantType  string
		wantValue string
	}{
		{
			name:      "none",
			repo:      &RepositoryConfig{},
			wantType:  "",
			wantValue: "",
		},
		{
			name: "branch",
			repo: &RepositoryConfig{
				Branch: " feature/test ",
			},
			wantType:  "branch",
			wantValue: "feature/test",
		},
		{
			name: "pr",
			repo: &RepositoryConfig{
				PR: " 123 ",
			},
			wantType:  "pr",
			wantValue: "123",
		},
		{
			name: "pr takes precedence over branch",
			repo: &RepositoryConfig{
				Branch: "feature/test",
				PR:     "123",
			},
			wantType:  "pr",
			wantValue: "123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotValue := repositoryCheckoutTarget(tt.repo)
			if gotType != tt.wantType || gotValue != tt.wantValue {
				t.Fatalf("repositoryCheckoutTarget() = (%q, %q), want (%q, %q)", gotType, gotValue, tt.wantType, tt.wantValue)
			}
		})
	}
}

func TestConfigureGitHubTokenBrokerGHWrapperRequestsCloneRepository(t *testing.T) {
	homeDir := t.TempDir()
	fakeBin := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	realGH := filepath.Join(fakeBin, "gh")
	if err := os.WriteFile(realGH, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	settings := &SessionSettings{Env: map[string]string{
		"AGENTAPI_GITHUB_BROKER_URL":   "https://broker.example.test/credentials",
		"AGENTAPI_GITHUB_BROKER_TOKEN": "lease-token",
	}}
	if err := configureGitHubTokenBroker(settings); err != nil {
		t.Fatal(err)
	}

	wrapperPath := filepath.Join(homeDir, ".local", "bin", "gh")
	wrapper, err := os.ReadFile(wrapperPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(wrapper)
	for _, expected := range []string{`[ "${1:-}" = "repo" ]`, `[ "${2:-}" = "clone" ]`, `repository="${3#https://*/}"`, `--data-urlencode "repository=$repository"`} {
		if !strings.Contains(content, expected) {
			t.Fatalf("gh wrapper does not contain %q:\n%s", expected, content)
		}
	}
}

func TestConfigureGitHubTokenBrokerUsesSessionHome(t *testing.T) {
	processHome := t.TempDir()
	sessionHome := t.TempDir()
	t.Setenv("HOME", processHome)

	settings := &SessionSettings{Env: map[string]string{
		"HOME":                         sessionHome,
		"AGENTAPI_GITHUB_BROKER_URL":   "https://broker.example.test/credentials",
		"AGENTAPI_GITHUB_BROKER_TOKEN": "lease-token",
	}}
	if err := configureGitHubTokenBroker(settings); err != nil {
		t.Fatal(err)
	}

	helperPath := filepath.Join(sessionHome, ".local", "bin", "agentapi-github-credential")
	if _, err := os.Stat(helperPath); err != nil {
		t.Fatalf("credential helper was not written to the session home: %v", err)
	}
	gitConfig, err := os.ReadFile(filepath.Join(sessionHome, ".gitconfig"))
	if err != nil {
		t.Fatalf("global git config was not written to the session home: %v", err)
	}
	if content := string(gitConfig); !strings.Contains(content, helperPath) || !strings.Contains(content, "useHttpPath = true") {
		t.Fatalf("session git config does not enable the broker helper:\n%s", content)
	}
	if _, err := os.Stat(filepath.Join(processHome, ".gitconfig")); !os.IsNotExist(err) {
		t.Fatalf("process home git config was unexpectedly modified: %v", err)
	}
}

func TestSetupSettingsConfiguresGitHubBrokerWithoutPreloadedRepositories(t *testing.T) {
	settings := &SessionSettings{Env: map[string]string{
		"AGENTAPI_GITHUB_BROKER_URL": "https://broker.example.test/credentials",
	}}
	err := SetupSettings(settings, SetupOptions{CompileOptions: CompileOptions{OutputDir: t.TempDir()}})
	if err == nil || !strings.Contains(err.Error(), "broker URL and token must both be configured") {
		t.Fatalf("SetupSettings() error = %v, want incomplete broker configuration error", err)
	}
}
