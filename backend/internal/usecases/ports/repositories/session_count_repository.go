package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// SessionCountRepository persists append-only session status events.
type SessionCountRepository interface {
	SaveEvent(context.Context, entities.SessionStatusUsageEvent) error
	Close() error
}
