package entities

import (
	"errors"
	"fmt"
	"time"
)

func (n *Notification) ID() NotificationID {
	return n.id
}

// UserID returns the user ID
func (n *Notification) UserID() UserID {
	return n.userID
}

// SubscriptionID returns the subscription ID
func (n *Notification) SubscriptionID() SubscriptionID {
	return n.subscriptionID
}

// SessionID returns the session ID
func (n *Notification) SessionID() SessionID {
	return n.sessionID
}

// Title returns the notification title
func (n *Notification) Title() string {
	return n.title
}

// Body returns the notification body
func (n *Notification) Body() string {
	return n.body
}

// Type returns the notification type
func (n *Notification) Type() NotificationType {
	return n.notType
}

// Data returns a copy of the notification data
func (n *Notification) Data() map[string]interface{} {
	data := make(map[string]interface{})
	for k, v := range n.data {
		data[k] = v
	}
	return data
}

// Status returns the notification status
func (n *Notification) Status() NotificationStatus {
	return n.status
}

// CreatedAt returns when the notification was created
func (n *Notification) CreatedAt() time.Time {
	return n.createdAt
}

// SentAt returns when the notification was sent
func (n *Notification) SentAt() time.Time {
	return n.sentAt
}

// URL returns the notification URL
func (n *Notification) URL() *string {
	return n.url
}

// IconURL returns the notification icon URL
func (n *Notification) IconURL() *string {
	return n.iconURL
}

// Tags returns a copy of the notification tags
func (n *Notification) Tags() Tags {
	tags := make(Tags)
	for k, v := range n.tags {
		tags[k] = v
	}
	return tags
}

// DeliveredAt returns when the notification was delivered
func (n *Notification) DeliveredAt() *time.Time {
	return n.deliveredAt
}

// ClickedAt returns when the notification was clicked
func (n *Notification) ClickedAt() *time.Time {
	return n.clickedAt
}

// ErrorMessage returns the error message if delivery failed
func (n *Notification) ErrorMessage() *string {
	return n.errorMessage
}

// SetSessionID sets the session ID
func (n *Notification) SetSessionID(sessionID SessionID) {
	n.sessionID = sessionID
}

// SetURL sets the notification URL
func (n *Notification) SetURL(url string) {
	n.url = &url
}

// SetIconURL sets the notification icon URL
func (n *Notification) SetIconURL(iconURL string) {
	n.iconURL = &iconURL
}

// SetData sets the notification data
func (n *Notification) SetData(data map[string]interface{}) {
	n.data = make(map[string]interface{})
	for k, v := range data {
		n.data[k] = v
	}
}

// AddData adds a key-value pair to the notification data
func (n *Notification) AddData(key string, value interface{}) {
	if n.data == nil {
		n.data = make(map[string]interface{})
	}
	n.data[key] = value
}

// MarkDelivered marks the notification as delivered
func (n *Notification) MarkDelivered() {
	n.status = NotificationStatusDelivered
	now := time.Now()
	n.deliveredAt = &now
}

// MarkFailed marks the notification as failed
func (n *Notification) MarkFailed(errorMessage string) {
	n.status = NotificationStatusFailed
	n.errorMessage = &errorMessage
}

// MarkClicked marks the notification as clicked
func (n *Notification) MarkClicked() {
	n.status = NotificationStatusClicked
	now := time.Now()
	n.clickedAt = &now
}

// IsDelivered returns true if the notification was delivered
func (n *Notification) IsDelivered() bool {
	return n.status == NotificationStatusDelivered || n.status == NotificationStatusClicked
}

// HasFailed returns true if the notification delivery failed
func (n *Notification) HasFailed() bool {
	return n.status == NotificationStatusFailed
}

// Validate ensures the notification is in a valid state
func (n *Notification) Validate() error {
	if n.id == "" {
		return errors.New("notification ID cannot be empty")
	}

	if n.userID == "" {
		return errors.New("user ID cannot be empty")
	}

	if n.subscriptionID == "" {
		return errors.New("subscription ID cannot be empty")
	}

	if n.title == "" {
		return errors.New("title cannot be empty")
	}

	if n.body == "" {
		return errors.New("body cannot be empty")
	}

	// Validate notification type
	validTypes := []NotificationType{NotificationTypeMessage, NotificationTypeStatus, NotificationTypeSession, NotificationTypeManual}
	typeValid := false
	for _, validType := range validTypes {
		if n.notType == validType {
			typeValid = true
			break
		}
	}
	if !typeValid {
		return fmt.Errorf("invalid notification type: %s", n.notType)
	}

	if n.sentAt.IsZero() {
		return errors.New("sent at time cannot be zero")
	}

	return nil
}
