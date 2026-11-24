package notification

import (
	"fmt"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// Service provides notification functionality
type Service struct {
	storage            Storage
	webpush            *WebPushService
	slack              *SlackService            // Optional, for Slack DM notifications
	secretSyncer       SubscriptionSecretSyncer // Optional, for syncing subscriptions to K8s Secrets (legacy)
	subscriptionReader SubscriptionReader       // Optional, for reading subscriptions from K8s Secrets
	subscriptionWriter SubscriptionWriter       // Optional, for writing subscriptions directly to K8s Secrets
	baseURLResolver    func() string
}

func (s *Service) SetBaseURLResolver(resolver func() string) { s.baseURLResolver = resolver }

// NewService creates a new notification service
func NewService(baseDir string) (*Service, error) {
	storage := NewJSONLStorage(baseDir)

	// WebPush service is optional - notifications can be stored without sending
	webpush, err := NewWebPushService()
	if err != nil {
		log.Printf("[NOTIFICATION_SERVICE] Web push is unavailable: %v", err)
	}

	// Slack service is optional - only available when SLACK_BOT_TOKEN is set
	slackSvc, err := NewSlackService()
	if err != nil {
		log.Printf("[NOTIFICATION_SERVICE] Slack notifications are unavailable: %v", err)
	}

	return &Service{
		storage: storage,
		webpush: webpush,
		slack:   slackSvc,
	}, nil
}

// SetSecretSyncer sets the secret syncer for syncing subscriptions to K8s Secrets
// This is optional and only used when Kubernetes mode is enabled
func (s *Service) SetSecretSyncer(syncer SubscriptionSecretSyncer) {
	s.secretSyncer = syncer
}

// SetSubscriptionReader sets the subscription reader for reading subscriptions from K8s Secrets.
// When set, SendNotificationToUser and SendNotificationToSession will read subscriptions from
// the external storage (e.g., Kubernetes Secrets) instead of the local file-based storage.
func (s *Service) SetSubscriptionReader(reader SubscriptionReader) {
	s.subscriptionReader = reader
}

// SetSubscriptionWriter sets the subscription writer for direct K8s Secret writes.
// When set, all subscription mutations bypass local file storage entirely.
func (s *Service) SetSubscriptionWriter(writer SubscriptionWriter) {
	s.subscriptionWriter = writer
}

// readCurrentSubscriptions returns the authoritative subscription list for a user.
// In k8s mode (subscriptionWriter set) it reads from K8s Secret; otherwise from local storage.
func (s *Service) readCurrentSubscriptions(userID string) ([]Subscription, error) {
	if s.subscriptionWriter != nil && s.subscriptionReader != nil {
		return s.subscriptionReader.GetSubscriptions(userID)
	}
	return s.storage.GetSubscriptions(userID)
}

// persistSubscriptions writes the updated subscription list.
// In k8s mode (subscriptionWriter set) it writes directly to K8s Secret; otherwise it replaces
// each entry in local storage and calls Sync.
func (s *Service) persistSubscriptions(userID string, subs []Subscription) error {
	if s.subscriptionWriter != nil {
		return s.subscriptionWriter.UpdateSubscriptions(userID, subs)
	}
	// Local-storage path: rebuild from scratch then sync.
	// Clear existing entries for this user by deleting each known endpoint.
	existing, err := s.storage.GetSubscriptions(userID)
	if err != nil {
		return fmt.Errorf("get existing subscriptions: %w", err)
	}
	for _, sub := range existing {
		if err := s.storage.DeleteSubscription(userID, sub.Endpoint); err != nil {
			return fmt.Errorf("delete existing subscription %q: %w", sub.Endpoint, err)
		}
	}
	for _, sub := range subs {
		if err := s.storage.AddSubscription(userID, sub); err != nil {
			return err
		}
	}
	if s.secretSyncer != nil {
		if syncErr := s.secretSyncer.Sync(userID); syncErr != nil {
			log.Printf("[NOTIFICATION_SERVICE] Warning: failed to sync subscription secret: %v", syncErr)
		}
	}
	return nil
}

// getSubscriptionsForUser returns subscriptions for a user, preferring the external reader if set.
func (s *Service) getSubscriptionsForUser(userID string) ([]Subscription, error) {
	if s.subscriptionReader != nil {
		return s.subscriptionReader.GetSubscriptions(userID)
	}
	return s.storage.GetSubscriptions(userID)
}

// getAllSubscriptions returns all subscriptions, preferring the external reader if set.
func (s *Service) getAllSubscriptions() ([]Subscription, error) {
	if s.subscriptionReader != nil {
		return s.subscriptionReader.GetAllSubscriptions()
	}
	return s.storage.GetAllSubscriptions()
}

// GetStorage returns the storage instance (used by secret syncer)
func (s *Service) GetStorage() Storage {
	return s.storage
}

// Subscribe creates a new push notification subscription
func (s *Service) Subscribe(user *entities.User, endpoint string, keys map[string]string, deviceInfo *DeviceInfo) (*Subscription, error) {
	// Get username from GitHub user info if available, otherwise use UserID
	username := string(user.ID())
	if user.UserType() == entities.UserTypeGitHub && user.GitHubInfo() != nil {
		username = user.GitHubInfo().Login()
	}

	now := time.Now()
	newSub := Subscription{
		UserID:            string(user.ID()),
		UserType:          string(user.UserType()),
		Username:          username,
		Endpoint:          endpoint,
		Keys:              keys,
		SessionIDs:        []string{},
		NotificationTypes: []string{"message", "status_change", "session_update", "error"},
		DeviceInfo:        deviceInfo,
		CreatedAt:         now,
		UpdatedAt:         now,
		LastUsed:          now,
		Active:            true,
	}

	userID := string(user.ID())
	current, err := s.readCurrentSubscriptions(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to read subscriptions: %w", err)
	}

	// Replace existing sub with same endpoint, or append.
	updated := make([]Subscription, 0, len(current)+1)
	replaced := false
	for _, sub := range current {
		if sub.Endpoint == endpoint {
			updated = append(updated, newSub)
			replaced = true
		} else {
			updated = append(updated, sub)
		}
	}
	if !replaced {
		updated = append(updated, newSub)
	}

	if err := s.persistSubscriptions(userID, updated); err != nil {
		return nil, fmt.Errorf("failed to save subscription: %w", err)
	}

	return &newSub, nil
}

// SubscribeSlack creates or updates a Slack DM subscription for a user
func (s *Service) SubscribeSlack(user *entities.User, slackUserID string) (*Subscription, error) {
	if slackUserID == "" {
		return nil, fmt.Errorf("slack user ID is required")
	}

	username := string(user.ID())
	if user.UserType() == entities.UserTypeGitHub && user.GitHubInfo() != nil {
		username = user.GitHubInfo().Login()
	}

	userID := string(user.ID())
	current, err := s.readCurrentSubscriptions(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to read subscriptions: %w", err)
	}

	now := time.Now()
	newSub := Subscription{
		UserID:            userID,
		UserType:          string(user.UserType()),
		Username:          username,
		Type:              SubscriptionTypeSlack,
		Endpoint:          slackUserID,
		Keys:              map[string]string{},
		SessionIDs:        []string{},
		NotificationTypes: []string{"message", "status_change", "session_update", "error"},
		CreatedAt:         now,
		UpdatedAt:         now,
		LastUsed:          now,
		Active:            true,
	}

	// Remove existing Slack subs and add the new one.
	updated := make([]Subscription, 0, len(current)+1)
	for _, sub := range current {
		if sub.Type != SubscriptionTypeSlack {
			updated = append(updated, sub)
		}
	}
	updated = append(updated, newSub)

	if err := s.persistSubscriptions(userID, updated); err != nil {
		return nil, fmt.Errorf("failed to save Slack subscription: %w", err)
	}

	return &newSub, nil
}

// DeleteSlackSubscription removes the Slack subscription for a user
func (s *Service) DeleteSlackSubscription(userID string) error {
	current, err := s.readCurrentSubscriptions(userID)
	if err != nil {
		return err
	}
	updated := make([]Subscription, 0, len(current))
	for _, sub := range current {
		if sub.Type != SubscriptionTypeSlack {
			updated = append(updated, sub)
		}
	}
	return s.persistSubscriptions(userID, updated)
}

// GetSubscriptions returns all active subscriptions for a user
func (s *Service) GetSubscriptions(userID string) ([]Subscription, error) {
	return s.readCurrentSubscriptions(userID)
}

// DeleteSubscription removes a subscription by endpoint
func (s *Service) DeleteSubscription(userID string, endpoint string) error {
	current, err := s.readCurrentSubscriptions(userID)
	if err != nil {
		return err
	}
	updated := make([]Subscription, 0, len(current))
	found := false
	for _, sub := range current {
		if sub.Endpoint == endpoint {
			found = true
		} else {
			updated = append(updated, sub)
		}
	}
	if !found {
		return fmt.Errorf("subscription not found: %s", endpoint)
	}
	return s.persistSubscriptions(userID, updated)
}

// SetSubscriptionTypeActive sets the Active field for all subscriptions of a given type for a user.
// This is used to enable/disable a notification channel without deleting the subscription data.
func (s *Service) SetSubscriptionTypeActive(userID, subType string, active bool) error {
	current, err := s.readCurrentSubscriptions(userID)
	if err != nil {
		return err
	}

	updated := make([]Subscription, 0, len(current))
	for _, sub := range current {
		t := sub.Type
		if t == "" {
			t = SubscriptionTypeWebPush // backward compat
		}
		if t == subType {
			sub.Active = active
		}
		updated = append(updated, sub)
	}

	return s.persistSubscriptions(userID, updated)
}

// SendNotificationToUser sends a notification to all subscriptions of a user
func (s *Service) SendNotificationToUser(userID string, title, body, notificationType string, data map[string]interface{}) error {
	subscriptions, err := s.getSubscriptionsForUser(userID)
	if err != nil {
		return fmt.Errorf("failed to get subscriptions: %w", err)
	}

	var lastError error
	successCount := 0

	for _, sub := range subscriptions {
		// Skip inactive subscriptions (channel disabled by user)
		if !sub.Active {
			continue
		}
		// Check if subscription wants this notification type
		if !s.shouldSendNotification(sub, notificationType, data) {
			continue
		}

		var sendErr error
		subType := sub.Type
		if subType == "" {
			subType = SubscriptionTypeWebPush // backward compat
		}

		switch subType {
		case SubscriptionTypeWebPush:
			if s.webpush == nil {
				sendErr = fmt.Errorf("web push service not configured")
			} else {
				sendErr = s.webpush.SendNotification(sub, title, body, data)
			}
		case SubscriptionTypeSlack:
			if s.slack == nil {
				sendErr = fmt.Errorf("slack service not configured")
			} else {
				url := ""
				if u, ok := data["url"].(string); ok {
					url = u
				}
				initialMessage := ""
				if im, ok := data["initial_message"].(string); ok {
					initialMessage = im
				}
				sendErr = s.slack.SendDM(sub.Endpoint, title, body, url, initialMessage)
			}
		default:
			sendErr = fmt.Errorf("unsupported subscription type: %s", subType)
		}

		// Record history
		history := NotificationHistory{
			UserID:         userID,
			SubscriptionID: sub.ID,
			Title:          title,
			Body:           body,
			Type:           notificationType,
			SessionID:      getSessionIDFromData(data),
			Data:           data,
			SentAt:         time.Now(),
			Delivered:      sendErr == nil,
		}

		if sendErr != nil {
			errMsg := sendErr.Error()
			history.ErrorMessage = &errMsg
			lastError = sendErr
		} else {
			successCount++
		}

		// Save to history
		if histErr := s.storage.AddNotificationHistory(userID, history); histErr != nil {
			// Log but don't fail the notification send
			fmt.Printf("Failed to save notification history: %v\n", histErr)
		}
	}

	if successCount == 0 && lastError != nil {
		return fmt.Errorf("failed to send any notifications: %w", lastError)
	}

	return nil
}

// SendNotificationToSession sends a notification to all users subscribed to a session
