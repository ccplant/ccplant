package sessionsettings

import (
	"os"
	"os/exec"
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
	for _, expected := range []string{
		`[ "${1:-}" = "repo" ]`,
		`[ "${2:-}" = "clone" ]`,
		`repository="${3#https://*/}"`,
		`--data-urlencode "repository=$repository"`,
		`GH_TOKEN="$token" GH_ENTERPRISE_TOKEN="$token"`,
		`[GITHUB_BROKER] Invoking gh with broker credential`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("gh wrapper does not contain %q:\n%s", expected, content)
		}
	}
}

func TestConfigureGitHubTokenBrokerGHWrapperAuthenticatesGHES(t *testing.T) {
	homeDir := t.TempDir()
	fakeBin := t.TempDir()
	capturePath := filepath.Join(t.TempDir(), "gh-env")
	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	realGH := filepath.Join(fakeBin, "gh")
	if err := os.WriteFile(realGH, []byte("#!/bin/sh\nprintf '%s\\n%s\\n' \"$GH_TOKEN\" \"$GH_ENTERPRISE_TOKEN\" > \"$GH_CAPTURE\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "curl"), []byte("#!/bin/sh\nprintf '%s\\n' '{\"token\":\"broker-token\"}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	settings := &SessionSettings{Env: map[string]string{
		"AGENTAPI_GITHUB_BROKER_URL":   "https://broker.example.test/credentials",
		"AGENTAPI_GITHUB_BROKER_TOKEN": "lease-token",
		"GH_HOST":                      "git.example.test",
	}}
	if err := configureGitHubTokenBroker(settings); err != nil {
		t.Fatal(err)
	}

	wrapperPath := filepath.Join(homeDir, ".local", "bin", "gh")
	cmd := exec.Command(wrapperPath, "repo", "clone", "acme/widgets")
	cmd.Env = append(os.Environ(),
		"AGENTAPI_GITHUB_BROKER_URL=https://broker.example.test/credentials",
		"AGENTAPI_GITHUB_BROKER_TOKEN=lease-token",
		"GH_CAPTURE="+capturePath,
		"GH_HOST=git.example.test",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gh wrapper failed: %v: %s", err, output)
	}
	if got := string(output); !strings.Contains(got, "host=git.example.test repository=acme/widgets") {
		t.Fatalf("gh wrapper did not emit safe routing diagnostics: %s", got)
	}
	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(captured), "broker-token\nbroker-token\n"; got != want {
		t.Fatalf("gh authentication environment = %q, want %q", got, want)
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

func TestConfigureGitHubTokenBrokerUsesProvisionerSocketWithoutLeaseInAgentEnv(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	settings := &SessionSettings{Env: map[string]string{
		"AGENTAPI_GITHUB_BROKER_URL":    "http://localhost/github-credentials",
		"AGENTAPI_GITHUB_BROKER_SOCKET": "/tmp/ccplant/provisioner.sock",
	}}

	if err := configureGitHubTokenBroker(settings); err != nil {
		t.Fatal(err)
	}
	helper, err := os.ReadFile(filepath.Join(homeDir, ".local", "bin", "agentapi-github-credential"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(helper)
	if !strings.Contains(content, `--unix-socket "$AGENTAPI_GITHUB_BROKER_SOCKET"`) {
		t.Fatalf("credential helper does not use provisioner socket:\n%s", content)
	}
	if _, ok := settings.Env["AGENTAPI_GITHUB_BROKER_TOKEN"]; ok {
		t.Fatal("agent environment unexpectedly contains an upstream broker lease")
	}
}

func TestConfigureGitHubTokenBrokerRemovesDirectCredentials(t *testing.T) {
	directCredentials := []string{
		"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN",
		"GITHUB_PERSONAL_ACCESS_TOKEN", "GITHUB_APP_ID", "GITHUB_INSTALLATION_ID",
		"GITHUB_APP_PEM", "GITHUB_APP_PEM_PATH", "REPOSITORY_RESTRICTION",
	}
	settings := &SessionSettings{Env: map[string]string{
		"HOME":                         t.TempDir(),
		"AGENTAPI_GITHUB_BROKER_URL":   "https://broker.example.test/credentials",
		"AGENTAPI_GITHUB_BROKER_TOKEN": "lease-token",
		"GITHUB_API":                   "https://git.example.test/api/v3",
		"GITHUB_URL":                   "https://git.example.test",
		"GH_HOST":                      "git.example.test",
	}}
	for _, name := range directCredentials {
		settings.Env[name] = "stale-direct-credential"
		t.Setenv(name, "stale-direct-credential")
	}

	if err := configureGitHubTokenBroker(settings); err != nil {
		t.Fatal(err)
	}
	for _, name := range directCredentials {
		if _, ok := settings.Env[name]; ok {
			t.Errorf("direct credential %s remains in session settings", name)
		}
		if _, ok := os.LookupEnv(name); ok {
			t.Errorf("direct credential %s remains in process environment", name)
		}
	}
	for name, want := range map[string]string{
		"GITHUB_API": "https://git.example.test/api/v3",
		"GITHUB_URL": "https://git.example.test",
		"GH_HOST":    "git.example.test",
	} {
		if got := settings.Env[name]; got != want {
			t.Errorf("routing environment %s = %q, want %q", name, got, want)
		}
	}
}

func TestSetupSettingsConfiguresGitHubBrokerWithoutPreloadedRepositories(t *testing.T) {
	homeDir := t.TempDir()
	compileOpts := DefaultCompileOptions()
	compileOpts.OutputDir = homeDir
	compileOpts.EnvFilePath = filepath.Join(homeDir, ".session", "env")
	compileOpts.StartupPath = filepath.Join(homeDir, ".session", "startup.sh")
	settings := &SessionSettings{Env: map[string]string{
		"AGENTAPI_GITHUB_BROKER_URL":   "https://broker.example.test/credentials",
		"AGENTAPI_GITHUB_BROKER_TOKEN": "lease-token",
	}}
	if err := SetupSettings(settings, SetupOptions{CompileOptions: compileOpts}); err != nil {
		t.Fatal(err)
	}
	if settings.Env["HOME"] != homeDir {
		t.Fatalf("HOME = %q, want %q", settings.Env["HOME"], homeDir)
	}
	helperPath := filepath.Join(homeDir, ".local", "bin", "agentapi-github-credential")
	if _, err := os.Stat(helperPath); err != nil {
		t.Fatalf("credential helper was not written to compile output home: %v", err)
	}
	gitConfig, err := os.ReadFile(filepath.Join(homeDir, ".gitconfig"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gitConfig), helperPath) {
		t.Fatalf("git config does not reference helper in compile output home: %s", gitConfig)
	}
}

func TestSetupSettingsRejectsIncompleteGitHubBrokerConfiguration(t *testing.T) {
	settings := &SessionSettings{Env: map[string]string{
		"AGENTAPI_GITHUB_BROKER_URL": "https://broker.example.test/credentials",
	}}
	err := SetupSettings(settings, SetupOptions{CompileOptions: CompileOptions{OutputDir: t.TempDir()}})
	if err == nil || !strings.Contains(err.Error(), "broker URL and token must both be configured") {
		t.Fatalf("SetupSettings() error = %v, want incomplete broker configuration error", err)
	}
}

func TestSetupSettingsOverridesInheritedHomeWithCompileOutput(t *testing.T) {
	homeDir := t.TempDir()
	compileOpts := DefaultCompileOptions()
	compileOpts.OutputDir = homeDir
	compileOpts.EnvFilePath = filepath.Join(homeDir, ".session", "env")
	compileOpts.StartupPath = filepath.Join(homeDir, ".session", "startup.sh")
	settings := &SessionSettings{Env: map[string]string{"HOME": "/root"}}

	if err := SetupSettings(settings, SetupOptions{CompileOptions: compileOpts}); err != nil {
		t.Fatal(err)
	}
	if settings.Env["HOME"] != homeDir {
		t.Fatalf("HOME = %q, want compile output %q", settings.Env["HOME"], homeDir)
	}
}
