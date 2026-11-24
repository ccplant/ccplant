package notification

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"
)

func (s *JSONStorage) AddNotificationHistory(userID string, notification NotificationHistory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureNotificationsDir(userID); err != nil {
		return err
	}

	// Generate ID if not provided
	if notification.ID == "" {
		notification.ID = fmt.Sprintf("notif_%d_%s", time.Now().Unix(), uuid.New().String()[:8])
	}

	// Load existing history
	history, err := s.loadNotificationHistory(userID)
	if err != nil {
		return err
	}

	// Add new notification
	history = append(history, notification)

	return s.saveNotificationHistory(userID, history)
}

// GetNotificationHistory retrieves notification history with pagination and filtering
func (s *JSONStorage) GetNotificationHistory(userID string, limit, offset int, filters map[string]string) ([]NotificationHistory, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allNotifications, err := s.loadNotificationHistory(userID)
	if err != nil {
		return nil, 0, err
	}

	// Apply filters
	var filteredNotifications []NotificationHistory
	for _, notification := range allNotifications {
		// Apply filters
		if sessionID := filters["session_id"]; sessionID != "" && notification.SessionID != sessionID {
			continue
		}
		if notificationType := filters["type"]; notificationType != "" && notification.Type != notificationType {
			continue
		}

		filteredNotifications = append(filteredNotifications, notification)
	}

	// Sort by newest first
	sort.Slice(filteredNotifications, func(i, j int) bool {
		return filteredNotifications[i].SentAt.After(filteredNotifications[j].SentAt)
	})

	total := len(filteredNotifications)

	// Apply pagination
	if offset >= total {
		return []NotificationHistory{}, total, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return filteredNotifications[offset:end], total, nil
}

// RotateNotificationHistory keeps only the most recent N entries
func (s *JSONStorage) RotateNotificationHistory(userID string, maxEntries int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	allNotifications, err := s.loadNotificationHistory(userID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file means no rotation needed
		}
		return err
	}

	// Sort by newest first
	sort.Slice(allNotifications, func(i, j int) bool {
		return allNotifications[i].SentAt.After(allNotifications[j].SentAt)
	})

	if len(allNotifications) <= maxEntries {
		return nil // No rotation needed
	}

	// Keep only the most recent maxEntries
	keepNotifications := allNotifications[:maxEntries]

	return s.saveNotificationHistory(userID, keepNotifications)
}
