package repositories

import (
	"context"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// SessionCountRepository persists point-in-time session counts independently
// of the database used by the rest of the application.
type SessionCountRepository interface {
	ListDimensions(context.Context) ([]entities.SessionCountDimension, error)
	SaveSnapshot(context.Context, time.Time, []entities.SessionCountSample) error
	Close() error
}
