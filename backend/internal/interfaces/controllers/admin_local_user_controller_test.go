package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"k8s.io/client-go/kubernetes/fake"
)

func localUserContext(t *testing.T, method, path string, body any, caller *entities.User, names, values []string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	b, _ := json.Marshal(body)
	e := echo.New()
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("internal_user", caller)
	ctx.SetParamNames(names...)
	ctx.SetParamValues(values...)
	return ctx, rec
}

func TestAdminLocalUserCreateIssueTokenAndAuthenticate(t *testing.T) {
	kube := fake.NewSimpleClientset()
	users := repositories.NewKubernetesLocalUserRepository(kube, "default")
	tokens := repositories.NewKubernetesAPITokenRepository(kube, "default")
	authSvc := infra.NewSimpleAuthService()
	authSvc.SetLocalUserRepository(users)
	controller := NewAdminLocalUserController(users, tokens, authSvc)
	admin := entities.NewUser("admin", entities.UserTypeAPIKey, "admin")
	_ = admin.SetRoles([]entities.Role{entities.RoleAdmin})
	ctx, rec := localUserContext(t, http.MethodPost, "/admin/users", map[string]any{"username": "alice", "display_name": "Alice"}, admin, nil, nil)
	if err := controller.Create(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	ctx, rec = localUserContext(t, http.MethodPost, "/admin/users/local:alice/api-tokens", map[string]any{"name": "initial"}, admin, []string{"id"}, []string{"local:alice"})
	if err := controller.CreateToken(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var issued createLocalUserTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if issued.PlaintextToken == "" {
		t.Fatal("missing one-time plaintext token")
	}
	authenticated, err := authSvc.ValidateAPIKey(context.Background(), issued.PlaintextToken)
	if err != nil {
		t.Fatal(err)
	}
	if authenticated.ID() != "local:alice" || authenticated.Username() != "alice" || !authenticated.HasPermission(entities.PermissionSessionCreate) {
		t.Fatalf("unexpected identity: %s %s", authenticated.ID(), authenticated.Username())
	}
}
