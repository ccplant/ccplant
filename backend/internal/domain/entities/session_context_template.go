package entities

import "time"

const (
	SessionContextTemplatePreparing = "preparing"
	SessionContextTemplateReady     = "ready"
)

// SessionContextTemplate is an immutable checkpoint converted from a session.
type SessionContextTemplate struct {
	ID              string        `json:"id"`
	SourceSessionID string        `json:"source_session_id"`
	SnapshotID      string        `json:"snapshot_id"`
	Name            string        `json:"name"`
	Description     string        `json:"description,omitempty"`
	OwnerUserID     string        `json:"owner_user_id"`
	Scope           ResourceScope `json:"scope"`
	TeamID          string        `json:"team_id,omitempty"`
	AgentType       string        `json:"agent_type"`
	Status          string        `json:"status"`
	CreatedAt       time.Time     `json:"created_at"`
	LastUsedAt      *time.Time    `json:"last_used_at,omitempty"`
	UseCount        int64         `json:"use_count"`
}
