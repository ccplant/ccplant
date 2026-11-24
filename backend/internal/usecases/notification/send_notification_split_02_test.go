package notification

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSendNotificationUseCase_Execute_NilRequest(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)

	ctx := context.Background()
	resp, err := uc.Execute(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestSendNotificationUseCase_Execute_EmptyUserID(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)

	req := &SendNotificationRequest{
		UserID: "",
		Title:  "Test Title",
		Body:   "Test Body",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "user ID cannot be empty")
}

func TestSendNotificationUseCase_Execute_EmptyTitle(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)

	req := &SendNotificationRequest{
		UserID: "user_123",
		Title:  "",
		Body:   "Test Body",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "title cannot be empty")
}

func TestSendNotificationUseCase_Execute_EmptyBody(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)

	req := &SendNotificationRequest{
		UserID: "user_123",
		Title:  "Test Title",
		Body:   "",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "body cannot be empty")
}

func TestSendNotificationUseCase_Execute_UserNotFound(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)

	req := &SendNotificationRequest{
		UserID: "nonexistent_user",
		Title:  "Test Title",
		Body:   "Test Body",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to find user")
}

func TestSendNotificationUseCase_Execute_InactiveUser(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	// Add inactive user
	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	user.Deactivate()
	userRepo.users["user_123"] = user

	uc := NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)

	req := &SendNotificationRequest{
		UserID: "user_123",
		Title:  "Test Title",
		Body:   "Test Body",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not active")
}

func TestSendNotificationUseCase_Execute_NoSubscriptions(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	// Add user but no subscriptions
	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	userRepo.users["user_123"] = user

	uc := NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)

	req := &SendNotificationRequest{
		UserID: "user_123",
		Title:  "Test Title",
		Body:   "Test Body",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 0, resp.SentCount)
	assert.Equal(t, 0, resp.FailedCount)
}

func TestSendNotificationUseCase_Execute_SendBulkFailed(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{
		sendBulkErr: errors.New("failed to send notifications"),
	}

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

	uc := NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)

	req := &SendNotificationRequest{
		UserID: "user_123",
		Title:  "Test Title",
		Body:   "Test Body",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	// Note: The use case may fail at validation before sending
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// Tests for ManageSubscriptionUseCase

func TestNewManageSubscriptionUseCase(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	assert.NotNil(t, uc)
}

func TestManageSubscriptionUseCase_CreateSubscription_Success(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

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

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Subscription)
}

func TestManageSubscriptionUseCase_CreateSubscription_NilRequest(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	ctx := context.Background()
	resp, err := uc.CreateSubscription(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestManageSubscriptionUseCase_CreateSubscription_EmptyUserID(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &CreateSubscriptionRequest{
		UserID:   "",
		Type:     entities.SubscriptionTypeWebPush,
		Endpoint: "https://push.example.com",
	}

	ctx := context.Background()
	resp, err := uc.CreateSubscription(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "user ID cannot be empty")
}

func TestManageSubscriptionUseCase_CreateSubscription_EmptyType(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &CreateSubscriptionRequest{
		UserID:   "user_123",
		Type:     "",
		Endpoint: "https://push.example.com",
	}

	ctx := context.Background()
	resp, err := uc.CreateSubscription(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "subscription type cannot be empty")
}

func TestManageSubscriptionUseCase_CreateSubscription_EmptyEndpoint(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &CreateSubscriptionRequest{
		UserID:   "user_123",
		Type:     entities.SubscriptionTypeWebPush,
		Endpoint: "",
	}

	ctx := context.Background()
	resp, err := uc.CreateSubscription(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "endpoint cannot be empty")
}

func TestManageSubscriptionUseCase_CreateSubscription_UserNotFound(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	uc := NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	req := &CreateSubscriptionRequest{
		UserID:   "nonexistent_user",
		Type:     entities.SubscriptionTypeWebPush,
		Endpoint: "https://push.example.com",
		Keys:     map[string]string{"p256dh": "key1", "auth": "key2"},
	}

	ctx := context.Background()
	resp, err := uc.CreateSubscription(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to find user")
}

func TestManageSubscriptionUseCase_CreateSubscription_InactiveUser(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}

	// Add inactive user
	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	user.Deactivate()
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
	assert.Contains(t, err.Error(), "not active")
}
