package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type webhookLaunchSessionManager struct {
	existing          []entities.Session
	createdRequest    *entities.RunServerRequest
	stopCalls         int
	sendMessageCalls  int
	listSessionsCalls int
}

func (m *webhookLaunchSessionManager) CreateSession(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	m.createdRequest = req
	return &webhookLaunchSession{id: id, userID: req.UserID, status: "creating"}, nil
}
func (m *webhookLaunchSessionManager) GetSession(string) entities.Session { return nil }
func (m *webhookLaunchSessionManager) ListSessions(entities.SessionFilter) []entities.Session {
	m.listSessionsCalls++
	return m.existing
}
func (m *webhookLaunchSessionManager) DeleteSession(string) error { return nil }
func (m *webhookLaunchSessionManager) SendMessage(context.Context, string, string) error {
	m.sendMessageCalls++
	return nil
}
func (m *webhookLaunchSessionManager) StopAgent(context.Context, string) error {
	m.stopCalls++
	return nil
}
func (m *webhookLaunchSessionManager) GetMessages(context.Context, string) ([]repositories.Message, error) {
	return nil, nil
}
func (m *webhookLaunchSessionManager) Shutdown(time.Duration) error { return nil }

type webhookLaunchSession struct {
	id     string
	userID string
	status string
}

func (s *webhookLaunchSession) ID() string                    { return s.id }
func (s *webhookLaunchSession) Addr() string                  { return "" }
func (s *webhookLaunchSession) UserID() string                { return s.userID }
func (s *webhookLaunchSession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s *webhookLaunchSession) TeamID() string                { return "" }
func (s *webhookLaunchSession) Tags() map[string]string       { return nil }
func (s *webhookLaunchSession) Status() string                { return s.status }
func (s *webhookLaunchSession) StartedAt() time.Time          { return time.Time{} }
func (s *webhookLaunchSession) UpdatedAt() time.Time          { return time.Time{} }
func (s *webhookLaunchSession) LastMessageAt() time.Time      { return time.Time{} }
func (s *webhookLaunchSession) Description() string           { return "" }
func (s *webhookLaunchSession) Cancel()                       {}

func TestCreateSessionFromWebhookDefersReuseToStartAPI(t *testing.T) {
	manager := &webhookLaunchSessionManager{
		existing: []entities.Session{&webhookLaunchSession{id: "existing", userID: "user-1", status: "running"}},
	}
	config := entities.NewWebhookSessionConfig()
	config.SetInitialMessageTemplate("initial")
	config.SetReuseMessageTemplate("reuse")
	config.SetReuseSession(true)
	webhook := entities.NewWebhook("webhook-1", "test", "user-1", entities.WebhookTypeCustom)
	webhook.SetSessionConfig(config)
	trigger := entities.NewWebhookTrigger("trigger-1", "test")

	service := NewWebhookSessionService(nil, manager, nil)
	_, _, err := service.CreateSessionFromWebhook(context.Background(), SessionCreationParams{
		Webhook: webhook,
		Trigger: &trigger,
		Payload: map[string]interface{}{},
		Tags:    map[string]string{"webhook_id": "webhook-1"},
	})
	if err != nil {
		t.Fatalf("CreateSessionFromWebhook() error = %v", err)
	}
	if manager.listSessionsCalls != 0 || manager.stopCalls != 0 || manager.sendMessageCalls != 0 {
		t.Fatalf("reuse ran in worker: list=%d stop=%d send=%d", manager.listSessionsCalls, manager.stopCalls, manager.sendMessageCalls)
	}
	if manager.createdRequest == nil {
		t.Fatal("CreateSession was not called")
	}
	if got := manager.createdRequest.ReuseMessage; got != "reuse" {
		t.Fatalf("ReuseMessage = %q, want reuse", got)
	}
	if !manager.createdRequest.StopBeforeReuse {
		t.Fatal("StopBeforeReuse = false, want true")
	}
	if got := manager.createdRequest.ReuseMatchTags["webhook_id"]; got != "webhook-1" {
		t.Fatalf("ReuseMatchTags[webhook_id] = %q, want webhook-1", got)
	}
}

func TestResolveTriggeredUsername(t *testing.T) {
	tests := []struct {
		name             string
		tags             map[string]string
		payload          map[string]interface{}
		credentialSource string
		want             string
	}{
		{
			name:             "uses github sender tag",
			tags:             map[string]string{"github_sender": " octocat ", "username": "legacy-user"},
			payload:          map[string]interface{}{},
			credentialSource: "github_sender",
			want:             "octocat",
		},
		{
			name:             "uses explicit username tag",
			tags:             map[string]string{"username": " configured-user "},
			payload:          map[string]interface{}{"user": map[string]interface{}{"login": "payload-user"}},
			credentialSource: "triggered_user",
			want:             "configured-user",
		},
		{
			name: "uses github sender login as the triggering user",
			tags: map[string]string{},
			payload: map[string]interface{}{
				"sender": map[string]interface{}{"login": " github-sender "},
				"user":   map[string]interface{}{"login": "payload-user"},
			},
			credentialSource: "triggered_user",
			want:             "github-sender",
		},
		{
			name:             "defaults to payload user login for triggered user credentials",
			tags:             map[string]string{},
			payload:          map[string]interface{}{"user": map[string]interface{}{"login": " github-user "}},
			credentialSource: "triggered_user",
			want:             "github-user",
		},
		{
			name:             "does not infer user for other credential sources",
			tags:             map[string]string{},
			payload:          map[string]interface{}{"user": map[string]interface{}{"login": "github-user"}},
			credentialSource: "team",
			want:             "",
		},
		{
			name:             "missing user login remains unresolved",
			tags:             map[string]string{},
			payload:          map[string]interface{}{},
			credentialSource: "triggered_user",
			want:             "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveTriggeredUsername(tt.tags, tt.payload, tt.credentialSource); got != tt.want {
				t.Fatalf("resolveTriggeredUsername() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveCredentialSource(t *testing.T) {
	params := &entities.SessionParams{CredentialSource: "team"}

	if got := resolveCredentialSource(map[string]string{"credential_source": " github_sender "}, params); got != "github_sender" {
		t.Fatalf("resolveCredentialSource() = %q, want github_sender", got)
	}
	if got := resolveCredentialSource(map[string]string{}, params); got != "team" {
		t.Fatalf("resolveCredentialSource() fallback = %q, want team", got)
	}
}

func TestApplyTriggeredUsernameTags(t *testing.T) {
	tags := map[string]string{}

	got := applyTriggeredUsernameTags(tags, map[string]interface{}{
		"sender": map[string]interface{}{"login": "github-user"},
	}, "triggered_user")

	if got != "github-user" {
		t.Fatalf("applyTriggeredUsernameTags() = %q, want github-user", got)
	}
	if tags["username"] != "github-user" {
		t.Fatalf("username tag = %q, want github-user", tags["username"])
	}
	if tags["triggered_user_id"] != "github-user" {
		t.Fatalf("triggered_user_id tag = %q, want github-user", tags["triggered_user_id"])
	}
}
