package repositories

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type LocalUserRepository interface {
	Create(context.Context, *entities.LocalUser) error
	GetByID(context.Context, entities.UserID) (*entities.LocalUser, error)
}
