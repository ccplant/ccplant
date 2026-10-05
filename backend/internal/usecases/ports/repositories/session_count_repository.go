package repositories

import (
	"context"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// SessionUsageDimensions is the immutable ownership and placement metadata
// needed to persist a status event after the live session metadata is removed.
type SessionUsageDimensions struct {
	SessionID    string
	Pool         string
	Scope        string
	PrincipalID  string
	LastStatusAt time.Time
}

// SessionCountRepository persists append-only session status events.
type SessionCountRepository interface {
	SaveEvent(context.Context, entities.SessionStatusUsageEvent) error
	Close() error
}
