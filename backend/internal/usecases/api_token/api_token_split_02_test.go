package api_token

import (
	"context"
	"errors"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestGetAPIToken_TeamMemberCanRead(t *testing.T) {
	repo := newFakeRepo()
	createUC := NewCreateAPITokenUseCase(repo, &fakeAuth{})
	out, err := createUC.Execute(context.Background(), &CreateAPITokenInput{
		Caller: callerWith([]entities.Permission{entities.PermissionSessionCreate}, "team"),
		Scope:  "team", TeamID: "org/team",
		Permissions: []entities.Permission{entities.PermissionSessionCreate},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	getUC := NewGetAPITokenUseCase(repo)
	member := callerWith([]entities.Permission{entities.PermissionSessionRead}, "team")
	got, err := getUC.Execute(context.Background(), &GetAPITokenInput{Caller: member, TokenID: out.Token.ID()})
	if err != nil {
		t.Fatalf("member get: %v", err)
	}
	if got.Token.TeamID() != "org/team" {
		t.Errorf("team = %q", got.Token.TeamID())
	}
}

// --- Delete ---

func TestDeleteAPIToken_OwnerDeletesAndRevokes(t *testing.T) {
	repo := newFakeRepo()
	auth := &fakeAuth{}
	createUC := NewCreateAPITokenUseCase(repo, auth)
	out, err := createUC.Execute(context.Background(), &CreateAPITokenInput{
		Caller: callerWith([]entities.Permission{entities.PermissionSessionRead}), Scope: "user",
		Permissions: []entities.Permission{entities.PermissionSessionRead},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	deleteUC := NewDeleteAPITokenUseCase(repo, auth)
	// ensure owner.ID matches creator
	owner := entities.NewUser(out.Token.UserID(), entities.UserTypeRegular, "owner")
	owner.SetPermissions([]entities.Permission{entities.PermissionSessionRead})
	if err := deleteUC.Execute(context.Background(), &DeleteAPITokenInput{Caller: owner, TokenID: out.Token.ID()}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(auth.revoked) != 1 || auth.revoked[0] != out.Token.Secret() {
		t.Errorf("expected revoke, got %v", auth.revoked)
	}
	// token gone
	if _, err := repo.GetByID(context.Background(), out.Token.ID()); !errors.Is(err, entities.ErrAPITokenNotFound) {
		t.Errorf("expected gone, got %v", err)
	}
}

func TestDeleteAPIToken_NonOwnerNoDeleteNoLeak(t *testing.T) {
	repo := newFakeRepo()
	auth := &fakeAuth{}
	createUC := NewCreateAPITokenUseCase(repo, auth)
	out, err := createUC.Execute(context.Background(), &CreateAPITokenInput{
		Caller: callerWith([]entities.Permission{entities.PermissionSessionRead}), Scope: "user",
		Permissions: []entities.Permission{entities.PermissionSessionRead},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	deleteUC := NewDeleteAPITokenUseCase(repo, auth)
	other := entities.NewUser(entities.UserID("other-1"), entities.UserTypeRegular, "other")
	other.SetPermissions([]entities.Permission{entities.PermissionSessionRead})
	if err := deleteUC.Execute(context.Background(), &DeleteAPITokenInput{Caller: other, TokenID: out.Token.ID()}); err != nil {
		t.Fatalf("delete returned error (should be silent): %v", err)
	}
	// token must still exist
	if _, err := repo.GetByID(context.Background(), out.Token.ID()); err != nil {
		t.Errorf("token was deleted by non-owner: %v", err)
	}
	if len(auth.revoked) != 0 {
		t.Errorf("non-owner should not revoke, got %v", auth.revoked)
	}
}

func TestDeleteAPIToken_TeamCreatorOrAdmin(t *testing.T) {
	repo := newFakeRepo()
	auth := &fakeAuth{}
	createUC := NewCreateAPITokenUseCase(repo, auth)
	creator := callerWith([]entities.Permission{entities.PermissionSessionCreate}, "team")
	out, err := createUC.Execute(context.Background(), &CreateAPITokenInput{
		Caller: creator, Scope: "team", TeamID: "org/team",
		Permissions: []entities.Permission{entities.PermissionSessionCreate},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	deleteUC := NewDeleteAPITokenUseCase(repo, auth)

	// a different team member (not creator) cannot delete
	member := entities.NewUser(entities.UserID("member-1"), entities.UserTypeRegular, "member")
	member.SetPermissions([]entities.Permission{entities.PermissionSessionCreate})
	info := entities.NewGitHubUserInfo(2, "member", "member", "m@example.com", "", "", "")
	member.SetGitHubInfo(info, []entities.GitHubTeamMembership{{Organization: "org", TeamSlug: "team"}})
	if err := deleteUC.Execute(context.Background(), &DeleteAPITokenInput{Caller: member, TokenID: out.Token.ID()}); err != nil {
		t.Fatalf("delete error: %v", err)
	}
	if _, err := repo.GetByID(context.Background(), out.Token.ID()); err != nil {
		t.Errorf("non-creator team member deleted the token (should not): %v", err)
	}

	// creator can delete
	if err := deleteUC.Execute(context.Background(), &DeleteAPITokenInput{Caller: creator, TokenID: out.Token.ID()}); err != nil {
		t.Fatalf("creator delete: %v", err)
	}
	if _, err := repo.GetByID(context.Background(), out.Token.ID()); !errors.Is(err, entities.ErrAPITokenNotFound) {
		t.Errorf("creator delete failed: %v", err)
	}

	// recreate and admin can delete
	out2, err := createUC.Execute(context.Background(), &CreateAPITokenInput{
		Caller: creator, Scope: "team", TeamID: "org/team",
		Permissions: []entities.Permission{entities.PermissionSessionCreate},
	})
	if err != nil {
		t.Fatalf("recreate: %v", err)
	}
	if err := deleteUC.Execute(context.Background(), &DeleteAPITokenInput{Caller: adminCaller(t), TokenID: out2.Token.ID()}); err != nil {
		t.Fatalf("admin delete: %v", err)
	}
}

func TestDeleteAPIToken_IdempotentNonexistent(t *testing.T) {
	repo := newFakeRepo()
	deleteUC := NewDeleteAPITokenUseCase(repo, &fakeAuth{})
	if err := deleteUC.Execute(context.Background(), &DeleteAPITokenInput{
		Caller: callerWith([]entities.Permission{entities.PermissionSessionRead}), TokenID: "ghost",
	}); err != nil {
		t.Errorf("idempotent delete returned error: %v", err)
	}
}
