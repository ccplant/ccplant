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
	route, err := w.routes.Get(ctx, event.SessionID)
	if err != nil {
		return fmt.Errorf("get session route: %w", err)
	}
	if route != nil {
		return w.recordRoute(ctx, route, event)
	}
	session := w.source.GetSession(event.SessionID)
	if session == nil {
		return fmt.Errorf("session metadata not found")
	}
	pool, err := w.pool(ctx, event.SessionID, "")
	if err != nil {
		return err
	}
	return w.save(ctx, event, pool, string(session.Scope()), session.UserID(), session.TeamID())
}

func (w *Worker) recordRoute(ctx context.Context, route *repositories.SessionRoute, event repositories.SessionStatusEvent) error {
	pool, err := w.pool(ctx, route.SessionID, route.Pool)
	if err != nil {
		return err
	}
	return w.save(ctx, event, pool, route.Scope, route.UserID, route.TeamID)
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

func (w *Worker) save(ctx context.Context, event repositories.SessionStatusEvent, pool, scope, userID, teamID string) error {
	principalID := userID
	if scope == string(entities.ScopeTeam) {
		team, err := w.teams.FindByTeamID(ctx, teamID)
		if err != nil {
			return fmt.Errorf("resolve team principal: %w", err)
		}
		if team == nil || team.PrincipalID() == "" {
			return fmt.Errorf("team %q has no principal ID", teamID)
		}
		principalID = team.PrincipalID()
	}
	if principalID == "" {
		return fmt.Errorf("principal not found")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(event.SessionID+"\x00"+event.Status+"\x00"+event.Timestamp.UTC().Format(time.RFC3339Nano))))
	return w.repository.SaveEvent(ctx, entities.SessionStatusUsageEvent{EventID: id, OccurredAt: event.Timestamp.UTC(), SessionID: event.SessionID, Pool: pool, Scope: scope, PrincipalID: principalID, Status: event.Status})
}
