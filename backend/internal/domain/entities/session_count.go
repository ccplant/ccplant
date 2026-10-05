package entities

import "time"

// SessionStatusUsageEvent is an append-only session state transition. A usage
// snapshot can be reconstructed by selecting each session's latest event at a
// point in time and aggregating it by pool and principal.
type SessionStatusUsageEvent struct {
	EventID     string    `json:"event_id"`
	OccurredAt  time.Time `json:"occurred_at"`
	SessionID   string    `json:"session_id"`
	Pool        string    `json:"pool"`
	Scope       string    `json:"scope"`
	PrincipalID string    `json:"principal_id"`
	Status      string    `json:"status"`
}
