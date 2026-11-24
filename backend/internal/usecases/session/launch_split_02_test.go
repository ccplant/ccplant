package session

import (
	"context"
	"reflect"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestLaunchAllowsTeamProfileForPersonalSessionWhenUserBelongsToTeam(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-team", "shared", "owner")
	profile.SetScope(entities.ScopeTeam)
	profile.SetTeamID("acme/platform")
	cfg := entities.NewSessionProfileConfig()
	cfg.SetEnvironment(map[string]string{"SHARED": "team-value"})
	profile.SetConfig(cfg)

	settings := entities.NewSettings("user-1")
	settings.SetEnabledTeamSessionProfileIDs([]string{"profile-team"})
	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}}).
		WithSettingsRepository(&fakeLaunchSettingsRepo{settings: settings})
	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1", Scope: entities.ScopeUser, Teams: []string{"acme/platform"},
		SessionProfileID: "profile-team",
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got := sessionManager.req.ProfileEnvironment["SHARED"]; got != "team-value" {
		t.Fatalf("profile environment = %q, want team-value", got)
	}
	if got := sessionManager.req.SettingsTeamID; got != "acme/platform" {
		t.Fatalf("SettingsTeamID = %q, want acme/platform", got)
	}
}

func TestLaunchRejectsTeamProfileForPersonalSessionOutsideUserTeams(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-team", "shared", "owner")
	profile.SetScope(entities.ScopeTeam)
	profile.SetTeamID("other/team")

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}},
	)
	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1", Scope: entities.ScopeUser, Teams: []string{"acme/platform"},
		SessionProfileID: "profile-team",
	})
	if err == nil {
		t.Fatal("expected access denied error")
	}
	if sessionManager.req != nil {
		t.Fatal("team profile must not be applied outside the user's teams")
	}
}

func TestLaunchRejectsTeamProfileForPersonalSessionWithoutOptIn(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-team", "shared", "owner")
	profile.SetScope(entities.ScopeTeam)
	profile.SetTeamID("acme/platform")
	settings := entities.NewSettings("user-1")
	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}}).
		WithSettingsRepository(&fakeLaunchSettingsRepo{settings: settings})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1", Scope: entities.ScopeUser, Teams: []string{"acme/platform"},
		SessionProfileID: "profile-team",
	})
	if err == nil {
		t.Fatal("expected access denied error")
	}
}

func TestLaunchPropagatesProfileFiles(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "files", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetProfileFiles([]entities.ProfileFile{{
		Name:        "SSH key",
		Path:        "/home/agentapi/.ssh/id_ed25519",
		Content:     "private-key",
		Permissions: "0600",
	}})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}},
	)
	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1", Scope: entities.ScopeUser,
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	want := []sessionsettings.ManagedFile{{
		Path:        "/home/agentapi/.ssh/id_ed25519",
		Content:     "private-key",
		Permissions: "0600",
	}}
	if !reflect.DeepEqual(sessionManager.req.ProfileFiles, want) {
		t.Fatalf("ProfileFiles = %#v, want %#v", sessionManager.req.ProfileFiles, want)
	}
}

func TestLaunchPropagatesProfileRepositories(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "repositories", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetProfileRepositories([]entities.ProfileRepository{{
		Repository: "acme/shared", Path: "/home/agentapi/workdir/shared", Branch: "develop",
	}})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}},
	)
	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{UserID: "user-1", Scope: entities.ScopeUser})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	want := []sessionsettings.RepositoryConfig{{FullName: "acme/shared", CloneDir: "/home/agentapi/workdir/shared", Branch: "develop"}}
	if !reflect.DeepEqual(sessionManager.req.ProfileRepositories, want) {
		t.Fatalf("ProfileRepositories = %#v, want %#v", sessionManager.req.ProfileRepositories, want)
	}
}

func TestLaunchPropagatesProfileSystemPrompt(t *testing.T) {
	profile := entities.NewSessionProfile("profile-1", "instructions", "user-1")
	cfg := entities.NewSessionProfileConfig()
	cfg.SetSystemPrompt("# Profile instructions")
	profile.SetConfig(cfg)

	sessionManager := &recordingSessionManager{}
	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}},
	)
	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:           "user-1",
		SessionProfileID: "profile-1",
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got := sessionManager.req.ProfileSystemPrompt; got != "# Profile instructions" {
		t.Fatalf("ProfileSystemPrompt = %q", got)
	}
}

func TestLaunchResolvesProfileSourceEnvironmentAndMCPServers(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	source := entities.NewSessionProfile("profile-source", "source", "user-1")
	sourceCfg := entities.NewSessionProfileConfig()
	sourceCfg.SetEnvironment(map[string]string{
		"SHARED":      "source",
		"SOURCE_ONLY": "source-value",
	})
	sourceCfg.SetSkills([]string{"org/source-skills", "org/shared"})
	sourceCfg.SetCommandWrapperTemplate("exec env SOURCE=1 {{ .Command }}")
	sourceCfg.SetSystemPrompt("# Base instructions")
	github := entities.NewMCPServer("github", "http")
	github.SetURL("https://source.example/github")
	servers := entities.NewMCPServersSettings()
	servers.SetServer("github", github)
	sourceCfg.SetMCPServers(servers)
	source.SetConfig(sourceCfg)

	profile := entities.NewSessionProfile("profile-main", "main", "user-1")
	profileCfg := entities.NewSessionProfileConfig()
	profileCfg.SetSourceSessionProfileID("profile-source")
	profileCfg.SetEnvironment(map[string]string{
		"SHARED":       "profile",
		"PROFILE_ONLY": "profile-value",
	})
	profileCfg.SetSkills([]string{"org/shared", "org/profile-skills"})
	profileCfg.SetSystemPrompt("# Profile instructions")
	profileCfg.SetSystemPromptMode("append")
	githubOverride := entities.NewMCPServer("github", "http")
	githubOverride.SetURL("https://profile.example/github")
	githubOverride.SetHeaders(map[string]string{"Authorization": "Bearer profile"})
	profileServers := entities.NewMCPServersSettings()
	profileServers.SetServer("github", githubOverride)
	profileCfg.SetMCPServers(profileServers)
	profile.SetConfig(profileCfg)

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile, source}},
	)
	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:           "user-1",
		Scope:            entities.ScopeUser,
		SessionProfileID: "profile-main",
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	for key, want := range map[string]string{
		"SHARED":       "profile",
		"SOURCE_ONLY":  "source-value",
		"PROFILE_ONLY": "profile-value",
	} {
		if got := sessionManager.req.ProfileEnvironment[key]; got != want {
			t.Errorf("ProfileEnvironment[%s] = %q, want %q", key, got, want)
		}
	}
	got := sessionManager.req.ProfileMCPServers.GetServer("github")
	if got == nil || got.URL() != "https://profile.example/github" || got.Headers()["Authorization"] != "Bearer profile" {
		t.Fatalf("profile MCP override was not applied: %#v", got)
	}
	wantSkills := []string{"org/source-skills", "org/shared", "org/profile-skills"}
	if !reflect.DeepEqual(sessionManager.req.ProfileSkills, wantSkills) {
		t.Fatalf("ProfileSkills = %#v, want %#v", sessionManager.req.ProfileSkills, wantSkills)
	}
	if sessionManager.req.ProfileCommandWrapper != "exec env SOURCE=1 {{ .Command }}" {
		t.Fatalf("ProfileCommandWrapper = %q", sessionManager.req.ProfileCommandWrapper)
	}
	if got := sessionManager.req.ProfileSystemPrompt; got != "# Base instructions\n\n# Profile instructions" {
		t.Fatalf("ProfileSystemPrompt = %q", got)
	}
}

func TestResolveEffectiveSessionProfileConfigOverridesSourceSystemPromptByDefault(t *testing.T) {
	source := entities.NewSessionProfile("source", "source", "user-1")
	sourceCfg := entities.NewSessionProfileConfig()
	sourceCfg.SetSystemPrompt("base")
	source.SetConfig(sourceCfg)
	profile := entities.NewSessionProfile("profile", "profile", "user-1")
	profileCfg := entities.NewSessionProfileConfig()
	profileCfg.SetSourceSessionProfileID("source")
	profileCfg.SetSystemPrompt("override")
	profile.SetConfig(profileCfg)

	got, err := ResolveEffectiveSessionProfileConfig(context.Background(), &fakeSessionProfileRepo{profiles: []*entities.SessionProfile{source, profile}}, profile, entities.ScopeUser, "user-1", "")
	if err != nil {
		t.Fatalf("ResolveEffectiveSessionProfileConfig() error = %v", err)
	}
	if got.SystemPrompt() != "override" {
		t.Fatalf("SystemPrompt = %q", got.SystemPrompt())
	}
}

func TestLaunchRejectsCyclicProfileSource(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	source := entities.NewSessionProfile("profile-source", "source", "user-1")
	sourceCfg := entities.NewSessionProfileConfig()
	sourceCfg.SetSourceSessionProfileID("profile-main")
	source.SetConfig(sourceCfg)

	profile := entities.NewSessionProfile("profile-main", "main", "user-1")
	profileCfg := entities.NewSessionProfileConfig()
	profileCfg.SetSourceSessionProfileID("profile-source")
	profile.SetConfig(profileCfg)

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile, source}},
	)
	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:           "user-1",
		Scope:            entities.ScopeUser,
		SessionProfileID: "profile-main",
	})
	if err == nil {
		t.Fatal("expected cyclic source error")
	}
	if sessionManager.req != nil {
		t.Fatal("cyclic profile source must not launch a session")
	}
}

func TestLaunchAppliesDefaultProfileSandbox(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{
		Sandbox: &entities.SandboxParams{Enabled: true, CountMode: true},
	})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req == nil || sessionManager.req.Sandbox == nil || !sessionManager.req.Sandbox.Enabled || !sessionManager.req.Sandbox.CountMode {
		t.Fatalf("expected profile sandbox params to be applied, got %#v", sessionManager.req)
	}
}

func TestLaunchAppliesCountModeSandboxWhenProfileHasNoPolicy(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	profile.SetConfig(entities.NewSessionProfileConfig())

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req == nil || sessionManager.req.Sandbox == nil {
		t.Fatalf("expected count mode sandbox params, got %#v", sessionManager.req)
	}
	if !sessionManager.req.Sandbox.Enabled || !sessionManager.req.Sandbox.CountMode || sessionManager.req.Sandbox.PolicyID != "" {
		t.Fatalf("expected count mode sandbox without policy, got %#v", sessionManager.req.Sandbox)
	}
}

func TestLaunchAppliesDefaultProfileSandboxPolicyID(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetSandboxPolicyID("policy-1")
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req == nil || sessionManager.req.Sandbox == nil {
		t.Fatalf("expected profile sandbox policy params, got %#v", sessionManager.req)
	}
	if !sessionManager.req.Sandbox.Enabled || sessionManager.req.Sandbox.PolicyID != "policy-1" {
		t.Fatalf("expected sandbox policy to enable sandbox, got %#v", sessionManager.req.Sandbox)
	}
}
