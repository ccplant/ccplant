package entities

import "time"

// InitialMessageHistoryItem is one distinct initial message used to create a session.
type InitialMessageHistoryItem struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	LastUsedAt time.Time `json:"last_used_at"`
}
