package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type StorageFilter struct {
	Scope   entities.ResourceScope
	OwnerID string
	TeamID  string
	TeamIDs []string
}

// StorageRepository persists storage metadata and its last-write-wins manifest.
type StorageRepository interface {
	Create(context.Context, *entities.Storage) error
	GetByID(context.Context, string) (*entities.Storage, error)
	List(context.Context, StorageFilter) ([]*entities.Storage, error)
	Update(context.Context, *entities.Storage) error
	Delete(context.Context, string) error
}
