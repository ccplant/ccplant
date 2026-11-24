package session

import (
	"context"
	"reflect"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestLaunchAppliesDefaultProfileUnsyncedFilePaths(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetUnsyncedFilePaths([]string{"/home/agentapi/.codex/auth.json"})
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
	want := []string{"/home/agentapi/.codex/auth.json"}
	if !reflect.DeepEqual(sessionManager.req.UnsyncedFilePaths, want) {
		t.Fatalf("expected unsynced file paths %#v, got %#v", want, sessionManager.req.UnsyncedFilePaths)
	}
}

func TestLaunchAppliesProfileCredentialSource(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "team profile", "user-1")
	profile.SetOwnership(entities.ScopeTeam, "user-1", "org/team-a")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{CredentialSource: "session_user"})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeTeam,
		TeamID: "org/team-a",
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got := sessionManager.req.CredentialSource; got != "session_user" {
		t.Fatalf("credential source = %q, want session_user", got)
	}
}

func TestLaunchExplicitUnsyncedFilePathsOverrideProfile(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetUnsyncedFilePaths([]string{"/home/agentapi/.codex/auth.json"})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:            "user-1",
		Scope:             entities.ScopeUser,
		UnsyncedFilePaths: []string{"/home/agentapi/.claude/.credentials.json"},
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	want := []string{"/home/agentapi/.claude/.credentials.json"}
	if !reflect.DeepEqual(sessionManager.req.UnsyncedFilePaths, want) {
		t.Fatalf("expected explicit unsynced file paths %#v, got %#v", want, sessionManager.req.UnsyncedFilePaths)
	}
}

func TestLaunchExplicitDockerOverridesProfileDocker(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{
		Docker: &entities.DockerParams{Enabled: true},
	})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
		Docker: &entities.DockerParams{Enabled: false},
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req == nil || sessionManager.req.Docker == nil {
		t.Fatalf("expected explicit Docker params, got %#v", sessionManager.req)
	}
	if sessionManager.req.Docker.Enabled {
		t.Fatalf("expected explicit Docker.Enabled=false to override profile, got %#v", sessionManager.req.Docker)
	}
}

func TestLaunchAppliesDefaultProfileAuthProxy(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{
		AuthProxy: boolPointer(true),
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
	if sessionManager.req == nil || sessionManager.req.AuthProxy == nil || !*sessionManager.req.AuthProxy {
		t.Fatalf("expected profile AuthProxy=true to be applied, got %#v", sessionManager.req)
	}
}

func TestLaunchExplicitAuthProxyOverridesProfileAuthProxy(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{
		AuthProxy: boolPointer(true),
	})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:    "user-1",
		Scope:     entities.ScopeUser,
		AuthProxy: boolPointer(false),
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req == nil || sessionManager.req.AuthProxy == nil || *sessionManager.req.AuthProxy {
		t.Fatalf("expected explicit AuthProxy=false to override profile, got %#v", sessionManager.req)
	}
}

func TestLaunchAppliesProfileSelectedByTags(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	defaultProfile := entities.NewSessionProfile("profile-default", "default", "user-1")
	defaultProfile.SetIsDefault(true)
	defaultCfg := entities.NewSessionProfileConfig()
	defaultCfg.SetParams(&entities.SessionParams{AgentType: "claude"})
	defaultProfile.SetConfig(defaultCfg)

	selectedProfile := entities.NewSessionProfile("profile-selected", "selected", "user-1")
	selectedProfile.SetSelectorTags(map[string]string{"env": "dev"})
	selectedCfg := entities.NewSessionProfileConfig()
	selectedCfg.SetParams(&entities.SessionParams{AgentType: "codex"})
	selectedCfg.SetTags(map[string]string{"profile": "selected", "env": "profile"})
	selectedProfile.SetConfig(selectedCfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{
			defaultProfile,
			selectedProfile,
		}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
		Tags:   map[string]string{"env": "dev", "request": "kept"},
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req.AgentType != "codex" {
		t.Fatalf("expected tag-selected profile agent type, got %q", sessionManager.req.AgentType)
	}
	if sessionManager.req.Tags["env"] != "dev" || sessionManager.req.Tags["profile"] != "selected" || sessionManager.req.Tags["request"] != "kept" {
		t.Fatalf("unexpected merged tags: %#v", sessionManager.req.Tags)
	}
}

func TestLaunchExplicitProfileIDOverridesTagSelector(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	explicitProfile := entities.NewSessionProfile("profile-explicit", "explicit", "user-1")
	explicitCfg := entities.NewSessionProfileConfig()
	explicitCfg.SetParams(&entities.SessionParams{AgentType: "claude"})
	explicitProfile.SetConfig(explicitCfg)

	selectedProfile := entities.NewSessionProfile("profile-selected", "selected", "user-1")
	selectedProfile.SetSelectorTags(map[string]string{"env": "dev"})
	selectedCfg := entities.NewSessionProfileConfig()
	selectedCfg.SetParams(&entities.SessionParams{AgentType: "codex"})
	selectedProfile.SetConfig(selectedCfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{
			explicitProfile,
			selectedProfile,
		}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:           "user-1",
		Scope:            entities.ScopeUser,
		Tags:             map[string]string{"env": "dev"},
		SessionProfileID: "profile-explicit",
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req.AgentType != "claude" {
		t.Fatalf("expected explicit profile agent type, got %q", sessionManager.req.AgentType)
	}
}

func TestLaunchChoosesMostSpecificTagSelectedProfile(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	genericProfile := entities.NewSessionProfile("profile-generic", "generic", "user-1")
	genericProfile.SetSelectorTags(map[string]string{"env": "dev"})
	genericCfg := entities.NewSessionProfileConfig()
	genericCfg.SetParams(&entities.SessionParams{AgentType: "claude"})
	genericProfile.SetConfig(genericCfg)

	specificProfile := entities.NewSessionProfile("profile-specific", "specific", "user-1")
	specificProfile.SetSelectorTags(map[string]string{"env": "dev", "repo": "owner/repo"})
	specificCfg := entities.NewSessionProfileConfig()
	specificCfg.SetParams(&entities.SessionParams{AgentType: "codex"})
	specificProfile.SetConfig(specificCfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{
			genericProfile,
			specificProfile,
		}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
		Tags:   map[string]string{"env": "dev", "repo": "owner/repo"},
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req.AgentType != "codex" {
		t.Fatalf("expected most specific selected profile, got %q", sessionManager.req.AgentType)
	}
}

func TestLaunchSelectsProfileByRepositoryInfo(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	defaultProfile := entities.NewSessionProfile("profile-default", "default", "user-1")
	defaultProfile.SetIsDefault(true)
	defaultCfg := entities.NewSessionProfileConfig()
	defaultCfg.SetParams(&entities.SessionParams{AgentType: "claude"})
	defaultProfile.SetConfig(defaultCfg)

	repositoryProfile := entities.NewSessionProfile("profile-repository", "repository", "user-1")
	repositoryProfile.SetSelectorTags(map[string]string{"repository": "owner/repo"})
	repositoryCfg := entities.NewSessionProfileConfig()
	repositoryCfg.SetParams(&entities.SessionParams{AgentType: "codex"})
	repositoryProfile.SetConfig(repositoryCfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{
			defaultProfile,
			repositoryProfile,
		}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
		RepoInfo: &entities.RepositoryInfo{
			FullName: "owner/repo",
			CloneDir: "session-1",
		},
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req.AgentType != "codex" {
		t.Fatalf("expected repository-selected profile agent type, got %q", sessionManager.req.AgentType)
	}
}

func TestLaunchReuseRoutesMessageToExistingSession(t *testing.T) {
	sessionManager := &recordingSessionManager{
		existing: []entities.Session{&launchTestSession{id: "existing-1", userID: "user-1", status: "active"}},
	}
	launcher := NewLaunchUseCase(sessionManager)

	result, err := launcher.Launch(context.Background(), "new-1", LaunchRequest{
		UserID:         "user-1",
		Scope:          entities.ScopeUser,
		InitialMessage: "initial",
		ReuseSession:   true,
		ReuseMatchTags: map[string]string{"webhook_id": "webhook-1"},
		ReuseMessage:   "reuse",
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !result.SessionReused || result.SessionID != "existing-1" {
		t.Fatalf("expected reused existing session, got %#v", result)
	}
	if sessionManager.req != nil {
		t.Fatal("CreateSession should not be called when reuse succeeds")
	}
	if sessionManager.sendMessageID != "existing-1" || sessionManager.sendMessageBody != "reuse" {
		t.Fatalf("unexpected SendMessage call: id=%q body=%q", sessionManager.sendMessageID, sessionManager.sendMessageBody)
	}
}

func TestLaunchReuseStopsExistingSessionBeforeSendingWhenRequested(t *testing.T) {
	sessionManager := &recordingSessionManager{
		existing: []entities.Session{&launchTestSession{id: "existing-1", userID: "user-1", status: "active"}},
	}
	launcher := NewLaunchUseCase(sessionManager)

	result, err := launcher.Launch(context.Background(), "new-1", LaunchRequest{
		UserID:          "user-1",
		Scope:           entities.ScopeUser,
		InitialMessage:  "initial",
		ReuseSession:    true,
		ReuseMatchTags:  map[string]string{"webhook_id": "webhook-1"},
		ReuseMessage:    "reuse",
		StopBeforeReuse: true,
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !result.SessionReused || result.SessionID != "existing-1" {
		t.Fatalf("expected reused existing session, got %#v", result)
	}
	if sessionManager.req != nil {
		t.Fatal("CreateSession should not be called when reuse succeeds")
	}
	if sessionManager.stopAgentID != "existing-1" {
		t.Fatalf("unexpected StopAgent id: %q", sessionManager.stopAgentID)
	}
	if sessionManager.sendMessageID != "existing-1" || sessionManager.sendMessageBody != "reuse" {
		t.Fatalf("unexpected SendMessage call: id=%q body=%q", sessionManager.sendMessageID, sessionManager.sendMessageBody)
	}
	if !reflect.DeepEqual(sessionManager.calls, []string{"stop", "send"}) {
		t.Fatalf("unexpected call order: %#v", sessionManager.calls)
	}
}
