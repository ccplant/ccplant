package sessioncount

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	sessionrunner "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const defaultInterval = time.Minute

type allocationStore interface {
	ListAllocations(context.Context, string) ([]*sessionrunner.Allocation, error)
	ListBindings(context.Context, string) ([]*sessionrunner.Binding, error)
}

type Worker struct {
	allocations allocationStore
	routes      repositories.SessionRouteRepository
	teams       repositories.TeamConfigRepository
	repository  repositories.SessionCountRepository
	interval    time.Duration
	now         func() time.Time
}

func NewWorker(allocations allocationStore, routes repositories.SessionRouteRepository, teams repositories.TeamConfigRepository, repository repositories.SessionCountRepository, interval time.Duration) *Worker {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Worker{allocations: allocations, routes: routes, teams: teams, repository: repository, interval: interval, now: time.Now}
}

func (w *Worker) Run(ctx context.Context) {
	w.collectAndLog(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.collectAndLog(ctx)
		}
	}
}

func (w *Worker) collectAndLog(ctx context.Context) {
	if err := w.Collect(ctx); err != nil {
		log.Printf("[SESSION_COUNT_WORKER] Collection failed: %v", err)
	}
}

// Collect saves a complete snapshot. A failed read prevents all writes, so a
// missing sample means collection failed while a stored zero means no session.
func (w *Worker) Collect(ctx context.Context) error {
	allocations, err := w.allocations.ListAllocations(ctx, "")
	if err != nil {
		return fmt.Errorf("list allocations: %w", err)
	}
	routes, err := w.routes.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list session routes: %w", err)
	}
	bindings, err := w.allocations.ListBindings(ctx, "")
	if err != nil {
		return fmt.Errorf("list pool bindings: %w", err)
	}
	previous, err := w.repository.ListDimensions(ctx)
	if err != nil {
		return fmt.Errorf("list stored dimensions: %w", err)
	}

	routeBySession := make(map[string]*repositories.SessionRoute, len(routes))
	for _, route := range routes {
		routeBySession[route.SessionID] = route
	}

	type count struct{ all, active, running, suspended int }
	counts := map[entities.SessionCountDimension]count{}
	for _, dimension := range previous {
		if dimension.Pool != "" && dimension.PrincipalID != "" {
			counts[dimension] = count{}
		}
	}
	for _, binding := range bindings {
		principalID, err := w.bindingPrincipalID(ctx, binding)
		if err != nil {
			return err
		}
		if principalID != "" {
			counts[entities.SessionCountDimension{Pool: binding.Pool, PrincipalID: principalID}] = count{}
		}
	}
	for _, allocation := range allocations {
		route := routeBySession[allocation.SessionID]
		if route == nil {
			// Enqueue happens immediately before route persistence. Avoid inventing
			// an owner during that short window; the next snapshot will include it.
			continue
		}
		principalID, err := w.routePrincipalID(ctx, route)
		if err != nil {
			return err
		}
		if principalID == "" {
			continue
		}
		dimension := entities.SessionCountDimension{Pool: allocation.Pool, PrincipalID: principalID}
		value := counts[dimension]
		value.all++
		switch route.Status {
		case "active", "stable":
			value.active++
		case "running":
			value.running++
		case "suspended":
			value.suspended++
		}
		counts[dimension] = value
	}

	sampledAt := w.now().UTC().Truncate(w.interval)
	samples := make([]entities.SessionCountSample, 0, len(counts))
	for dimension, value := range counts {
		samples = append(samples, entities.SessionCountSample{SessionCountDimension: dimension, SampledAt: sampledAt, AllCount: value.all, ActiveCount: value.active, RunningCount: value.running, SuspendedCount: value.suspended})
	}
	sort.Slice(samples, func(i, j int) bool {
		if samples[i].Pool == samples[j].Pool {
			return samples[i].PrincipalID < samples[j].PrincipalID
		}
		return samples[i].Pool < samples[j].Pool
	})
	if err := w.repository.SaveSnapshot(ctx, sampledAt, samples); err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	return nil
}

func (w *Worker) routePrincipalID(ctx context.Context, route *repositories.SessionRoute) (string, error) {
	if route.Scope != string(entities.ScopeTeam) {
		return route.UserID, nil
	}
	team, err := w.teams.FindByTeamID(ctx, route.TeamID)
	if err != nil {
		return "", fmt.Errorf("resolve team %q principal: %w", route.TeamID, err)
	}
	if team == nil || team.PrincipalID() == "" {
		return "", fmt.Errorf("team %q has no principal ID", route.TeamID)
	}
	return team.PrincipalID(), nil
}

func (w *Worker) bindingPrincipalID(ctx context.Context, binding *sessionrunner.Binding) (string, error) {
	switch binding.SubjectType {
	case sessionrunner.SubjectUser:
		return binding.SubjectID, nil
	case sessionrunner.SubjectTeam:
		team, err := w.teams.FindByTeamID(ctx, binding.SubjectID)
		if err != nil {
			return "", fmt.Errorf("resolve team binding %q principal: %w", binding.SubjectID, err)
		}
		if team == nil || team.PrincipalID() == "" {
			return "", fmt.Errorf("team binding %q has no principal ID", binding.SubjectID)
		}
		return team.PrincipalID(), nil
	default:
		return "", nil
	}
}
