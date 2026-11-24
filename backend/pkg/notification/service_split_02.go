package notification

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (s *Service) SendNotificationToSession(sessionID string, title, body, notificationType string, data map[string]interface{}) error {
	// Add session ID to data
	if data == nil {
		data = make(map[string]interface{})
	}
	data["session_id"] = sessionID

	subscriptions, err := s.getAllSubscriptions()
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
		// Check if user is subscribed to this session
		if !s.isSubscribedToSession(sub, sessionID) {
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
			UserID:         sub.UserID,
			SubscriptionID: sub.ID,
			Title:          title,
			Body:           body,
			Type:           notificationType,
			SessionID:      sessionID,
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
		if histErr := s.storage.AddNotificationHistory(sub.UserID, history); histErr != nil {
			// Log but don't fail the notification send
			fmt.Printf("Failed to save notification history: %v\n", histErr)
		}
	}

	if successCount == 0 && lastError != nil {
		return fmt.Errorf("failed to send any notifications: %w", lastError)
	}

	return nil
}

// ProcessWebhook handles incoming webhooks from agentapi
func (s *Service) ProcessWebhook(webhook WebhookRequest) error {
	// Map event types to notification parameters
	var title, body, notificationType string
	data := webhook.Data
	if data == nil {
		data = make(map[string]interface{})
	}
	// Set URL only if not already provided (e.g., enriched by the webhook handler)
	if _, exists := data["url"]; !exists {
		baseURL := ""
		if s.baseURLResolver != nil {
			baseURL = s.baseURLResolver()
		}
		if baseURL == "" {
			baseURL = os.Getenv("NOTIFICATION_BASE_URL")
		}
		if baseURL != "" {
			data["url"] = baseURL + "/sessions/" + webhook.SessionID
		} else {
			data["url"] = fmt.Sprintf("/sessions/%s", webhook.SessionID)
		}
	}

	switch webhook.EventType {
	case "message_received":
		title = "新しいメッセージ"
		body = "Claude からの返答が到着しました"
		notificationType = "message"
	case "status_change":
		status, _ := data["status"].(string)
		if status == "running" {
			title = "ステータス変更"
			body = "エージェントが応答中です"
		} else {
			title = "ステータス変更"
			body = "エージェントの応答が完了しました"
		}
		notificationType = "status_change"
	case "session_update":
		title = "セッション更新"
		body = "セッションが更新されました"
		notificationType = "session_update"
	case "error":
		title = "エラー発生"
		body = "セッションでエラーが発生しました"
		notificationType = "error"
	default:
		// Unknown event type, skip
		return nil
	}

	// Send notification to all users subscribed to this session
	return s.SendNotificationToSession(webhook.SessionID, title, body, notificationType, data)
}

// GetNotificationHistory retrieves notification history for a user
func (s *Service) GetNotificationHistory(userID string, limit, offset int, filters map[string]string) (*HistoryResponse, error) {
	notifications, total, err := s.storage.GetNotificationHistory(userID, limit, offset, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification history: %w", err)
	}

	hasMore := (offset + len(notifications)) < total

	return &HistoryResponse{
		Notifications: notifications,
		Total:         total,
		HasMore:       hasMore,
	}, nil
}

// shouldSendNotification checks if a notification should be sent to a subscription
func (s *Service) shouldSendNotification(sub Subscription, notificationType string, data map[string]interface{}) bool {
	// "manual" notifications (explicitly triggered via API/CLI) are always delivered
	// regardless of the subscription's notification type filter.
	if notificationType == "manual" {
		return true
	}

	// Check if subscription wants this notification type
	if len(sub.NotificationTypes) > 0 {
		found := false
		for _, nt := range sub.NotificationTypes {
			if nt == notificationType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// isSubscribedToSession checks if a subscription is for a specific session
func (s *Service) isSubscribedToSession(sub Subscription, sessionID string) bool {
	// Empty session_ids means subscribed to all sessions
	if len(sub.SessionIDs) == 0 {
		return true
	}

	// Check if the session is in the subscription's session list
	for _, sid := range sub.SessionIDs {
		if sid == sessionID {
			return true
		}
	}

	return false
}

// getSessionIDFromData extracts session ID from notification data
func getSessionIDFromData(data map[string]interface{}) string {
	if data == nil {
		return ""
	}

	if sessionID, ok := data["session_id"].(string); ok {
		return sessionID
	}

	return ""
}

// SendNotification sends a notification based on a SendNotificationRequest.
// It routes to SendNotificationToSession or SendNotificationToUser depending on which field is set.
func (s *Service) SendNotification(req SendNotificationRequest) (*SendNotificationResponse, error) {
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if req.Body == "" {
		return nil, fmt.Errorf("body is required")
	}
	if req.SessionID == "" && req.UserID == "" {
		return nil, fmt.Errorf("either session_id or user_id is required")
	}

	data := make(map[string]interface{})
	if req.URL != "" {
		data["url"] = req.URL
	}
	if req.Icon != "" {
		data["icon"] = req.Icon
	}
	if req.InitialMessage != "" {
		data["initial_message"] = req.InitialMessage
	}

	var err error
	if req.SessionID != "" {
		err = s.SendNotificationToSession(req.SessionID, req.Title, req.Body, "manual", data)
	} else {
		err = s.SendNotificationToUser(req.UserID, req.Title, req.Body, "manual", data)
	}

	if err != nil {
		return &SendNotificationResponse{Success: false, Message: err.Error()}, err
	}

	return &SendNotificationResponse{Success: true}, nil
}

// GetBaseDir returns the base directory for user data
func GetBaseDir() string {
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = "/home/agentapi"
	}
	return filepath.Join(homeDir, ".agentapi-proxy")
}
