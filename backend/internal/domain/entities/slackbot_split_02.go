package entities

import (
	"fmt"
	"strings"
	"time"
)

func normalizeSlackChannelPattern(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "#")
	return strings.ToLower(value)
}

// IsUserIDAllowed returns true if the given Slack user ID is allowed.
// Matching is exact. If no user IDs are configured, all users are allowed.
func (s *SlackBot) IsUserIDAllowed(userID string) bool {
	if len(s.allowedUserIDs) == 0 {
		return true
	}
	for _, id := range s.allowedUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// BotToken returns the transient bot token value (write-only, never returned in API)
func (s *SlackBot) BotToken() string { return s.botToken }

// SetBotToken sets the transient bot token value
func (s *SlackBot) SetBotToken(token string) {
	s.botToken = token
	s.updatedAt = time.Now()
}

// AppToken returns the transient app token value (write-only, never returned in API)
func (s *SlackBot) AppToken() string { return s.appToken }

// SetAppToken sets the transient app token value
func (s *SlackBot) SetAppToken(token string) {
	s.appToken = token
	s.updatedAt = time.Now()
}

// Validate validates the SlackBot configuration
func (s *SlackBot) Validate() error {
	if s.id == "" {
		return ErrInvalidSlackBot{Field: "id", Message: "id is required"}
	}
	if s.name == "" {
		return ErrInvalidSlackBot{Field: "name", Message: "name is required"}
	}
	if s.userID == "" {
		return ErrInvalidSlackBot{Field: "user_id", Message: "user_id is required"}
	}
	if s.maxSessions < 0 {
		return ErrInvalidSlackBot{Field: "max_sessions", Message: "max_sessions must be non-negative"}
	}
	return nil
}

// ErrInvalidSlackBot is returned when a SlackBot fails validation
type ErrInvalidSlackBot struct {
	Field   string
	Message string
}

func (e ErrInvalidSlackBot) Error() string {
	return fmt.Sprintf("invalid slackbot %s: %s", e.Field, e.Message)
}

// ErrSlackBotNotFound is returned when a SlackBot is not found
type ErrSlackBotNotFound struct {
	ID string
}

func (e ErrSlackBotNotFound) Error() string {
	return fmt.Sprintf("slackbot not found: %s", e.ID)
}
