package services

import (
	"context"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type localUserRepoStub struct{ user *entities.LocalUser }

func (r localUserRepoStub) Create(context.Context, *entities.LocalUser) error { return nil }
func (r localUserRepoStub) GetByID(_ context.Context, id entities.UserID) (*entities.LocalUser, error) {
	if r.user == nil || r.user.ID != id {
		return nil, entities.ErrLocalUserNotFound
	}
	return r.user, nil
}

func TestSimpleAuthServiceAuthenticatesPersistedLocalUser(t *testing.T) {
	local, err := entities.NewLocalUser("alice", "Alice", "", entities.RoleUser, "admin")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewSimpleAuthService()
	svc.SetLocalUserRepository(localUserRepoStub{user: local})
	token := entities.NewAPIToken("tok", "apt_local", "apt_loca", "initial", entities.APITokenScopeUser, local.ID, "", []entities.Permission{entities.PermissionSessionCreate}, nil, "admin")
	if err := svc.LoadAPIToken(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ValidateAPIKey(context.Background(), "apt_local")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "local:alice" || got.Username() != "alice" || !got.HasPermission(entities.PermissionSessionCreate) || got.IsAdmin() {
		t.Fatalf("unexpected authenticated user: id=%s username=%s roles=%v", got.ID(), got.Username(), got.Roles())
	}
}

func TestSimpleAuthServiceDoesNotGrantAdminRoleWithoutAdminPermission(t *testing.T) {
	local, _ := entities.NewLocalUser("operator", "Operator", "", entities.RoleAdmin, "admin")
	svc := NewSimpleAuthService()
	svc.SetLocalUserRepository(localUserRepoStub{user: local})
	token := entities.NewAPIToken("tok", "apt_limited", "apt_limi", "limited", entities.APITokenScopeUser, local.ID, "", []entities.Permission{entities.PermissionSessionRead}, nil, "admin")
	_ = svc.LoadAPIToken(context.Background(), token)
	got, err := svc.ValidateAPIKey(context.Background(), "apt_limited")
	if err != nil {
		t.Fatal(err)
	}
	if got.IsAdmin() || got.HasPermission(entities.PermissionAdmin) {
		t.Fatal("limited token gained admin privileges")
	}
}
