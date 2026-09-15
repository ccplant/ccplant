package executiontoken

import (
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestExecutionTokenRoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	want := ExecutionClaims{
		ScheduleID: "schedule-1", ExecutionID: "execution-1", SessionID: "session-1", UserID: "alice",
		Scope: entities.ScopeTeam, TeamID: "org/team", Teams: []string{"org/team"},
		ExpiresAt: now.Add(time.Minute).Unix(),
	}
	token, err := SignExecutionToken([]byte("secret"), want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyExecutionToken([]byte("secret"), token, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScheduleID != want.ScheduleID || got.ExecutionID != want.ExecutionID || got.UserID != want.UserID || got.TeamID != want.TeamID {
		t.Fatalf("claims = %#v, want %#v", got, want)
	}
}

func TestExecutionTokenRejectsTamperingAndExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token, err := SignExecutionToken([]byte("secret"), ExecutionClaims{
		ScheduleID: "schedule-1", ExecutionID: "execution-1", SessionID: "session-1", UserID: "alice", ExpiresAt: now.Add(time.Second).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyExecutionToken([]byte("wrong"), token, now); err == nil {
		t.Fatal("wrong secret was accepted")
	}
	if _, err := VerifyExecutionToken([]byte("secret"), token, now.Add(time.Second)); err == nil {
		t.Fatal("expired token was accepted")
	}
}

func TestSlackExecutionToken(t *testing.T) {
	now := time.Now()
	claims := ExecutionClaims{SlackBotID: "bot", ExecutionID: "event", SessionID: "session", UserID: "owner", TriggeredUserID: "actor", ExpiresAt: now.Add(time.Minute).Unix()}
	token, err := SignExecutionToken([]byte("secret"), claims)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyExecutionToken([]byte("secret"), token, now)
	if err != nil || got.SlackBotID != "bot" || got.TriggeredUserID != "actor" {
		t.Fatalf("claims=%+v err=%v", got, err)
	}
	if _, err := VerifyExecutionToken([]byte("wrong"), token, now); err == nil {
		t.Fatal("accepted wrong signing key")
	}
	if _, err := VerifyExecutionToken([]byte("secret"), token, now.Add(time.Minute)); err == nil {
		t.Fatal("accepted expired token")
	}
	claims.ScheduleID = "schedule"
	token, _ = SignExecutionToken([]byte("secret"), claims)
	if _, err := VerifyExecutionToken([]byte("secret"), token, now); err == nil {
		t.Fatal("accepted ambiguous trigger")
	}
}

func TestWebhookExecutionToken(t *testing.T) {
	now := time.Now()
	claims := ExecutionClaims{WebhookID: "webhook", ExecutionID: "execution", SessionID: "session", UserID: "owner", TriggeredUserID: "actor", ExpiresAt: now.Add(time.Minute).Unix()}
	token, _ := SignExecutionToken([]byte("secret"), claims)
	got, err := VerifyExecutionToken([]byte("secret"), token, now)
	if err != nil || got.WebhookID != "webhook" || got.TriggeredUserID != "actor" {
		t.Fatalf("claims=%+v err=%v", got, err)
	}
	for _, origin := range []string{"schedule", "slackbot"} {
		mixed := claims
		if origin == "schedule" {
			mixed.ScheduleID = origin
		} else {
			mixed.SlackBotID = origin
		}
		token, _ = SignExecutionToken([]byte("secret"), mixed)
		if _, err := VerifyExecutionToken([]byte("secret"), token, now); err == nil {
			t.Fatal("mixed trigger identities accepted")
		}
	}
}
