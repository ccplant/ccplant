package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type SessionContextTemplateFilter struct {
	UserID  string
	TeamIDs []string
}

type SessionContextTemplateRepository interface {
	Create(context.Context, *entities.SessionContextTemplate) error
	Get(context.Context, string) (*entities.SessionContextTemplate, error)
	List(context.Context, SessionContextTemplateFilter) ([]*entities.SessionContextTemplate, error)
	Update(context.Context, *entities.SessionContextTemplate) error
	Delete(context.Context, string) error
}
