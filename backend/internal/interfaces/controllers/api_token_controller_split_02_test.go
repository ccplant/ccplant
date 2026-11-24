package controllers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	apitokenuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/api_token"
)

func TestAPITokenController_Get_OtherUserNotFound(t *testing.T) {
	ctrl, repo, _ := setupAPITokenController()
	assert.NoError(t, repo.Create(context.Background(), entities.NewAPIToken(
		"tok_owner", "secret_o", "secret_o", "n",
		entities.APITokenScopeUser, entities.UserID("owner"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil, entities.UserID("owner"))))

	c, recorder := makeAPITokenEchoContextWithParam(t, http.MethodGet, "/api-tokens/tok_owner", "tok_owner", nil, newTestAPIKeyUser("intruder"))
	assert.NotNil(t, recorder)
	assertHTTPError(t, ctrl.Get(c), http.StatusNotFound) // 404, no cross-scope leak
}

func TestAPITokenController_Get_MissingNotFound(t *testing.T) {
	ctrl, repo, authSvc := setupAPITokenController()
	assert.NotNil(t, repo)
	assert.NotNil(t, authSvc)
	c, recorder := makeAPITokenEchoContextWithParam(t, http.MethodGet, "/api-tokens/ghost", "ghost", nil, newTestAPIKeyUser("user-1"))
	assert.NotNil(t, recorder)
	assertHTTPError(t, ctrl.Get(c), http.StatusNotFound)
}

// --- Delete ---

func TestAPITokenController_Delete_Owner_204(t *testing.T) {
	ctrl, repo, authSvc := setupAPITokenController()
	assert.NoError(t, repo.Create(context.Background(), entities.NewAPIToken(
		"tok_del", "secret_del", "secret_d", "n",
		entities.APITokenScopeUser, entities.UserID("user-1"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil, entities.UserID("user-1"))))

	c, rec := makeAPITokenEchoContextWithParam(t, http.MethodDelete, "/api-tokens/tok_del", "tok_del", nil, newTestAPIKeyUser("user-1"))
	require.NoError(t, ctrl.Delete(c))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Len(t, authSvc.revoked, 1, "immediate local revocation expected")
	_, err := repo.GetByID(context.Background(), "tok_del")
	assert.True(t, errors.Is(err, entities.ErrAPITokenNotFound))
}

func TestAPITokenController_Delete_IdempotentNonexistent_204(t *testing.T) {
	ctrl, repo, authSvc := setupAPITokenController()
	assert.NotNil(t, repo)
	c, rec := makeAPITokenEchoContextWithParam(t, http.MethodDelete, "/api-tokens/ghost", "ghost", nil, newTestAPIKeyUser("user-1"))
	require.NoError(t, ctrl.Delete(c))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, authSvc.revoked)
}

func TestAPITokenController_Delete_NonOwner_NoLeak_204(t *testing.T) {
	ctrl, repo, authSvc := setupAPITokenController()
	assert.NoError(t, repo.Create(context.Background(), entities.NewAPIToken(
		"tok_other", "secret_o", "secret_o", "n",
		entities.APITokenScopeUser, entities.UserID("owner"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil, entities.UserID("owner"))))

	c, rec := makeAPITokenEchoContextWithParam(t, http.MethodDelete, "/api-tokens/tok_other", "tok_other", nil, newTestAPIKeyUser("intruder"))
	require.NoError(t, ctrl.Delete(c))
	assert.Equal(t, http.StatusNoContent, rec.Code) // no leak, no delete
	assert.Empty(t, authSvc.revoked)
	if _, err := repo.GetByID(context.Background(), "tok_other"); err != nil {
		t.Errorf("non-owner delete removed the token: %v", err)
	}
}

func TestAPITokenController_Delete_TeamCreatorOrAdmin(t *testing.T) {
	ctrl, repo, authSvc := setupAPITokenController()
	creator := newTestGitHubUser("creator", "org", "team")
	out, err := apitokenuc.NewCreateAPITokenUseCase(repo, authSvc).Execute(context.Background(), &apitokenuc.CreateAPITokenInput{
		Caller:      creator,
		Scope:       "team",
		TeamID:      "org/team",
		Name:        "team",
		Permissions: []entities.Permission{entities.PermissionSessionCreate},
	})
	require.NoError(t, err)

	// admin can delete
	c, rec := makeAPITokenEchoContextWithParam(t, http.MethodDelete, "/api-tokens/"+out.Token.ID(), out.Token.ID(), nil, newTestAdminUser("admin"))
	require.NoError(t, ctrl.Delete(c))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// --- Backward compatibility ---

func TestAPITokenController_PersonalAPIKeyEndpointStillWorks(t *testing.T) {
	// The legacy /users/me/api-key controller must continue to exist and
	// behave unchanged. It is wired separately in the router; here we just
	// assert the controller type is still instantiable to guard against
	// accidental removal.
	controller := NewPersonalAPIKeyController(nil, nil)
	assert.NotNil(t, controller)
}
