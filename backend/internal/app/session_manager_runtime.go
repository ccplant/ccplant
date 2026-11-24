package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	allocationworker "github.com/takutakahashi/agentapi-proxy/internal/modules/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// SessionManagerRuntime is the execution-plane composition root. It does not
// construct the public API router, OAuth/auth services, webhooks, schedules or
// Slack workers. Kubernetes workload credentials terminate here.
type SessionManagerRuntime struct {
	config        *config.Config
	echo          *echo.Echo
	manager       *services.KubernetesSessionManager
	kvStore       kvstore.Store
	redis         *redis.Client
	allocator     *allocationworker.Worker
	runtimeCancel context.CancelFunc
}

func NewSessionManagerRuntime(parent context.Context, cfg *config.Config, verbose bool) (*SessionManagerRuntime, error) {
	if err := validateSessionManagerRuntimeConfig(cfg); err != nil {
		return nil, err
	}
	manager, err := prepareSessionManagerRuntime(parent, cfg, verbose)
	if err != nil {
		return nil, err
	}
	startup := &sessionManagerStartup{config: cfg, manager: manager,
		remote: cfg.SessionManager.UpstreamURL != "" && cfg.SessionManager.ConnectionToken != ""}
	ready := false
	defer func() {
		if !ready {
			cleanupSessionManagerStartup(manager, startup.store, startup.redis, startup.cancel)
		}
	}()
	if err := startup.initializePersistence(); err != nil {
		return nil, err
	}
	startup.startConfiguration(parent)
	if err := startup.initializeRepositories(); err != nil {
		return nil, err
	}
	if err := startup.initializeState(parent); err != nil {
		return nil, err
	}
	if err := startup.initializeRedis(); err != nil {
		return nil, err
	}
	e, err := startup.router()
	if err != nil {
		return nil, err
	}
	allocator, err := startSessionManagerAllocation(startup.context, sessionManagerAllocationDependencies{
		config: startup.config, manager: manager, redis: startup.redis, routes: startup.routes, remote: startup.remote,
	})
	if err != nil {
		return nil, err
	}
	ready = true
	return &SessionManagerRuntime{config: startup.config, echo: e, manager: manager, kvStore: startup.store,
		redis: startup.redis, allocator: allocator, runtimeCancel: startup.cancel}, nil
}

func fetchParentRuntimeProfile(ctx context.Context, upstream, managerID, token string) (*sessionsettings.RuntimeProfile, error) {
	endpoint := strings.TrimRight(upstream, "/") + "/internal/session-managers/" + url.PathEscape(managerID) + "/runtime-profile"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("[SESSION_MANAGER] Close runtime profile response: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil {
			return nil, fmt.Errorf("runtime profile returned HTTP %d (read response: %w)", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("runtime profile returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var snapshot coreallocation.RuntimeProfileSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("decode runtime profile: %w", err)
	}
	if snapshot.Profile == nil {
		return nil, errors.New("runtime profile response did not contain a profile")
	}
	return snapshot.Profile, nil
}

type leaderElectionRunner interface {
	Run(context.Context)
}

// runRecoveringLeaderElection restarts leader election after a lost lease.
// client-go's LeaderElector.Run returns when renewal fails; without this loop
// the process remains healthy but never attempts to become leader again.
func runRecoveringLeaderElection(ctx context.Context, retryPeriod time.Duration, newRunner func() (leaderElectionRunner, error)) {
	for ctx.Err() == nil {
		runner, err := newRunner()
		if err != nil {
			log.Printf("[SESSION_MANAGER] Initialize remote leader election: %v", err)
		} else {
			runner.Run(ctx)
			if ctx.Err() != nil {
				return
			}
			log.Printf("[SESSION_MANAGER] Remote leader election stopped; retrying")
		}
		if retryPeriod <= 0 {
			retryPeriod = time.Second
		}
		timer := time.NewTimer(retryPeriod)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
