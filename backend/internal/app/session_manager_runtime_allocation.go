package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	allocationworker "github.com/takutakahashi/agentapi-proxy/internal/modules/sessionallocation"
	externalmanager "github.com/takutakahashi/agentapi-proxy/internal/modules/sessionmanager"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

type sessionManagerAllocationDependencies struct {
	config  *config.Config
	manager *services.KubernetesSessionManager
	redis   *redis.Client
	routes  portrepos.SessionRouteRepository
	remote  bool
}
type sessionManagerElectionSettings struct {
	lease, renew, retry time.Duration
	name                string
}

func sessionManagerElectionConfig(cfg *config.Config) sessionManagerElectionSettings {
	lease := durationOr(cfg.SessionManager.Allocation.LeaseDuration, 15*time.Second)
	renew := durationOr(cfg.SessionManager.Allocation.RenewDeadline, 10*time.Second)
	retry := durationOr(cfg.SessionManager.Allocation.RetryPeriod, 2*time.Second)
	leaseName := cfg.SessionManager.Allocation.LeaseName
	if leaseName == "" {
		leaseName = schedule.SessionAllocatorLeaseName
	}
	return sessionManagerElectionSettings{lease: lease, renew: renew, retry: retry, name: leaseName}
}

func startSessionManagerAllocation(ctx context.Context, deps sessionManagerAllocationDependencies) (*allocationworker.Worker, error) {
	election := sessionManagerElectionConfig(deps.config)
	if deps.remote {
		return nil, startRemoteSessionManagerElection(ctx, deps, election)
	}
	return startLocalSessionManagerAllocation(ctx, deps, election), nil
}

func startLocalSessionManagerAllocation(runtimeCtx context.Context, deps sessionManagerAllocationDependencies, election sessionManagerElectionSettings) *allocationworker.Worker {
	cfg, manager, redisClient := deps.config, deps.manager, deps.redis
	lease, renew, retry, leaseName := election.lease, election.renew, election.retry, election.name
	sessionRouteRepo := deps.routes
	localClient := newLocalAllocationClient(manager, sessionRouteRepo)
	allocator := allocationworker.NewWorker(manager, localClient)
	if cfg.SessionManager.UpgradeVersionURL != "" {
		go runSessionManagerVersionPoller(runtimeCtx, cfg, manager.GetClient(), manager.GetNamespace())
	}
	elector := schedule.NewRedisLeaderElector(redisClient, schedule.LeaderElectionConfig{
		LeaseDuration: lease, RenewDeadline: renew, RetryPeriod: retry,
		LeaseName: leaseName, Namespace: manager.GetNamespace(),
	})
	go elector.Run(runtimeCtx, func(leaderCtx context.Context) {
		log.Printf("[SESSION_MANAGER] Became local allocation leader")
		if err := allocator.Start(leaderCtx); err != nil {
			log.Printf("[SESSION_MANAGER] Allocation loop failed: %v", err)
		}
	}, allocator.Stop)
	return allocator
}

func startRemoteSessionManagerElection(runtimeCtx context.Context, deps sessionManagerAllocationDependencies, election sessionManagerElectionSettings) error {
	cfg, manager := deps.config, deps.manager
	lease, renew, retry, leaseName := election.lease, election.renew, election.retry, election.name
	// Runtime configuration is owned by this manager's command-line arguments
	// and environment. The parent controls allocation, but must not replace the
	// manager's image, sidecars, resources, or other workload settings.
	instanceID, hostnameErr := os.Hostname()
	if hostnameErr != nil {
		log.Printf("[SESSION_MANAGER] Hostname unavailable; using generated identity: %v", hostnameErr)
	}
	if instanceID == "" {
		instanceID = uuid.NewString()
	}
	lock, lockErr := resourcelock.New(
		resourcelock.LeasesResourceLock,
		manager.GetNamespace(),
		leaseName,
		manager.GetClient().CoreV1(),
		manager.GetClient().CoordinationV1(),
		resourcelock.ResourceLockConfig{Identity: instanceID},
	)
	if lockErr != nil {
		return fmt.Errorf("create Kubernetes session-manager lease: %w", lockErr)
	}
	leaderElectionConfig := leaderelection.LeaderElectionConfig{
		Lock: lock, LeaseDuration: lease, RenewDeadline: renew, RetryPeriod: retry, ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaderCtx context.Context) {
				log.Printf("[SESSION_MANAGER] Became remote execution leader")
				// Session traffic for runner-pool workloads uses the per-session direct
				// runtime channel, but manager-level lifecycle operations (notably
				// deletion) still use outbound control during the migration to typed
				// manager operations. Keep this reconnecting worker alive for every
				// remote manager so a proxy rollout cannot permanently strand sessions.
				control := externalmanager.NewControlWorker(externalmanager.ControlWorkerConfig{UpstreamURL: cfg.SessionManager.UpstreamURL, ConnectionToken: cfg.SessionManager.ConnectionToken, LocalURL: cfg.SessionManager.APIURL, InstanceID: instanceID, LocalHMACSecret: cfg.SessionManager.HMACSecret})
				go control.Start(leaderCtx)
				runSessionRunnerManagerHeartbeat(leaderCtx, sessionManagerHeartbeat{
					upstream: cfg.SessionManager.UpstreamURL, managerID: cfg.SessionManager.ID,
					token: cfg.SessionManager.ConnectionToken, config: cfg, manager: manager,
				})
			},
			OnStoppedLeading: func() { log.Printf("[SESSION_MANAGER] Lost remote execution leadership") },
			OnNewLeader:      func(identity string) { log.Printf("[SESSION_MANAGER] Remote execution leader is %s", identity) },
		},
	}
	go runRecoveringLeaderElection(runtimeCtx, retry, func() (leaderElectionRunner, error) {
		return leaderelection.NewLeaderElector(leaderElectionConfig)
	})
	return nil
}
