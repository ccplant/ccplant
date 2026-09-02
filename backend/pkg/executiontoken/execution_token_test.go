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
