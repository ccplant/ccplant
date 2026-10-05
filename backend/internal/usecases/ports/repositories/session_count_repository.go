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

// SessionRuntimeRepository exposes authorized status history for runtime
// aggregation. Implementations include the last event before From for every
// matching session so callers can carry state across the range boundary.
type SessionRuntimeRepository interface {
	ListRuntimeEvents(context.Context, entities.SessionRuntimeQuery) ([]entities.SessionStatusUsageEvent, error)
	RuntimeCoverageStart(context.Context, string) (*time.Time, error)
}
