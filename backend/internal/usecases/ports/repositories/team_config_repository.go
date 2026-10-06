package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// TeamConfigRepository defines the interface for team configuration persistence
type TeamConfigRepository interface {
	// Save creates or updates a team configuration
	Save(ctx context.Context, config *entities.TeamConfig) error

	// FindByTeamID retrieves a team configuration by team ID
	FindByTeamID(ctx context.Context, teamID string) (*entities.TeamConfig, error)

	// Delete removes a team configuration
	Delete(ctx context.Context, teamID string) error

	// Exists checks if a team configuration exists
	Exists(ctx context.Context, teamID string) (bool, error)

	// List retrieves all team configurations
	List(ctx context.Context) ([]*entities.TeamConfig, error)

	// ListRelevant retrieves only configs owned by the principal or mapped to
	// one of the supplied external team memberships.
	ListRelevant(ctx context.Context, memberships []entities.GitHubTeamMembership, principalID string) ([]*entities.TeamConfig, error)
}
