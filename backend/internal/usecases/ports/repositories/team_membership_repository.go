package repositories

import (
	"context"
	"errors"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

var (
	ErrTeamSyncRateLimited = errors.New("team membership sync is rate limited")
	ErrTeamSyncInProgress  = errors.New("team membership sync is already in progress")
	ErrTeamSyncConflict    = errors.New("team membership sync state changed")
)

// TeamMembershipRepository persists GitHub-backed membership snapshots and
// coordinates sync operations across replicas.
type TeamMembershipRepository interface {
	Get(ctx context.Context, teamPrincipalID string) (*entities.TeamMembershipSnapshot, bool, error)
	List(ctx context.Context) ([]*entities.TeamMembershipSnapshot, error)
	AcquireSync(ctx context.Context, teamPrincipalID, operationID string, now time.Time) (*entities.TeamMembershipSnapshot, error)
	Replace(ctx context.Context, snapshot *entities.TeamMembershipSnapshot, operationID string) error
	ReleaseSync(ctx context.Context, teamPrincipalID, operationID string) error
	Delete(ctx context.Context, teamPrincipalID string) error
}
