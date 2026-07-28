package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// InitialMessageHistoryRepository persists bounded, user-scoped initial-message history.
type InitialMessageHistoryRepository interface {
	List(ctx context.Context, userID string, limit int) ([]entities.InitialMessageHistoryItem, error)
	UpsertAndTrim(ctx context.Context, userID, content string, maxItems int) (entities.InitialMessageHistoryItem, error)
	DeleteAll(ctx context.Context, userID string) error
}
