package app

import (
	"context"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
)

// StartSessionRouteReconciler runs the level-triggered session route
// reconciler on the elected API replica. Route state is otherwise advanced only
// by events from managers and runtimes; this loop guarantees that a route whose
// owner disappeared is still finalized instead of lingering as a stale session.
func (s *Server) StartSessionRouteReconciler(ctx context.Context) {
	cfg := s.config.SessionRouteReconciler
	if !cfg.Enabled {
		log.Printf("[ROUTE_RECONCILE] Session route reconciler disabled")
		return
	}
	if s.sessionRouteRepo == nil || s.router == nil || s.router.handlers == nil || s.router.handlers.sessionController == nil {
		return
	}
	controller := s.router.handlers.sessionController
	interval := durationOr(cfg.Interval, 5*time.Minute)
	opts := controllers.SessionRouteReconcileOptions{
		DryRun:                   cfg.DryRun,
		OrphanGrace:              durationOr(cfg.OrphanGrace, 0),
		StuckDeletionGrace:       durationOr(cfg.StuckDeletionGrace, 0),
		TTLStuckCeiling:          durationOr(cfg.TTLStuckCeiling, 0),
		ConfigurationOrphanGrace: durationOr(cfg.ConfigurationOrphanGrace, 0),
	}

	run := func(runCtx context.Context) {
		pass := func() {
			result := controller.ReconcileSessionRoutes(runCtx, opts)
			finalized := 0
			for _, n := range result.Finalized {
				finalized += n
			}
			if finalized > 0 || result.Failed > 0 || result.ConfigurationsDeleted > 0 {
				log.Printf("[ROUTE_RECONCILE] Pass complete: scanned=%d finalized=%v failed=%d configurations_deleted=%d dry_run=%t",
					result.Scanned, result.Finalized, result.Failed, result.ConfigurationsDeleted, opts.DryRun)
			}
		}
		pass()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				pass()
			}
		}
	}

	lease := buildWorkerLeaseClient(s.config)
	if lease == nil {
		// Every action is idempotent, so running on each replica is safe; leader
		// election only avoids duplicate work and duplicate usage events.
		log.Printf("[ROUTE_RECONCILE] Started without leader election (interval: %s, dry_run: %t)", interval, opts.DryRun)
		go run(ctx)
		return
	}
	elector := schedule.NewLeaderElector(lease, schedule.LeaderElectionConfig{
		LeaseDuration: 2 * time.Minute,
		RenewDeadline: 90 * time.Second,
		RetryPeriod:   30 * time.Second,
		LeaseName:     schedule.SessionRouteReconcilerLeaseName,
		Namespace:     s.namespace,
	})
	go elector.Run(ctx, run, func() {})
	log.Printf("[ROUTE_RECONCILE] Started with leader election (interval: %s, dry_run: %t)", interval, opts.DryRun)
}
