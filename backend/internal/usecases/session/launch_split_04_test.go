package session

import (
	"context"
	"errors"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestLaunchReuseReturnsErrorWhenStopBeforeReuseFails(t *testing.T) {
	sessionManager := &recordingSessionManager{
		existing:     []entities.Session{&launchTestSession{id: "existing-1", userID: "user-1", status: "active"}},
		stopAgentErr: errors.New("stop failed"),
	}
	launcher := NewLaunchUseCase(sessionManager)

	_, err := launcher.Launch(context.Background(), "new-1", LaunchRequest{
		UserID:          "user-1",
		Scope:           entities.ScopeUser,
		InitialMessage:  "initial",
		ReuseSession:    true,
		ReuseMatchTags:  map[string]string{"webhook_id": "webhook-1"},
		StopBeforeReuse: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if sessionManager.sendMessageID != "" {
		t.Fatal("SendMessage should not be called when StopAgent fails")
	}
	if sessionManager.req != nil {
		t.Fatal("CreateSession should not be called when StopAgent fails")
	}
}

func TestLaunchReuseReturnsErrorForNonStaleSendFailure(t *testing.T) {
	sessionManager := &recordingSessionManager{
		existing:       []entities.Session{&launchTestSession{id: "existing-1", userID: "user-1", status: "active"}},
		sendMessageErr: errors.New("connection refused"),
	}
	launcher := NewLaunchUseCase(sessionManager)

	_, err := launcher.Launch(context.Background(), "new-1", LaunchRequest{
		UserID:         "user-1",
		Scope:          entities.ScopeUser,
		InitialMessage: "initial",
		ReuseSession:   true,
		ReuseMatchTags: map[string]string{"webhook_id": "webhook-1"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if sessionManager.req != nil {
		t.Fatal("CreateSession should not be called for non-stale reuse errors")
	}
}

func boolPointer(v bool) *bool {
	return &v
}

func TestLaunchProfileAuthenticationMethods(t *testing.T) {
	for _, override := range []string{"", "auth_json"} {
		t.Run("request_"+override, func(t *testing.T) {
			manager := &recordingSessionManager{}
			profile := entities.NewSessionProfile("profile-1", "default", "user-1")
			profile.SetIsDefault(true)
			cfg := entities.NewSessionProfileConfig()
			cfg.SetParams(&entities.SessionParams{Model: "profile-model", CodexAuthMode: "openai_compatible", ClaudeAuthMode: "bedrock"})
			profile.SetConfig(cfg)
			launcher := NewLaunchUseCase(manager).WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})
			requestModel := ""
			if override != "" {
				requestModel = "request-model"
			}
			_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{UserID: "user-1", Scope: entities.ScopeUser, Model: requestModel, CodexAuthMode: override})
			if err != nil {
				t.Fatal(err)
			}
			if manager.req.ResolvedSessionProfileID != "profile-1" {
				t.Fatalf("profile ID was not propagated")
			}
			want := "openai_compatible"
			if override != "" {
				want = override
			}
			if manager.req.CodexAuthMode != want || manager.req.ClaudeAuthMode != "bedrock" {
				t.Fatalf("unexpected auth methods: %q / %q", manager.req.CodexAuthMode, manager.req.ClaudeAuthMode)
			}
			wantModel := "profile-model"
			if requestModel != "" {
				wantModel = requestModel
			}
			if manager.req.Model != wantModel {
				t.Fatalf("unexpected model: %q", manager.req.Model)
			}
		})
	}
}
