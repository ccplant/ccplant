package session

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

type recordingSessionManager struct {
	req             *entities.RunServerRequest
	existing        []entities.Session
	sendMessageErr  error
	stopAgentErr    error
	sendMessageID   string
	sendMessageBody string
	stopAgentID     string
	calls           []string
	createdID       string
}

func (m *recordingSessionManager) CreateSession(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	m.req = req
	m.createdID = id
	return &launchTestSession{id: id, userID: req.UserID, status: "active"}, nil
}

func (m *recordingSessionManager) GetSession(string) entities.Session { return nil }
func (m *recordingSessionManager) ListSessions(entities.SessionFilter) []entities.Session {
	return m.existing
}
func (m *recordingSessionManager) DeleteSession(string) error { return nil }
func (m *recordingSessionManager) SendMessage(_ context.Context, id string, message string) error {
	m.calls = append(m.calls, "send")
	m.sendMessageID = id
	m.sendMessageBody = message
	return m.sendMessageErr
}
func (m *recordingSessionManager) StopAgent(_ context.Context, id string) error {
	m.calls = append(m.calls, "stop")
	m.stopAgentID = id
	return m.stopAgentErr
}
func (m *recordingSessionManager) GetMessages(context.Context, string) ([]repositories.Message, error) {
	return nil, nil
}
func (m *recordingSessionManager) Shutdown(time.Duration) error { return nil }

type launchTestSession struct {
	id     string
	userID string
	status string
}

func (s *launchTestSession) ID() string                    { return s.id }
func (s *launchTestSession) Addr() string                  { return "" }
func (s *launchTestSession) UserID() string                { return s.userID }
func (s *launchTestSession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s *launchTestSession) TeamID() string                { return "" }
func (s *launchTestSession) Tags() map[string]string       { return nil }
func (s *launchTestSession) Status() string                { return s.status }
func (s *launchTestSession) StartedAt() time.Time          { return time.Time{} }
func (s *launchTestSession) UpdatedAt() time.Time          { return time.Time{} }
func (s *launchTestSession) LastMessageAt() time.Time      { return time.Time{} }
func (s *launchTestSession) Description() string           { return "" }
func (s *launchTestSession) Cancel()                       {}

type fakeSessionProfileRepo struct {
	profiles []*entities.SessionProfile
}

type fakeLaunchSettingsRepo struct{ settings *entities.Settings }

func (r *fakeLaunchSettingsRepo) Save(context.Context, *entities.Settings) error { return nil }
func (r *fakeLaunchSettingsRepo) FindByName(context.Context, string) (*entities.Settings, error) {
	return r.settings, nil
}
func (r *fakeLaunchSettingsRepo) Delete(context.Context, string) error { return nil }
func (r *fakeLaunchSettingsRepo) Exists(context.Context, string) (bool, error) {
	return r.settings != nil, nil
}
func (r *fakeLaunchSettingsRepo) List(context.Context) ([]*entities.Settings, error) { return nil, nil }

func (r *fakeSessionProfileRepo) Create(context.Context, *entities.SessionProfile) error {
	return nil
}
func (r *fakeSessionProfileRepo) Get(_ context.Context, id string) (*entities.SessionProfile, error) {
	for _, p := range r.profiles {
		if p.ID() == id {
			return p, nil
		}
	}
	return nil, entities.ErrSessionProfileNotFound{ID: id}
}
func (r *fakeSessionProfileRepo) List(_ context.Context, filter repositories.SessionProfileFilter) ([]*entities.SessionProfile, error) {
	var result []*entities.SessionProfile
	for _, p := range r.profiles {
		if filter.UserID != "" && p.UserID() != filter.UserID {
			continue
		}
		if filter.Scope != "" && p.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && p.TeamID() != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 && p.Scope() == entities.ScopeTeam {
			matched := false
			for _, teamID := range filter.TeamIDs {
				matched = matched || p.TeamID() == teamID
			}
			if !matched {
				continue
			}
		}
		result = append(result, p)
	}
	return result, nil
}
func (r *fakeSessionProfileRepo) Update(context.Context, *entities.SessionProfile) error {
	return nil
}
func (r *fakeSessionProfileRepo) Delete(context.Context, string) error { return nil }

func TestLaunchPropagatesTriggeredUserAndCredentialSource(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	launcher := NewLaunchUseCase(sessionManager)

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:           "webhook-owner",
		TriggeredUserID:  "github-actor",
		Scope:            entities.ScopeTeam,
		TeamID:           "org/team-a",
		Teams:            []string{"org/team-a"},
		CredentialSource: "triggered_user",
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got := sessionManager.req.TriggeredUserID; got != "github-actor" {
		t.Fatalf("TriggeredUserID = %q, want github-actor", got)
	}
	if got := sessionManager.req.CredentialSource; got != "triggered_user" {
		t.Fatalf("CredentialSource = %q, want triggered_user", got)
	}
	if got := sessionManager.req.UserID; got != "webhook-owner" {
		t.Fatalf("UserID = %q, want webhook-owner", got)
	}
}

func TestLaunchAppliesDefaultProfileDocker(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{
		Docker:     &entities.DockerParams{Enabled: true},
		SessionTTL: "48h",
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
	if sessionManager.req == nil {
		t.Fatal("CreateSession was not called")
	}
	if sessionManager.req.Docker == nil || !sessionManager.req.Docker.Enabled {
		t.Fatalf("expected profile Docker.Enabled to be applied, got %#v", sessionManager.req.Docker)
	}
	if sessionManager.req.SessionTTL != "48h" {
		t.Fatalf("expected profile SessionTTL to be applied, got %q", sessionManager.req.SessionTTL)
	}
}

func TestLaunchAppliesProfilePoolAsSessionParam(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "pooled", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetPool("native-linux")
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
	if got := sessionManager.req.Pool; got != "native-linux" {
		t.Fatalf("Pool = %q, want %q", got, "native-linux")
	}
}

func TestLaunchExplicitPoolSelectorOverridesProfilePool(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "pooled", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetPool("profile-pool")
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
		Pool:   "request-pool",
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got := sessionManager.req.Pool; got != "request-pool" {
		t.Fatalf("Pool = %q, want %q", got, "request-pool")
	}
}

func TestLaunchPropagatesProfileMCPServers(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "mcp", "user-1")
	profile.SetIsDefault(true)
	servers := entities.NewMCPServersSettings()
	github := entities.NewMCPServer("github", "http")
	github.SetURL("https://mcp.example.com/github")
	github.SetHeaders(map[string]string{"Authorization": "Bearer token"})
	servers.SetServer("github", github)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetMCPServers(servers)
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}},
	)
	if _, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1", Scope: entities.ScopeUser,
	}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req == nil || sessionManager.req.ProfileMCPServers == nil {
		t.Fatal("profile MCP servers were not propagated")
	}
	got := sessionManager.req.ProfileMCPServers.GetServer("github")
	if got == nil || got.URL() != "https://mcp.example.com/github" || got.Headers()["Authorization"] != "Bearer token" {
		t.Fatalf("unexpected MCP server: %#v", got)
	}
}

func TestLaunchPropagatesProfileSkills(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "skills", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetSkills([]string{"org/profile-skills"})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}},
	)
	if _, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1", Scope: entities.ScopeUser,
	}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got := sessionManager.req.ProfileSkills; len(got) != 1 || got[0] != "org/profile-skills" {
		t.Fatalf("ProfileSkills = %#v", got)
	}
}

func TestLaunchMaterializesPiProfilePackagesAndConnection(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "pi", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetPiPackages([]string{"git:github.com/org/pi-harness@v1"})
	cfg.SetPiConnection(&modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://gateway.example/v1", Model: "custom-model", Authentication: "api_key", APIKey: "secret"})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})
	if _, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{UserID: "user-1", Scope: entities.ScopeUser, AgentType: "pi"}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	env := sessionManager.req.ProfileEnvironment
	if env["CCPLANT_PI_PACKAGES"] != "git:github.com/org/pi-harness@v1" || len(env) != 1 {
		t.Fatalf("unexpected Pi profile environment: %#v", env)
	}
}

func TestLaunchPropagatesProfileSecretReferences(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "secrets", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetSecretIDs([]string{"secret-a", "secret-b"})
	cfg.SetSettingsTeamID("org/team-a")
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}},
	)
	if _, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{UserID: "user-1", Scope: entities.ScopeUser}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got := sessionManager.req.ProfileSecretIDs; len(got) != 2 || got[0] != "secret-a" || got[1] != "secret-b" {
		t.Fatalf("ProfileSecretIDs = %#v", got)
	}
	if got := sessionManager.req.SettingsTeamID; got != "org/team-a" {
		t.Fatalf("SettingsTeamID = %q, want org/team-a", got)
	}
}

func TestLaunchKeepsProfileEnvironmentSeparateFromExplicitEnvironment(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "environment", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetEnvironment(map[string]string{"SHARED": "profile", "PROFILE_ONLY": "profile-value"})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}},
	)
	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1", Scope: entities.ScopeUser,
		Environment: map[string]string{"SHARED": "request"},
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got := sessionManager.req.ProfileEnvironment["SHARED"]; got != "profile" {
		t.Fatalf("profile environment SHARED = %q, want profile", got)
	}
	if got := sessionManager.req.Environment["SHARED"]; got != "request" {
		t.Fatalf("explicit environment SHARED = %q, want request", got)
	}
}

func TestLaunchRejectsProfileOutsideLaunchTenant(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-other", "other", "other-user")
	cfg := entities.NewSessionProfileConfig()
	cfg.SetEnvironment(map[string]string{"SECRET": "other-user-value"})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).WithSessionProfileRepository(
		&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}},
	)
	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:           "user-1",
		Scope:            entities.ScopeUser,
		SessionProfileID: "profile-other",
	})
	if err == nil {
		t.Fatal("expected access denied error")
	}
	if sessionManager.req != nil {
		t.Fatal("profile environment must not leak to another user")
	}
}
