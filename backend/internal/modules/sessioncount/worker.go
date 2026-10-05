package sessioncount

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	sessionrunner "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type allocationStore interface {
	GetAllocation(context.Context, string) (*sessionrunner.Allocation, error)
}

type statusSource interface {
	GetSession(string) entities.Session
	ListSessions(entities.SessionFilter) []entities.Session
	SubscribeStatusEvents() (<-chan repositories.SessionStatusEvent, func())
}

// Worker seeds current state once, then persists status notifications without polling.
type Worker struct {
	source      statusSource
	allocations allocationStore
	routes      repositories.SessionRouteRepository
	teams       repositories.TeamConfigRepository
	repository  repositories.SessionCountRepository
}

func NewWorker(source statusSource, allocations allocationStore, routes repositories.SessionRouteRepository, teams repositories.TeamConfigRepository, repository repositories.SessionCountRepository) *Worker {
	return &Worker{source: source, allocations: allocations, routes: routes, teams: teams, repository: repository}
}

func (w *Worker) Run(ctx context.Context) {
	events, cancel := w.source.SubscribeStatusEvents()
	defer cancel()
	if err := w.seed(ctx); err != nil {
		log.Printf("[SESSION_COUNT_EVENTS] Initial state failed: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := w.RecordStatus(ctx, event); err != nil {
				log.Printf("[SESSION_COUNT_EVENTS] Record failed for session %s: %v", event.SessionID, err)
			}
		}
	}
}

func (w *Worker) seed(ctx context.Context) error {
	seen := map[string]struct{}{}
	for _, session := range w.source.ListSessions(entities.SessionFilter{}) {
		seen[session.ID()] = struct{}{}
		if err := w.RecordStatus(ctx, repositories.SessionStatusEvent{SessionID: session.ID(), Status: session.Status(), Timestamp: session.UpdatedAt()}); err != nil {
			return err
		}
	}
	routes, err := w.routes.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list routes for initial state: %w", err)
	}
	for _, route := range routes {
		if _, exists := seen[route.SessionID]; exists || route.Status == "" {
			continue
		}
		if err := w.recordRoute(ctx, route, repositories.SessionStatusEvent{SessionID: route.SessionID, Status: route.Status, Timestamp: route.StatusUpdatedAt}); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) RecordStatus(ctx context.Context, event repositories.SessionStatusEvent) error {
	if event.SessionID == "" || event.Status == "" {
		return nil
	}
	dimensions, err := w.ResolveDimensions(ctx, event.SessionID)
	if err != nil {
		return err
	}
	return w.RecordResolvedStatus(ctx, dimensions, event)
}

// ResolveDimensions captures the metadata required to record future events for
// a session, including after destructive cleanup removes its route/allocation.
func (w *Worker) ResolveDimensions(ctx context.Context, sessionID string) (repositories.SessionUsageDimensions, error) {
	route, err := w.routes.Get(ctx, sessionID)
	if err != nil {
		return repositories.SessionUsageDimensions{}, fmt.Errorf("get session route: %w", err)
	}
	if route != nil {
		return w.resolve(ctx, route.SessionID, route.Pool, route.Scope, route.UserID, route.TeamID, route.StatusUpdatedAt)
	}
	session := w.source.GetSession(sessionID)
	if session == nil {
		return repositories.SessionUsageDimensions{}, fmt.Errorf("session metadata not found")
	}
	return w.resolve(ctx, sessionID, "", string(session.Scope()), session.UserID(), session.TeamID(), session.UpdatedAt())
}

func (w *Worker) recordRoute(ctx context.Context, route *repositories.SessionRoute, event repositories.SessionStatusEvent) error {
	dimensions, err := w.resolve(ctx, route.SessionID, route.Pool, route.Scope, route.UserID, route.TeamID, route.StatusUpdatedAt)
	if err != nil {
		return err
	}
	return w.RecordResolvedStatus(ctx, dimensions, event)
}

func (w *Worker) pool(ctx context.Context, sessionID, pool string) (string, error) {
	if pool != "" {
		return pool, nil
	}
	allocation, err := w.allocations.GetAllocation(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("get allocation: %w", err)
	}
	if allocation == nil || allocation.Pool == "" {
		return "", fmt.Errorf("pool not found")
	}
	return allocation.Pool, nil
}

func (w *Worker) resolve(ctx context.Context, sessionID, pool, scope, userID, teamID string, lastStatusAt time.Time) (repositories.SessionUsageDimensions, error) {
	resolvedPool, err := w.pool(ctx, sessionID, pool)
	if err != nil {
		return repositories.SessionUsageDimensions{}, err
	}
	principalID := userID
	if scope == string(entities.ScopeTeam) {
		team, err := w.teams.FindByTeamID(ctx, teamID)
		if err != nil {
			return repositories.SessionUsageDimensions{}, fmt.Errorf("resolve team principal: %w", err)
		}
		if team == nil || team.PrincipalID() == "" {
			return repositories.SessionUsageDimensions{}, fmt.Errorf("team %q has no principal ID", teamID)
		}
		principalID = team.PrincipalID()
	}
	if principalID == "" {
		return repositories.SessionUsageDimensions{}, fmt.Errorf("principal not found")
	}
	return repositories.SessionUsageDimensions{SessionID: sessionID, Pool: resolvedPool, Scope: scope, PrincipalID: principalID, LastStatusAt: lastStatusAt}, nil
}

// RecordResolvedStatus records an event without consulting live session
// metadata. This is used for terminal events after deletion succeeds.
func (w *Worker) RecordResolvedStatus(ctx context.Context, dimensions repositories.SessionUsageDimensions, event repositories.SessionStatusEvent) error {
	if event.SessionID == "" {
		event.SessionID = dimensions.SessionID
	}
	if event.SessionID == "" || event.Status == "" {
		return nil
	}
	if dimensions.SessionID != event.SessionID {
		return fmt.Errorf("session usage dimensions mismatch: %s != %s", dimensions.SessionID, event.SessionID)
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(event.SessionID+"\x00"+event.Status+"\x00"+event.Timestamp.UTC().Format(time.RFC3339Nano))))
	return w.repository.SaveEvent(ctx, entities.SessionStatusUsageEvent{EventID: id, OccurredAt: event.Timestamp.UTC(), SessionID: event.SessionID, Pool: dimensions.Pool, Scope: dimensions.Scope, PrincipalID: dimensions.PrincipalID, Status: event.Status})
}
