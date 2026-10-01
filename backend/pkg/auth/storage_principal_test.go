package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/authzscope"
)

func TestAttachStoragePrincipalCopiesAuthorizationScope(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest("GET", "/settings", nil)
	c := e.NewContext(req, httptest.NewRecorder())
	user := entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice")
	authz := &AuthorizationContext{
		User: user, PersonalScope: PersonalScopeAuth{UserID: "alice"},
		TeamScope: TeamScopeAuth{Teams: []string{"org/red"}},
	}

	attachStoragePrincipal(c, authz)
	principal, ok := authzscope.FromContext(c.Request().Context())
	if !ok || principal.ActorID != "alice" || principal.UserID != "alice" || len(principal.TeamIDs) != 1 || principal.TeamIDs[0] != "org/red" {
		t.Fatalf("storage principal = %#v, present=%v", principal, ok)
	}
	authz.TeamScope.Teams[0] = "org/changed"
	if principal.TeamIDs[0] != "org/red" {
		t.Fatal("storage principal retained a mutable team slice")
	}
}
