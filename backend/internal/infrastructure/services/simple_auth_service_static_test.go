package services_test

import (
	"context"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

func TestLoadStaticAPIKey_AdminRole(t *testing.T) {
	svc := services.NewSimpleAuthService()
	ctx := context.Background()

	user, err := svc.ValidateAPIKey(ctx, "")
	if err == nil {
		t.Fatal("empty key must not validate")
	}
	_ = user

	if err := svc.LoadStaticAPIKey("pentester", "admin", "ap_static_admin", []string{"*"}); err != nil {
		t.Fatalf("LoadStaticAPIKey failed: %v", err)
	}

	loaded, err := svc.ValidateAPIKey(ctx, "ap_static_admin")
	if err != nil {
		t.Fatalf("static admin key should validate: %v", err)
	}
	if loaded.ID() != "pentester" {
		t.Errorf("unexpected user id: %s", loaded.ID())
	}
	if !loaded.IsAdmin() {
		t.Error("admin role key should produce an admin user")
	}
}

func TestLoadStaticAPIKey_RegularRoleWithPermissions(t *testing.T) {
	svc := services.NewSimpleAuthService()
	ctx := context.Background()

	if err := svc.LoadStaticAPIKey("limited", "user", "ap_static_user", []string{"session:create", "session:read"}); err != nil {
		t.Fatalf("LoadStaticAPIKey failed: %v", err)
	}

	loaded, err := svc.ValidateAPIKey(ctx, "ap_static_user")
	if err != nil {
		t.Fatalf("static user key should validate: %v", err)
	}
	if loaded.IsAdmin() {
		t.Error("regular role key must not be admin")
	}
	perms := loaded.Permissions()
	has := func(p entities.Permission) bool {
		for _, got := range perms {
			if got == p {
				return true
			}
		}
		return false
	}
	if !has(entities.PermissionSessionCreate) || !has(entities.PermissionSessionRead) {
		t.Errorf("expected session permissions, got %v", perms)
	}
	if has(entities.PermissionSessionDelete) {
		t.Errorf("unexpected permission granted: %v", perms)
	}
}

func TestLoadStaticAPIKey_RejectsMissingFields(t *testing.T) {
	svc := services.NewSimpleAuthService()
	if err := svc.LoadStaticAPIKey("", "admin", "ap_key", nil); err == nil {
		t.Error("empty user ID must be rejected")
	}
	if err := svc.LoadStaticAPIKey("user", "admin", "", nil); err == nil {
		t.Error("empty key must be rejected")
	}
}
