package auth

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
)

func TestWorkerControlAPIUsesInternalTokenAuthentication(t *testing.T) {
	if !isInternalTokenEndpoint("/internal/worker/sessions") {
		t.Fatal("worker control API must bypass user authentication middleware")
	}
	if isInternalTokenEndpoint("/internal/workers") {
		t.Fatal("unrelated path must not bypass user authentication middleware")
	}
}

func TestAuthenticateScheduleExecution(t *testing.T) {
	now := time.Now()
	token, err := executiontoken.SignExecutionToken([]byte("secret"), executiontoken.ExecutionClaims{ScheduleID: "s", ExecutionID: "e", SessionID: "session", UserID: "alice", Teams: []string{"org/team"}, ExpiresAt: now.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	req := httptest.NewRequest("POST", "/start", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	ctx := e.NewContext(req, httptest.NewRecorder())
	if !authenticateScheduleExecution(ctx, "secret", now) {
		t.Fatal("valid execution token rejected")
	}
	if user := GetUserFromContext(ctx); user == nil || string(user.ID()) != "alice" {
		t.Fatalf("user=%v", user)
	}
	if !GetAuthorizationContext(ctx).CanCreateInTeam("org/team") {
		t.Fatal("team context not restored")
	}
}

func TestGitHubConnectionCallbackUsesStateAuthentication(t *testing.T) {
	if !isOAuthEndpoint("/auth/github-connections/callback") {
		t.Fatal("GitHub connection callback must bypass bearer authentication and validate its one-time state")
	}
	if isOAuthEndpoint("/auth/github-connections") {
		t.Fatal("authenticated GitHub connection endpoints must not bypass authentication")
	}
}
