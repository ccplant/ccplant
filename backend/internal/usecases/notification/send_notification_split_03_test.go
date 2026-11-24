package notification

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestManageSubscriptionUseCase_CreateSubscription_ValidationFailed(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{
		validateSubErr: errors.New("invalid subscription endpoint"),
	}

	// Add user
	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	userRepo.users["user_123"] = user

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &CreateSubscriptionRequest{
		UserID:   "user_123",
		Type:     entities.SubscriptionTypeWebPush,
		Endpoint: "https://push.example.com",
		Keys:     map[string]string{"p256dh": "key1", "auth": "key2"},
	}

	ctx := context.Background()
	resp, err := uc.CreateSubscription(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "subscription validation failed")
}

func TestManageSubscriptionUseCase_DeleteSubscription_Success(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	// Add user
	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	userRepo.users["user_123"] = user

	// Add subscription
	sub := entities.NewSubscription(
		"sub_123",
		"user_123",
		entities.UserTypeAPIKey,
		entities.SubscriptionTypeWebPush,
		"testuser",
		"https://push.example.com",
		map[string]string{"p256dh": "key1", "auth": "key2"},
	)
	notifRepo.subscriptions["sub_123"] = sub

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &DeleteSubscriptionRequest{
		SubscriptionID: "sub_123",
		UserID:         "user_123",
	}

	ctx := context.Background()
	resp, err := uc.DeleteSubscription(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestManageSubscriptionUseCase_DeleteSubscription_NilRequest(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	ctx := context.Background()
	resp, err := uc.DeleteSubscription(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestManageSubscriptionUseCase_DeleteSubscription_EmptySubscriptionID(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &DeleteSubscriptionRequest{
		SubscriptionID: "",
		UserID:         "user_123",
	}

	ctx := context.Background()
	resp, err := uc.DeleteSubscription(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "subscription ID cannot be empty")
}

func TestManageSubscriptionUseCase_DeleteSubscription_EmptyUserID(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &DeleteSubscriptionRequest{
		SubscriptionID: "sub_123",
		UserID:         "",
	}

	ctx := context.Background()
	resp, err := uc.DeleteSubscription(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "user ID cannot be empty")
}

func TestManageSubscriptionUseCase_DeleteSubscription_NotFound(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &DeleteSubscriptionRequest{
		SubscriptionID: "nonexistent_sub",
		UserID:         "user_123",
	}

	ctx := context.Background()
	resp, err := uc.DeleteSubscription(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to find subscription")
}

func TestManageSubscriptionUseCase_DeleteSubscription_Unauthorized(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	// Add user (not admin)
	user := entities.NewUser("user_456", entities.UserTypeAPIKey, "otheruser")
	userRepo.users["user_456"] = user

	// Add subscription owned by different user
	sub := entities.NewSubscription(
		"sub_123",
		"user_123", // owned by user_123
		entities.UserTypeAPIKey,
		entities.SubscriptionTypeWebPush,
		"testuser",
		"https://push.example.com",
		map[string]string{"p256dh": "key1", "auth": "key2"},
	)
	notifRepo.subscriptions["sub_123"] = sub

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &DeleteSubscriptionRequest{
		SubscriptionID: "sub_123",
		UserID:         "user_456", // trying to delete as different user
	}

	ctx := context.Background()
	resp, err := uc.DeleteSubscription(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "does not have permission")
}

func TestManageSubscriptionUseCase_DeleteSubscription_AdminCanDelete(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	// Add admin user
	adminUser := entities.NewUser("admin_123", entities.UserTypeAPIKey, "adminuser")
	assert.NoError(t, adminUser.SetRoles([]entities.Role{entities.RoleAdmin}))
	userRepo.users["admin_123"] = adminUser

	// Add subscription owned by different user
	sub := entities.NewSubscription(
		"sub_123",
		"user_123", // owned by user_123
		entities.UserTypeAPIKey,
		entities.SubscriptionTypeWebPush,
		"testuser",
		"https://push.example.com",
		map[string]string{"p256dh": "key1", "auth": "key2"},
	)
	notifRepo.subscriptions["sub_123"] = sub

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &DeleteSubscriptionRequest{
		SubscriptionID: "sub_123",
		UserID:         "admin_123", // admin deleting other user's subscription
	}

	ctx := context.Background()
	resp, err := uc.DeleteSubscription(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
}
